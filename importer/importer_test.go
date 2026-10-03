/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package importer

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

const sampleConf = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.8.1.2/32
DNS = $PRIMARY_DNS, $SECONDARY_DNS
Jc = 4
H1 = 1234

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 203.0.113.5:51820
`

func qCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	buf.Write(size[:])
	zw := zlib.NewWriter(&buf)
	zw.Write(data)
	zw.Close()
	return buf.Bytes()
}

func TestVPNKeyWithConfig(t *testing.T) {
	lastConfig, _ := json.Marshal(map[string]any{"config": sampleConf, "hostName": "203.0.113.5"})
	payload, _ := json.Marshal(map[string]any{
		"containers": []any{map[string]any{
			"container": "amnezia-awg",
			"awg":       map[string]any{"last_config": string(lastConfig), "port": "51820"},
		}},
		"description": "My Server (Home)",
		"dns1":        "1.1.1.1",
		"dns2":        "1.0.0.1",
	})
	key := "vpn://" + base64.RawURLEncoding.EncodeToString(qCompress(t, payload))
	cs, err := FromText("", "  "+key+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("got %d configs", len(cs))
	}
	if cs[0].Name != "My-Server-Home" {
		t.Errorf("name %q", cs[0].Name)
	}
	if !strings.Contains(cs[0].Text, "DNS = 1.1.1.1, 1.0.0.1") || strings.Contains(cs[0].Text, "$PRIMARY") {
		t.Errorf("DNS placeholders not filled:\n%s", cs[0].Text)
	}
}

func TestVPNKeyFromFields(t *testing.T) {
	lastConfig, _ := json.Marshal(map[string]any{
		"client_ip":       "10.8.1.7",
		"client_priv_key": "cPriv",
		"server_pub_key":  "sPub",
		"psk_key":         "psk",
		"hostName":        "vpn.example.com",
		"port":            51820,
		"Jc":              "3",
		"H1":              "77",
		"mtu":             "1280",
	})
	payload, _ := json.Marshal(map[string]any{
		"containers": []any{
			map[string]any{"container": "amnezia-openvpn", "openvpn": map[string]any{}},
			map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(lastConfig)}},
		},
		"hostName": "vpn.example.com",
		"dns1":     "9.9.9.9",
	})
	key := "vpn://" + base64.RawURLEncoding.EncodeToString(qCompress(t, payload))
	cs, err := FromVPNKey(key)
	if err != nil {
		t.Fatal(err)
	}
	text := cs[0].Text
	for _, want := range []string{"PrivateKey = cPriv", "Address = 10.8.1.7/32", "DNS = 9.9.9.9", "MTU = 1280", "Jc = 3", "H1 = 77", "PublicKey = sPub", "PresharedKey = psk", "Endpoint = vpn.example.com:51820", "AllowedIPs = 0.0.0.0/0, ::/0"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if cs[0].Name != "vpn.example.com-awg" {
		t.Errorf("name %q", cs[0].Name)
	}
}

func TestVPNKeyErrors(t *testing.T) {
	for _, bad := range []string{"vpn://!!!", "vpn://AAAA", "vpn://" + base64.RawURLEncoding.EncodeToString([]byte(`{"containers":[]}`))} {
		if _, err := FromVPNKey(bad); err == nil {
			t.Errorf("FromVPNKey(%q) succeeded", bad)
		}
	}
	if _, err := FromText("x", "hello world"); err == nil {
		t.Error("FromText accepted garbage")
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	key, err := EncodeVPNKey("roundtrip", strings.ReplaceAll(sampleConf, "$PRIMARY_DNS, $SECONDARY_DNS", "8.8.8.8"), "")
	if err != nil {
		t.Fatal(err)
	}
	cs, err := FromVPNKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if cs[0].Name != "roundtrip" || !strings.Contains(cs[0].Text, "DNS = 8.8.8.8") || !strings.Contains(cs[0].Text, "Endpoint = 203.0.113.5:51820") {
		t.Fatalf("round trip lost data: %+v", cs[0])
	}
}

func TestZipRoundTrip(t *testing.T) {
	data, err := ToZip([]Config{{Name: "b", Text: "B"}, {Name: "a", Text: "A"}, {Name: "A", Text: "A2"}})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := FromBytes("export.zip", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
		t.Fatalf("got %d configs", len(cs))
	}
	names := map[string]string{}
	for _, c := range cs {
		names[c.Name] = c.Text
	}
	if names["A"] != "A2" || names["a-2"] != "A" || names["b"] != "B" {
		t.Fatalf("unexpected names %v", names)
	}
}

func TestFromBytesConf(t *testing.T) {
	cs, err := FromBytes(`C:\Users\x\Downloads\my vpn.conf`, []byte("\ufeff"+sampleConf))
	if err != nil {
		t.Fatal(err)
	}
	if cs[0].Name != "my-vpn" || !strings.HasPrefix(cs[0].Text, "[Interface]") {
		t.Fatalf("got %+v", cs[0])
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"  hello world ":        "hello-world",
		"Москва":                "tunnel",
		"a/b\\c":                "a-b-c",
		strings.Repeat("x", 40): strings.Repeat("x", 32),
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
