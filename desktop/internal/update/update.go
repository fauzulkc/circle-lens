// Package update reads GitHub's answer about the latest release and checks a downloaded setup file
// against it. It does no networking itself: the OS backend fetches, this package decides what is safe to
// install. A release qualifies only if it is newer than the running version, published (not a draft or
// pre-release), and has the setup asset under the expected download address, with its size and SHA-256
// digest, so a download that does not match exactly is never run.
package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Feed is GitHub's latest-release endpoint for Circle Lens.
const Feed = "https://api.github.com/repos/fauzulkc/circle-lens/releases/latest"

// Downloads is where release assets must come from.
const Downloads = "https://github.com/fauzulkc/circle-lens/releases/download/"

// MaxSetupSize caps a setup download (the exe is about 3 MB).
const MaxSetupSize = 64 << 20

// SetupName is the asset the Windows app updates from.
func SetupName(version string) string { return "CircleLens-Setup-" + version + ".exe" }

// Release is a newer version that can be installed.
type Release struct {
	Version string // "0.5.1"
	Page    string // the release's page, for "what's new"
	Setup   Asset
}

// Asset is one downloadable file of a release.
type Asset struct {
	Name   string
	URL    string
	Size   int64
	SHA256 []byte
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	HTMLURL    string `json:"html_url"`
	Assets     []struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
		URL    string `json:"browser_download_url"`
	} `json:"assets"`
}

// ErrUpToDate means the latest release is not newer than the running version.
var ErrUpToDate = errors.New("up to date")

// Latest reads the latest-release JSON. It returns ErrUpToDate when there is nothing newer than
// `current`, and another error when the answer cannot be trusted or used. Asset addresses must start
// with `downloads`.
func Latest(data []byte, current, downloads string) (Release, error) {
	var answer githubRelease
	if err := json.Unmarshal(data, &answer); err != nil {
		return Release{}, fmt.Errorf("unreadable answer: %w", err)
	}
	if answer.Draft || answer.Prerelease {
		return Release{}, ErrUpToDate
	}
	version := strings.TrimPrefix(answer.TagName, "v")
	if _, ok := parse(version); !ok {
		return Release{}, fmt.Errorf("unexpected version %q", answer.TagName)
	}
	if !Newer(version, current) {
		return Release{}, ErrUpToDate
	}
	name := SetupName(version)
	for _, asset := range answer.Assets {
		if asset.Name != name {
			continue
		}
		if !strings.HasPrefix(asset.URL, downloads) {
			return Release{}, fmt.Errorf("%s is not served from the expected place", name)
		}
		if asset.Size <= 0 || asset.Size > MaxSetupSize {
			return Release{}, fmt.Errorf("%s has an unexpected size", name)
		}
		digest, ok := strings.CutPrefix(asset.Digest, "sha256:")
		sum, err := hex.DecodeString(digest)
		if !ok || err != nil || len(sum) != sha256.Size {
			return Release{}, fmt.Errorf("%s has no SHA-256 digest", name)
		}
		return Release{Version: version, Page: answer.HTMLURL, Setup: Asset{Name: name, URL: asset.URL, Size: asset.Size, SHA256: sum}}, nil
	}
	return Release{}, fmt.Errorf("release %s has no %s", version, name)
}

// Verify checks a downloaded file is exactly the asset: same size, same SHA-256, and a Windows program.
func Verify(path string, asset Asset) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	head := make([]byte, 2)
	if _, err := io.ReadFull(file, head); err != nil || !bytes.Equal(head, []byte("MZ")) {
		return errors.New("the download is not a Windows program")
	}
	hash.Write(head)
	read, err := io.Copy(hash, io.LimitReader(file, MaxSetupSize))
	if err != nil {
		return err
	}
	if read+2 != asset.Size {
		return fmt.Errorf("the download is %d bytes, expected %d", read+2, asset.Size)
	}
	if !bytes.Equal(hash.Sum(nil), asset.SHA256) {
		return errors.New("the download does not match the release's SHA-256 digest")
	}
	return nil
}

// Newer reports whether version a (like "0.5.1") is later than b. A b that is not a plain version
// ("dev", a local build) is never updated.
func Newer(a, b string) bool {
	x, okA := parse(a)
	y, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// Valid reports whether version is a plain X.Y.Z version, the only kind that updates.
func Valid(version string) bool {
	_, ok := parse(version)
	return ok
}

func parse(version string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || part != strconv.Itoa(n) {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
