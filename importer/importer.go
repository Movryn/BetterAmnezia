/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

// Package importer turns the various formats users receive tunnels in into
// wg-quick style configuration text: plain .conf files, zip archives of
// them and AmneziaVPN "vpn://" share keys.
package importer

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Config is an imported tunnel before validation by the conf package.
type Config struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

const maxConfigSize = 512 * 1024

var nameCleaner = regexp.MustCompile(`[^a-zA-Z0-9_=+.-]+`)

// SanitizeName makes a string usable as a tunnel name (Windows interface
// alias limits apply: at most 32 characters of a restricted set).
func SanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = nameCleaner.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if len(s) > 32 {
		s = s[:32]
	}
	if s == "" {
		s = "tunnel"
	}
	return s
}

// NameFromFilename strips directories and the extension.
func NameFromFilename(filename string) string {
	base := path.Base(strings.ReplaceAll(filename, `\`, "/"))
	for _, ext := range []string{".conf.dpapi", ".conf", ".txt", ".vpn"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	return SanitizeName(base)
}

// LooksLikeConfig reports whether text appears to be a wg-quick config.
func LooksLikeConfig(text string) bool {
	return strings.Contains(strings.ToLower(text), "[interface]")
}

// FromBytes imports a file's content, detecting the format.
func FromBytes(filename string, data []byte) ([]Config, error) {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte("PK\x03\x04")) {
		return FromZip(data)
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	if !utf8.ValidString(text) {
		return nil, errors.New("file is not text")
	}
	return FromText(NameFromFilename(filename), text)
}

// FromText imports pasted text: a config, or one or more vpn:// keys.
func FromText(name, text string) ([]Config, error) {
	trimmed := strings.TrimSpace(text)
	if LooksLikeConfig(trimmed) {
		return []Config{{Name: SanitizeName(name), Text: trimmed + "\n"}}, nil
	}
	var out []Config
	var errs []error
	for _, field := range strings.Fields(trimmed) {
		if strings.HasPrefix(field, "vpn://") {
			cs, err := FromVPNKey(field)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			out = append(out, cs...)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return nil, errors.New("no tunnel configuration found; expected an [Interface] section or a vpn:// key")
}

// FromZip imports every .conf file in a zip archive.
func FromZip(data []byte) ([]Config, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var out []Config
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".conf") {
			continue
		}
		if f.UncompressedSize64 > maxConfigSize {
			return nil, fmt.Errorf("%s is too large", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxConfigSize+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, Config{Name: NameFromFilename(f.Name), Text: strings.TrimPrefix(string(b), "\ufeff")})
	}
	if len(out) == 0 {
		return nil, errors.New("archive contains no .conf files")
	}
	return out, nil
}

// ToZip writes configs into a zip archive, one .conf per tunnel.
func ToZip(configs []Config) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	used := make(map[string]int)
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	for _, c := range configs {
		name := SanitizeName(c.Name)
		if n := used[strings.ToLower(name)]; n > 0 {
			name = fmt.Sprintf("%s-%d", name, n+1)
		}
		used[strings.ToLower(name)]++
		w, err := zw.Create(name + ".conf")
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, c.Text); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeVPNPayload undoes base64url and Qt's qCompress (4-byte big endian
// length followed by a zlib stream). Uncompressed JSON is accepted too.
func decodeVPNPayload(key string) ([]byte, error) {
	b64 := strings.TrimPrefix(strings.TrimSpace(key), "vpn://")
	b64 = strings.TrimRight(b64, "=")
	raw, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		if raw, err = base64.RawStdEncoding.DecodeString(b64); err != nil {
			return nil, errors.New("vpn:// key is not valid base64")
		}
	}
	if len(raw) > 0 && raw[0] == '{' {
		return raw, nil
	}
	if len(raw) < 6 {
		return nil, errors.New("vpn:// key is too short")
	}
	expected := binary.BigEndian.Uint32(raw[:4])
	if expected > 16*1024*1024 {
		return nil, errors.New("vpn:// key declares an implausible size")
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		return nil, fmt.Errorf("vpn:// key is not compressed data: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, int64(expected)+1))
	if err != nil {
		return nil, err
	}
	return out, nil
}

type vpnKey struct {
	Containers       []map[string]json.RawMessage `json:"containers"`
	DefaultContainer string                       `json:"defaultContainer"`
	Description      string                       `json:"description"`
	DNS1             string                       `json:"dns1"`
	DNS2             string                       `json:"dns2"`
	HostName         string                       `json:"hostName"`
	// Some exports carry a WireGuard config at the top level.
	Config string `json:"config"`
}

// FromVPNKey decodes an AmneziaVPN share key into tunnel configurations.
func FromVPNKey(key string) ([]Config, error) {
	payload, err := decodeVPNPayload(key)
	if err != nil {
		return nil, err
	}
	var k vpnKey
	if err := json.Unmarshal(payload, &k); err != nil {
		return nil, fmt.Errorf("vpn:// key does not contain JSON: %w", err)
	}
	name := k.Description
	if name == "" {
		name = k.HostName
	}
	if name == "" {
		name = "amnezia"
	}
	if LooksLikeConfig(k.Config) {
		return []Config{{Name: SanitizeName(name), Text: k.fill(k.Config)}}, nil
	}
	var out []Config
	var problems []string
	for _, c := range k.Containers {
		var containerName string
		json.Unmarshal(c["container"], &containerName)
		protoKey := ""
		switch {
		case strings.Contains(containerName, "awg"):
			protoKey = "awg"
		case strings.Contains(containerName, "wireguard"):
			protoKey = "wireguard"
		default:
			if containerName != "" {
				problems = append(problems, fmt.Sprintf("%s is not a WireGuard or AmneziaWG container", containerName))
			}
			continue
		}
		raw, ok := c[protoKey]
		if !ok {
			continue
		}
		text, err := k.containerConfig(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", containerName, err))
			continue
		}
		cname := name
		if len(k.Containers) > 1 {
			cname = name + "-" + strings.TrimPrefix(containerName, "amnezia-")
		}
		out = append(out, Config{Name: SanitizeName(cname), Text: text})
	}
	if len(out) == 0 {
		if len(problems) > 0 {
			return nil, errors.New("vpn:// key has no usable tunnel: " + strings.Join(problems, "; "))
		}
		return nil, errors.New("vpn:// key has no WireGuard or AmneziaWG configuration")
	}
	return out, nil
}

func (k *vpnKey) fill(text string) string {
	r := strings.NewReplacer("$PRIMARY_DNS", k.DNS1, "$SECONDARY_DNS", k.DNS2)
	text = r.Replace(strings.ReplaceAll(text, "\r\n", "\n"))
	// Drop a DNS line left with empty entries.
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "dns") {
			parts := strings.SplitN(l, "=", 2)
			if len(parts) == 2 {
				var keep []string
				for _, d := range strings.Split(parts[1], ",") {
					if d = strings.TrimSpace(d); d != "" {
						keep = append(keep, d)
					}
				}
				lines[i] = strings.TrimSpace(parts[0]) + " = " + strings.Join(keep, ", ")
				if len(keep) == 0 {
					lines[i] = ""
				}
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

func (k *vpnKey) containerConfig(raw json.RawMessage) (string, error) {
	var proto map[string]json.RawMessage
	if err := json.Unmarshal(raw, &proto); err != nil {
		return "", err
	}
	var lastConfig string
	if err := json.Unmarshal(proto["last_config"], &lastConfig); err != nil || lastConfig == "" {
		return "", errors.New("no client configuration (the key may be a server management key)")
	}
	var lc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastConfig), &lc); err != nil {
		if LooksLikeConfig(lastConfig) {
			return k.fill(lastConfig), nil
		}
		return "", err
	}
	var text string
	json.Unmarshal(lc["config"], &text)
	if LooksLikeConfig(text) {
		return k.fill(text), nil
	}
	return k.buildFromFields(lc, proto)
}

func str(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// buildFromFields assembles a config from the individual fields some
// AmneziaVPN versions store instead of the full text.
func (k *vpnKey) buildFromFields(lc, proto map[string]json.RawMessage) (string, error) {
	priv := str(lc, "client_priv_key")
	pub := str(lc, "server_pub_key")
	addr := str(lc, "client_ip")
	if priv == "" || pub == "" || addr == "" {
		return "", errors.New("configuration fields are incomplete")
	}
	host := str(lc, "hostName")
	if host == "" {
		host = k.HostName
	}
	port := str(lc, "port")
	if port == "" {
		port = str(proto, "port")
	}
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", priv)
	if !strings.Contains(addr, "/") {
		addr += "/32"
	}
	fmt.Fprintf(&b, "Address = %s\n", addr)
	var dns []string
	for _, d := range []string{k.DNS1, k.DNS2} {
		if d != "" {
			dns = append(dns, d)
		}
	}
	if len(dns) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(dns, ", "))
	}
	if mtu := str(lc, "mtu"); mtu != "" {
		fmt.Fprintf(&b, "MTU = %s\n", mtu)
	}
	for _, f := range []string{"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4", "I1", "I2", "I3", "I4", "I5"} {
		v := str(lc, f)
		if v == "" {
			v = str(proto, f)
		}
		if v != "" {
			fmt.Fprintf(&b, "%s = %s\n", f, v)
		}
	}
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", pub)
	if psk := str(lc, "psk_key"); psk != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", psk)
	}
	allowed := "0.0.0.0/0, ::/0"
	var list []string
	if json.Unmarshal(lc["allowed_ips"], &list) == nil && len(list) > 0 {
		allowed = strings.Join(list, ", ")
	}
	fmt.Fprintf(&b, "AllowedIPs = %s\n", allowed)
	if host != "" && port != "" {
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			host = "[" + host + "]"
		}
		fmt.Fprintf(&b, "Endpoint = %s:%s\n", host, port)
	}
	if ka := str(lc, "persistent_keep_alive"); ka != "" && ka != "0" {
		fmt.Fprintf(&b, "PersistentKeepalive = %s\n", ka)
	}
	return b.String(), nil
}

// EncodeVPNKey builds a vpn:// key around a wg-quick config, readable by
// this importer and by AmneziaVPN.
func EncodeVPNKey(name, configText, containerName string) (string, error) {
	if containerName == "" {
		containerName = "amnezia-awg"
	}
	protoKey := "awg"
	if strings.Contains(containerName, "wireguard") {
		protoKey = "wireguard"
	}
	lastConfig, err := json.Marshal(map[string]string{"config": configText})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{
		"containers": []map[string]any{{
			"container": containerName,
			protoKey: map[string]any{
				"isThirdPartyConfig": true,
				"last_config":        string(lastConfig),
				"transport_proto":    "udp",
			},
		}},
		"defaultContainer": containerName,
		"description":      name,
	})
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload)))
	buf.Write(size[:])
	zw := zlib.NewWriter(&buf)
	zw.Write(payload)
	zw.Close()
	return "vpn://" + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}
