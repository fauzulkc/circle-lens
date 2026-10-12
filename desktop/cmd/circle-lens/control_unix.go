//go:build linux || darwin

package main

// One copy at a time, and commands to it (-toggle, -quit), through a Unix socket that only this user
// can reach: the running copy listens on it, and a second start sends it a word and exits.

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type controlServer struct {
	listener net.Listener
	path     string
}

// controlPath is the socket's place: the runtime folder on Linux, the user's cache folder on macOS.
func controlPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir, _ = os.UserCacheDir()
	}
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, instance+".sock")
}

// sendCommand tells the running copy something; false if none is running.
func sendCommand(command string) bool {
	conn, err := net.DialTimeout("unix", controlPath(), time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write([]byte(command + "\n"))
	return err == nil
}

// waitUntilGone waits for the running copy to let go of the socket.
func waitUntilGone(limit time.Duration) {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		conn, err := net.Dial("unix", controlPath())
		if err != nil {
			return
		}
		conn.Close()
	}
}

// listenForCommands takes the socket (removing one a crashed copy left behind).
func listenForCommands() (*controlServer, error) {
	path := controlPath()
	os.MkdirAll(filepath.Dir(path), 0o700)
	if conn, err := net.Dial("unix", path); err == nil {
		conn.Close()
		return nil, os.ErrExist
	}
	os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	os.Chmod(path, 0o600)
	return &controlServer{listener: listener, path: path}, nil
}

// serve hands each command to handle, from its own goroutine.
func (s *controlServer) serve(handle func(string)) {
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			line, _ := bufio.NewReader(conn).ReadString('\n')
			conn.Close()
			switch command := strings.TrimSpace(line); command {
			case "toggle", "open", "quit":
				handle(command)
			}
		}
	}()
}

func (s *controlServer) close() {
	s.listener.Close()
	os.Remove(s.path)
}
