//go:build !windows && !linux && !darwin

package main

// The macOS and Linux backends come next: they reuse internal/gesture, internal/lens, internal/settings
// and internal/icon, and only need to draw the lens and read the pointer (see docs/how-it-works.md, "Platforms").

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "Circle Lens desktop runs on Windows for now; macOS and Linux backends are planned.")
	os.Exit(1)
}
