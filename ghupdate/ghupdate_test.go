/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package ghupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.0.4", "1.0.3", true},
		{"v1.1.0", "1.0.9", true},
		{"1.0.10", "1.0.9", true},
		{"1.0.3", "1.0.3", false},
		{"1.0", "1.0.0", false},
		{"1.0.1", "1.0", true},
		{"1.0.2", "1.0.3", false},
		{"garbage", "1.0.0", false},
		{"2.0.0-rc1", "1.9.9", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDownloadVerifies(t *testing.T) {
	payload := []byte("msi bytes")
	sum := sha256.Sum256(payload)
	sums := hex.EncodeToString(sum[:]) + "  betteramnezia-amd64-1.2.0.msi\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.2.0","html_url":"https://example/r","assets":[
				{"name":"betteramnezia-amd64-1.2.0.msi","browser_download_url":"%[1]s/msi","size":9},
				{"name":"betteramnezia-x86-1.2.0.msi","browser_download_url":"%[1]s/bad","size":3},
				{"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/sums"}]}`, "http://"+r.Host)
		case "/msi":
			w.Write(payload)
		case "/bad":
			w.Write([]byte("bad"))
		case "/sums":
			w.Write([]byte(sums))
		}
	}))
	defer srv.Close()
	LatestURL = srv.URL + "/latest"
	r, err := Latest(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if r.Version() != "1.2.0" {
		t.Fatalf("version %q", r.Version())
	}
	dir := t.TempDir()
	a := r.Installer("amd64")
	if a == nil {
		t.Fatal("amd64 installer not found")
	}
	dst := filepath.Join(dir, "ok.msi")
	if err := r.Download(context.Background(), a, dst, "test", nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(payload) {
		t.Fatal("wrong content")
	}
	bad := filepath.Join(dir, "bad.msi")
	err = r.Download(context.Background(), r.Installer("x86"), bad, "test", nil)
	if err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
		t.Fatalf("unlisted file accepted: %v", err)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("failed download left behind")
	}
}
