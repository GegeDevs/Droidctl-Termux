package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runSu executes a command as root via su, returning trimmed combined output.
func runSu(args ...string) (string, error) {
	// su -c expects a single shell string; join args with spaces.
	cmdStr := strings.Join(args, " ")
	cmd := exec.Command("su", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runSuOutput is like runSu but keeps raw output (no trim) for log tails.
func runSuOutput(args ...string) (string, error) {
	cmdStr := strings.Join(args, " ")
	cmd := exec.Command("su", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// needRootDS verifies the droidspaces binary exists (as root).
func needRootDS() error {
	_, err := runSu("test", "-x", "'"+cfg.DS+"'")
	if err != nil {
		return fmt.Errorf("droidspaces binary not found at %s (need root)", cfg.DS)
	}
	return nil
}

// needCrane resolves the crane binary (explicit path or PATH).
func needCrane() (string, error) {
	if _, err := os.Stat(cfg.Crane); err == nil {
		return cfg.Crane, nil
	}
	if p, err := exec.LookPath("crane"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("crane not found. Put the binary at %s or set CRANE_BIN (get it from github.com/google/go-containerregistry)", cfg.Crane)
}

// craneRun runs crane with args, returning combined output.
func craneRun(crane string, args ...string) (string, error) {
	cmd := exec.Command(crane, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// findFirstExecutable returns the first executable regular file under rootfs
// (used for architecture probing). Returns "" if none.
func findFirstExecutable(rootfs string) string {
	var found string
	filepathWalk(rootfs, func(path string, info os.FileInfo) bool {
		if found != "" {
			return false
		}
		if info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			found = path
			return false
		}
		return true
	})
	return found
}

// filepathWalk is a tiny filepath.Walk stand-in (no external deps) that stops
// when the callback returns false.
func filepathWalk(root string, fn func(path string, info os.FileInfo) bool) {
	filepathWalkRec(root, fn)
}

func filepathWalkRec(path string, fn func(p string, info os.FileInfo) bool) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return true
	}
	for _, e := range entries {
		p := path + "/" + e.Name()
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !fn(p, info) {
			return false
		}
		if info.IsDir() {
			if !filepathWalkRec(p, fn) {
				return false
			}
		}
	}
	return true
}