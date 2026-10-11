//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"circlelens/internal/update"
)

// TestRealReleaseDownloads asks GitHub about the latest release, as an installed copy does, and checks
// the setup it offers downloads and matches its digest. It needs the network, so it only runs with
// CIRCLE_LENS_NETWORK_TESTS=1.
func TestRealReleaseDownloads(t *testing.T) {
	if os.Getenv("CIRCLE_LENS_NETWORK_TESTS") != "1" {
		t.Skip("set CIRCLE_LENS_NETWORK_TESTS=1 to run")
	}
	var answer bytes.Buffer
	if err := fetch(update.Feed, "application/vnd.github+json", updateFeedLimit, &answer); err != nil {
		t.Fatal(err)
	}
	release, err := update.Latest(answer.Bytes(), "0.0.1", update.Downloads)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("latest is %s, %s (%d bytes)", release.Version, release.Setup.URL, release.Setup.Size)
	path := filepath.Join(t.TempDir(), release.Setup.Name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = fetch(release.Setup.URL, "application/octet-stream", release.Setup.Size, file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := update.Verify(path, release.Setup); err != nil {
		t.Fatal(err)
	}
}
