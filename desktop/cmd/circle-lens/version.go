package main

// version is set at build time: -ldflags "-X main.version=…".
var version = "dev"

// instance names the single-instance lock and window classes. A test build sets its own
// (-X main.instance=…) so it can run beside the copy in daily use.
var instance = "CircleLens"
