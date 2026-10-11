package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// answer is a latest-release reply with an extension asset and the setup asset.
func answer(tag, digest, url string, size int64, extra string) []byte {
	return []byte(fmt.Sprintf(`{"tag_name":%[1]q,"html_url":"https://github.com/fauzulkc/circle-lens/releases/tag/%[1]s",%[5]s"assets":[
		{"name":"circle-lens-extension-%[6]s.zip","size":10,"digest":"sha256:00","browser_download_url":%[3]q},
		{"name":"CircleLens-Setup-%[6]s.exe","size":%[4]d,"digest":%[2]q,"browser_download_url":%[3]q}]}`,
		tag, digest, url, size, extra, strings.TrimPrefix(tag, "v")))
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.5.1", "0.5.0", true},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.5.0", "0.5.0", false},
		{"0.4.9", "0.5.0", false},
		{"0.5.1", "dev", false},
		{"0.5.1-beta", "0.5.0", false},
		{"0.05.1", "0.5.0", false},
		{"0.5", "0.4.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestLatestFindsTheSetup(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	url := Downloads + "v0.5.1/CircleLens-Setup-0.5.1.exe"
	release, err := Latest(answer("v0.5.1", "sha256:"+digest, url, 2843136, ""), "0.5.0", Downloads)
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "0.5.1" || release.Setup.URL != url || release.Setup.Size != 2843136 || hex.EncodeToString(release.Setup.SHA256) != digest {
		t.Fatalf("unexpected release %+v", release)
	}
	if !strings.HasSuffix(release.Page, "/tag/v0.5.1") {
		t.Fatalf("page %q", release.Page)
	}
}

func TestLatestRefusesWhatItCannotTrust(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	good := Downloads + "v0.5.1/CircleLens-Setup-0.5.1.exe"
	cases := map[string][]byte{
		"no digest":           answer("v0.5.1", "", good, 100, ""),
		"short digest":        answer("v0.5.1", "sha256:abcd", good, 100, ""),
		"md5 digest":          answer("v0.5.1", "md5:"+strings.Repeat("ab", 16), good, 100, ""),
		"another host":        answer("v0.5.1", digest, "https://example.com/CircleLens-Setup-0.5.1.exe", 100, ""),
		"another repo":        answer("v0.5.1", digest, "https://github.com/someone/circle-lens/releases/download/v0.5.1/CircleLens-Setup-0.5.1.exe", 100, ""),
		"plain http":          answer("v0.5.1", digest, "http://github.com/fauzulkc/circle-lens/releases/download/v0.5.1/x.exe", 100, ""),
		"too big":             answer("v0.5.1", digest, good, MaxSetupSize+1, ""),
		"empty":               answer("v0.5.1", digest, good, 0, ""),
		"odd tag":             answer("latest", digest, good, 100, ""),
		"not json":            []byte("<html>rate limited</html>"),
		"missing setup asset": []byte(`{"tag_name":"v0.5.1","assets":[]}`),
	}
	for name, data := range cases {
		if _, err := Latest(data, "0.5.0", Downloads); err == nil || errors.Is(err, ErrUpToDate) {
			t.Errorf("%s: want a refusal, got %v", name, err)
		}
	}
}

func TestLatestIsUpToDate(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	for name, data := range map[string][]byte{
		"same version":  answer("v0.5.0", digest, Downloads+"v0.5.0/CircleLens-Setup-0.5.0.exe", 100, ""),
		"older version": answer("v0.4.0", digest, Downloads+"v0.4.0/CircleLens-Setup-0.4.0.exe", 100, ""),
		"pre-release":   answer("v0.6.0", digest, Downloads+"v0.6.0/CircleLens-Setup-0.6.0.exe", 100, `"prerelease":true,`),
		"draft":         answer("v0.6.0", digest, Downloads+"v0.6.0/CircleLens-Setup-0.6.0.exe", 100, `"draft":true,`),
	} {
		if _, err := Latest(data, "0.5.0", Downloads); !errors.Is(err, ErrUpToDate) {
			t.Errorf("%s: want up to date, got %v", name, err)
		}
	}
	// A local build ("dev") never updates.
	if _, err := Latest(answer("v9.0.0", digest, Downloads+"v9.0.0/CircleLens-Setup-9.0.0.exe", 100, ""), "dev", Downloads); !errors.Is(err, ErrUpToDate) {
		t.Errorf("dev build: want up to date, got %v", err)
	}
}

func TestVerify(t *testing.T) {
	dir := t.TempDir()
	content := append([]byte("MZ"), []byte(strings.Repeat("program bytes ", 1000))...)
	sum := sha256.Sum256(content)
	path := filepath.Join(dir, "setup.exe")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	asset := Asset{Size: int64(len(content)), SHA256: sum[:]}
	if err := Verify(path, asset); err != nil {
		t.Fatalf("a matching file was refused: %v", err)
	}
	if err := Verify(path, Asset{Size: asset.Size + 1, SHA256: sum[:]}); err == nil {
		t.Error("a wrong size was accepted")
	}
	tampered := append([]byte{}, content...)
	tampered[100] ^= 1
	_ = os.WriteFile(path, tampered, 0o644)
	if err := Verify(path, asset); err == nil {
		t.Error("a changed file was accepted")
	}
	notProgram := append([]byte("PK"), content[2:]...)
	other := sha256.Sum256(notProgram)
	_ = os.WriteFile(path, notProgram, 0o644)
	if err := Verify(path, Asset{Size: int64(len(notProgram)), SHA256: other[:]}); err == nil {
		t.Error("a file that is not a Windows program was accepted")
	}
}
