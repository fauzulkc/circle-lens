//go:build windows

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestReleaseNotesMatchTheChangelog: whatsnew.md (shown as "What's new") is this version's CHANGELOG
// section. desktop/build.mjs writes it; if this fails, run `npm run build:desktop` and commit it.
func TestReleaseNotesMatchTheChangelog(t *testing.T) {
	data, err := os.ReadFile("../../../package.json")
	if err != nil {
		t.Skip("no package.json beside the desktop folder")
	}
	var root struct{ Version string }
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	changelog, err := os.ReadFile("../../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(changelog), "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
		if start < 0 && strings.TrimSpace(line) == "## "+root.Version {
			start = i
		} else if start >= 0 && strings.HasPrefix(line, "## ") {
			end = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("CHANGELOG.md has no section for %s", root.Version)
	}
	want := strings.TrimSpace(strings.Join(lines[start+1:end], "\n")) + "\n"
	if releaseNotes != want {
		t.Fatalf("whatsnew.md is not the %s section of CHANGELOG.md: run npm run build:desktop", root.Version)
	}
}

func TestPlainMarkdown(t *testing.T) {
	got := plainMarkdown("**Fixed:** a `Ctrl+Num+` shortcut; see [RELEASING.md](RELEASING.md).")
	if got != "Fixed: a Ctrl+Num+ shortcut; see RELEASING.md." {
		t.Fatalf("got %q", got)
	}
}
