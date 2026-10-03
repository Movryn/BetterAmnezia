/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

// Package ghupdate finds new BetterAmnezia releases on GitHub and downloads
// their installers, verified against the release's SHA256SUMS.txt.
package ghupdate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// LatestURL is the GitHub API endpoint for the newest published release.
var LatestURL = "https://api.github.com/repos/Movryn/betteramnezia/releases/latest"

// Asset is a file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is the subset of the GitHub release object we use.
type Release struct {
	Tag     string  `json:"tag_name"`
	Name    string  `json:"name"`
	Notes   string  `json:"body"`
	PageURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
	Draft   bool    `json:"draft"`
	Pre     bool    `json:"prerelease"`
}

// Version is the release version without the leading "v".
func (r *Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

func (r *Release) asset(name string) *Asset {
	for i := range r.Assets {
		if strings.EqualFold(r.Assets[i].Name, name) {
			return &r.Assets[i]
		}
	}
	return nil
}

// Installer returns the MSI for the given architecture ("amd64", "arm64",
// "x86").
func (r *Release) Installer(arch string) *Asset {
	return r.asset(fmt.Sprintf("betteramnezia-%s-%s.msi", arch, r.Version()))
}

var client = &http.Client{Timeout: 30 * time.Second}

func get(ctx context.Context, url, userAgent string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

// Latest fetches the newest published release.
func Latest(ctx context.Context, userAgent string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check failed: %s", resp.Status)
	}
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if r.Tag == "" || r.Draft || r.Pre {
		return nil, errors.New("no published release found")
	}
	return &r, nil
}

func parse(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// Newer reports whether version a is newer than b. Unparsable versions are
// never newer.
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// expectedHash reads the checksum of name from a sha256sum style list.
func expectedHash(sums io.Reader, name string) (string, error) {
	sc := bufio.NewScanner(io.LimitReader(sums, 1<<20))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimPrefix(fields[1], "*"), name) && len(fields[0]) == 64 {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s is not listed in SHA256SUMS.txt", name)
}

// Progress is called while downloading.
type Progress func(downloaded, total int64)

// Download saves asset to dst and verifies it against the release's
// SHA256SUMS.txt. On error dst is removed.
func (r *Release) Download(ctx context.Context, a *Asset, dst, userAgent string, progress Progress) (err error) {
	sumsAsset := r.asset("SHA256SUMS.txt")
	if sumsAsset == nil {
		return errors.New("the release has no SHA256SUMS.txt")
	}
	sumsResp, err := get(ctx, sumsAsset.URL, userAgent)
	if err != nil {
		return err
	}
	want, err := expectedHash(sumsResp.Body, a.Name)
	sumsResp.Body.Close()
	if err != nil {
		return err
	}

	resp, err := get(ctx, a.URL, userAgent)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(dst)
		}
	}()
	total := resp.ContentLength
	if total <= 0 {
		total = a.Size
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err = f.Write(buf[:n]); err != nil {
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
			if done > 512<<20 {
				return errors.New("download is unexpectedly large")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.New("the downloaded installer does not match its checksum")
	}
	return f.Close()
}
