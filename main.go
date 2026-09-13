// droidctl — unified CLI for OCI images (crane) + Droidspaces containers.
// Runs entirely from Termux (root via su). Go rewrite of the bash wrapper.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Global configuration (env-overridable, mirrors the bash version).
type Config struct {
	DS         string // droidspaces binary
	Crane      string // crane binary
	ImgDir     string // tarball dir
	RootfsDir  string // rootfs pool dir
	NetDefault string // default --net mode
}

var cfg Config

func init() {
	home, _ := os.UserHomeDir()
	cfg = Config{
		DS:         getenv("DROIDSPACES_BIN", "/data/local/Droidspaces/bin/droidspaces"),
		Crane:      getenv("CRANE_BIN", filepath.Join(home, "crane")),
		ImgDir:     getenv("DROIDCTL_IMGDIR", filepath.Join(home, "droidimages")),
		RootfsDir:  getenv("DROIDCTL_ROOTFS_DIR", "/data/local/tmp/droidimg"),
		NetDefault: getenv("DROIDCTL_NET", "host"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// imgName converts an image ref like "nginx:alpine" or "ghcr.io/x/y:z" into a
// safe filesystem/container name (same as bash: tr '/:' '--').
func imgName(ref string) string {
	r := strings.NewReplacer("/", "--", ":", "--")
	return r.Replace(ref)
}

const usage = `droidctl — unified CLI for OCI images (crane) + Droidspaces containers

Usage:
  droidctl pull <image>                 crane export --platform linux/arm64 -> tarball
  droidctl convert <image> [--cmd ...]  pull + extract + auto-prepare init (inittab, ds-init)
  droidctl images                       list extracted rootfs (convert pool)
  droidctl ps                           droidspaces show (running containers)
  droidctl create <name> <image>        validate convert-pool rootfs before start
  droidctl start <name> [--net=...]     start container (default --net=host; --rootfs auto)
  droidctl stop <name>                  stop container
  droidctl restart <name>               restart container
  droidctl logs <name>                  tail container / daemon logs
  droidctl exec <name> <cmd...>         run command inside container (non-interactive)
  droidctl info <name>                  container info
  droidctl check                        droidspaces kernel diagnostics
  droidctl rm <name>                    stop + delete container + its rootfs
  droidctl version                      show versions (droidspaces, crane)
  droidctl help                         this message

Env overrides:
  DROIDSPACES_BIN   droidspaces binary (default /data/local/Droidspaces/bin/droidspaces)
  CRANE_BIN         crane binary (default $HOME/crane)
  DROIDCTL_IMGDIR   tarball dir (default $HOME/droidimages)
  DROIDCTL_ROOTFS_DIR rootfs dir (default /data/local/tmp/droidimg)
  DROIDCTL_NET      default net mode (default host)
`

func main() {
	args := os.Args[1:]
	cmd := "help"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	var err error
	switch cmd {
	case "pull":
		err = cmdPull(args)
	case "convert":
		err = cmdConvert(args)
	case "images":
		err = cmdImages(args)
	case "ps":
		err = cmdPS(args)
	case "create":
		err = cmdCreate(args)
	case "start":
		err = cmdStart(args)
	case "stop":
		err = cmdStop(args)
	case "restart":
		err = cmdRestart(args)
	case "logs":
		err = cmdLogs(args)
	case "exec":
		err = cmdExec(args)
	case "info":
		err = cmdInfo(args)
	case "check":
		err = cmdCheck(args)
	case "rm":
		err = cmdRM(args)
	case "version":
		err = cmdVersion(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q (see: droidctl help)", cmd)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "droidctl: ERROR: %v\n", err)
		os.Exit(1)
	}
}