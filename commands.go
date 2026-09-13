package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------- pull ----------

func cmdPull(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl pull <image>")
	}
	crane, err := needCrane()
	if err != nil {
		return err
	}
	ref := args[0]
	img := imgName(ref)
	if err := os.MkdirAll(cfg.ImgDir, 0o755); err != nil {
		return err
	}
	fmt.Printf("droidctl: pulling %s (linux/arm64) ...\n", ref)
	out, err := craneRun(crane, "export", "--platform", "linux/arm64", ref, "-")
	if err != nil {
		return fmt.Errorf("pull failed: %v (DNS? run DNSResolve module so crane works in Termux): %s", err, strings.TrimSpace(out))
	}
	tarPath := filepath.Join(cfg.ImgDir, img+".tar")
	if err := os.WriteFile(tarPath, []byte(out), 0o644); err != nil {
		return err
	}
	size := humanSize(int64(len(out)))
	fmt.Printf("droidctl: saved %s (%s)\n", tarPath, size)
	return nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ---------- convert ----------

func cmdConvert(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl convert <image> [--cmd \"entrypoint args\"]")
	}
	ref := args[0]
	rest := args[1:]
	cmdStr := ""
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--cmd" && i+1 < len(rest) {
			cmdStr = rest[i+1]
			i++
		}
	}
	img := imgName(ref)

	// 1. pull if tarball missing
	tarPath := filepath.Join(cfg.ImgDir, img+".tar")
	if _, err := os.Stat(tarPath); err != nil {
		if err := cmdPull([]string{ref}); err != nil {
			return err
		}
	}

	// 2. extract to rootfs dir
	rootfs := filepath.Join(cfg.RootfsDir, img)
	if dirNonEmpty(rootfs) {
		fmt.Printf("droidctl: rootfs already exists at %s (skipping extract)\n", rootfs)
	} else {
		fmt.Printf("droidctl: extracting to %s ...\n", rootfs)
		if _, err := runSu("mkdir", "-p", "'"+rootfs+"'"); err != nil {
			return err
		}
		// /data/local/tmp is ext4 → hardlinks OK; use GNU tar from Termux with
		// --hard-dereference (toybox tar lacks it).
		tarCmd := fmt.Sprintf("PATH=/data/data/com.termux/files/usr/bin:$PATH tar --hard-dereference -xf '%s' -C '%s'", tarPath, rootfs)
		if out, err := runSu(tarCmd); err != nil {
			return fmt.Errorf("extract failed: %v: %s", err, out)
		}
		// fix ownership so we can inspect later
		runSu("chown", "-R", "$(stat -c '%u:%g' '"+cfg.ImgDir+"')", "'"+rootfs+"'", "2>/dev/null", "||", "true")
	}

	// verify architecture of a key binary (best effort)
	probe := findFirstExecutable(rootfs)
	if probe != "" {
		arch, err := detectArch(probe)
		if err == nil {
			switch arch {
			case "aarch64", "arm64":
				fmt.Println("droidctl: arch OK (arm64)")
			case "x86-64":
				return fmt.Errorf("arch mismatch: %s is x86-64. This image has no arm64 variant (or crane old)", probe)
			default:
				fmt.Printf("droidctl: note: could not detect arch of %s\n", probe)
			}
		}
	}

	// 3. prepare init (inittab + ds-init) — the gotcha we learned
	if cmdStr == "" {
		cmdStr = detectEntrypoint(img, rootfs)
	}
	if err := writeDsInit(rootfs, cmdStr); err != nil {
		return err
	}

	// busybox-init images: replace inittab if it references openrc (missing in images)
	fixInittab(rootfs, img)

	// Ensure /sbin/init exists (droidspaces requires it). For busybox-style
	// images, link it to the busybox binary (multi-call).
	if err := ensureSbinInit(rootfs); err != nil {
		return err
	}

	fmt.Printf("droidctl: convert OK — rootfs at %s\n", rootfs)
	fmt.Printf("droidctl: next -> droidctl start %s\n", img)
	return nil
}

func dirNonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// detectArch uses `file` on a binary (best effort; requires `file` installed).
func detectArch(path string) (string, error) {
	out, err := runSu("file", "'"+path+"'")
	if err != nil {
		return "", err
	}
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "aarch64"), strings.Contains(lower, "arm64"):
		return "arm64", nil
	case strings.Contains(lower, "x86-64"), strings.Contains(lower, "x86_64"):
		return "x86-64", nil
	}
	return "", fmt.Errorf("unknown arch in %q", out)
}

// detectEntrypoint returns a run command for known images, or "" if unknown.
func detectEntrypoint(img, rootfs string) string {
	switch {
	case strings.Contains(img, "nginx"):
		return "nginx -g 'daemon off;'"
	case strings.Contains(img, "redis"):
		return "redis-server --daemonize no"
	}
	for _, p := range []string{
		filepath.Join(rootfs, "docker-entrypoint.sh"),
		filepath.Join(rootfs, "usr/local/bin/docker-entrypoint.sh"),
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return "/" + strings.TrimPrefix(p, rootfs+"/")
		}
	}
	return ""
}

// writeDsInit writes /usr/local/bin/ds-init. Empty cmdStr → idle loop so
// `droidctl exec` works on images without a long-running entrypoint.
func writeDsInit(rootfs, cmdStr string) error {
	var body string
	if cmdStr != "" {
		body = fmt.Sprintf(`#!/bin/sh
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
exec %s
`, cmdStr)
	} else {
		body = `#!/bin/sh
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
echo "[droidctl] no entrypoint detected; idle loop (use: droidctl exec <name> <cmd>)"
exec /bin/sh -c 'while :; do sleep 3600; done'
`
	}
	script := filepath.Join(rootfs, "usr/local/bin", "ds-init")
	// must write as root (rootfs is root-owned)
	cmdStr2 := fmt.Sprintf("mkdir -p '%s/usr/local/bin' && cat > '%s' <<'EOF'\n%sEOF\nchmod +x '%s'", rootfs, script, body, script)
	_, err := runSu(cmdStr2)
	if err != nil {
		return fmt.Errorf("gagal tulis ds-init: %v", err)
	}
	return nil
}

// fixInittab replaces openrc-style inittab (openrc missing in images), or writes
// a minimal one when the image has none.
func fixInittab(rootfs, img string) {
	inittab := filepath.Join(rootfs, "etc/inittab")
	if out, err := runSu("grep", "-q", "openrc", "'"+inittab+"'"); err == nil {
		_ = out
		newtab := "::sysinit:/usr/local/bin/ds-init\n::shutdown:/bin/busybox poweroff\n"
		runSu(fmt.Sprintf("cat > '%s' <<'EOF'\n%sEOF", inittab, newtab))
		fmt.Println("droidctl: replaced openrc inittab with minimal one")
	}
}

// ensureSbinInit links /sbin/init -> busybox when missing (droidspaces needs it).
func ensureSbinInit(rootfs string) error {
	sbinInit := filepath.Join(rootfs, "sbin/init")
	if _, err := runSu("test", "-e", "'"+sbinInit+"'"); err == nil {
		return nil
	}
	for _, bb := range []string{"bin/busybox", "usr/bin/busybox"} {
		p := filepath.Join(rootfs, bb)
		if _, err := runSu("test", "-e", "'"+p+"'"); err == nil {
			linkTarget := "/" + bb
			runSu(fmt.Sprintf("mkdir -p '%s/sbin' && ln -sf %s '%s'", rootfs, linkTarget, sbinInit))
			fmt.Printf("droidctl: linked /sbin/init -> %s\n", linkTarget)
			return nil
		}
	}
	return nil
}

// ---------- images ----------

func cmdImages(args []string) error {
	if _, err := os.Stat(cfg.RootfsDir); err != nil {
		fmt.Printf("(no rootfs pool yet: %s)\n", cfg.RootfsDir)
		return nil
	}
	fmt.Printf("=== rootfs pool (%s) ===\n", cfg.RootfsDir)
	entries, _ := os.ReadDir(cfg.RootfsDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dir := filepath.Join(cfg.RootfsDir, name)
		size := dirSize(dir)
		init := "n/a"
		if _, err := os.Stat(filepath.Join(dir, "usr/local/bin/ds-init")); err == nil {
			init = "ds-init"
		}
		if _, err := os.Stat(filepath.Join(dir, "sbin/init")); err == nil {
			init += "+init"
		}
		fmt.Printf("  %-24s %8s  %s\n", name, humanSize(size), init)
	}
	return nil
}

func dirSize(dir string) int64 {
	var total int64
	filepathWalk(dir, func(p string, info os.FileInfo) bool {
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return true
	})
	return total
}

// ---------- ps ----------

func cmdPS(args []string) error {
	if err := needRootDS(); err != nil {
		return err
	}
	out, err := runSu(cfg.DS, "show")
	if err != nil {
		return fmt.Errorf("droidspaces show failed / no daemon: %v", err)
	}
	fmt.Println(out)
	return nil
}

// ---------- create ----------

func cmdCreate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: droidctl create <name> <image>")
	}
	name := args[0]
	img := imgName(args[1])
	rootfs := filepath.Join(cfg.RootfsDir, img)
	if _, err := os.Stat(rootfs); err != nil {
		return fmt.Errorf("rootfs for '%s' not found — run: droidctl convert %s", img, args[1])
	}
	fmt.Printf("droidctl: container '%s' will use rootfs %s\n", name, rootfs)
	fmt.Printf("droidctl: start with -> droidctl start %s\n", name)
	return nil
}

// ---------- start ----------

func cmdStart(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl start <name> [--net=host|nat|none] [--port X:Y] [--rootfs=PATH]")
	}
	if err := needRootDS(); err != nil {
		return err
	}
	name := args[0]
	rest := args[1:]
	net := cfg.NetDefault
	port := ""
	rootfs := ""
	extra := []string{}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case strings.HasPrefix(a, "--net="):
			net = strings.TrimPrefix(a, "--net=")
		case strings.HasPrefix(a, "--port"), a == "--port":
			if strings.HasPrefix(a, "--port=") {
				port = "--port " + strings.TrimPrefix(a, "--port=")
			} else if i+1 < len(rest) {
				port = "--port " + rest[i+1]
				i++
			}
		case strings.HasPrefix(a, "--rootfs="):
			rootfs = strings.TrimPrefix(a, "--rootfs=")
		default:
			extra = append(extra, a)
		}
	}
	// Auto-resolve rootfs from the conversion pool if container name matches an image
	if rootfs == "" {
		if p := filepath.Join(cfg.RootfsDir, name); dirNonEmpty(p) {
			rootfs = p
			fmt.Printf("droidctl: using rootfs from pool: %s\n", rootfs)
		}
	}
	dsArgs := []string{cfg.DS, "--name=" + name, "--net=" + net}
	if rootfs != "" {
		dsArgs = append(dsArgs, "--rootfs='"+rootfs+"'")
	}
	if port != "" {
		dsArgs = append(dsArgs, port)
	}
	if len(extra) > 0 {
		dsArgs = append(dsArgs, extra...)
	}
	dsArgs = append(dsArgs, "start")
	out, err := runSu(strings.Join(dsArgs, " "))
	if err != nil {
		return fmt.Errorf("start failed: %v: %s", err, out)
	}
	fmt.Println(out)
	return nil
}

// ---------- stop / restart ----------

func cmdStop(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl stop <name>")
	}
	if err := needRootDS(); err != nil {
		return err
	}
	out, _ := runSu(cfg.DS, "--name="+args[0], "stop")
	fmt.Println(out)
	return nil
}

func cmdRestart(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl restart <name>")
	}
	if err := needRootDS(); err != nil {
		return err
	}
	out, _ := runSu(cfg.DS, "--name="+args[0], "restart")
	fmt.Println(out)
	return nil
}

// ---------- logs ----------

func cmdLogs(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl logs <name>")
	}
	if err := needRootDS(); err != nil {
		return err
	}
	name := args[0]
	fmt.Printf("=== daemon log (droidspacesd.log, grep %s) ===\n", name)
	out, _ := runSu(fmt.Sprintf("tail -n 200 /data/local/Droidspaces/Logs/droidspacesd.log 2>/dev/null | grep -i '%s' | tail -25", name))
	if out != "" {
		fmt.Println(out)
	}
	fmt.Println("=== container log files ===")
	out, _ = runSu(fmt.Sprintf("ls -t /data/local/Droidspaces/Logs/*%s* 2>/dev/null | head -3", name))
	if out != "" {
		fmt.Println(out)
	}
	out, _ = runSu(fmt.Sprintf("for f in /data/local/Droidspaces/Logs/*%s*; do [ -f \"$f\" ] && { echo \"--- $f ---\"; tail -n 20 \"$f\"; }; done", name))
	if out != "" {
		fmt.Println(out)
	}
	return nil
}

// ---------- exec ----------

func cmdExec(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: droidctl exec <name> <cmd...>")
	}
	if err := needRootDS(); err != nil {
		return err
	}
	name := args[0]
	joined := strings.Join(args[1:], " ")
	escaped := strings.ReplaceAll(joined, "'", "'\\''")
	out, err := runSu(cfg.DS, "--name="+name, "run", "sh", "-c", "'"+escaped+"'")
	if err != nil {
		return fmt.Errorf("exec failed: %v: %s", err, out)
	}
	fmt.Println(out)
	return nil
}

// ---------- info ----------

func cmdInfo(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl info <name>")
	}
	if err := needRootDS(); err != nil {
		return err
	}
	out, err := runSu(cfg.DS, "--name="+args[0], "info")
	if err != nil {
		return fmt.Errorf("info failed: %v", err)
	}
	fmt.Println(out)
	return nil
}

// ---------- check ----------

func cmdCheck(args []string) error {
	if err := needRootDS(); err != nil {
		return err
	}
	out, err := runSu(cfg.DS, "check")
	if err != nil {
		return fmt.Errorf("check failed: %v", err)
	}
	fmt.Println(out)
	return nil
}

// ---------- rm ----------

func cmdRM(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: droidctl rm <name>")
	}
	if err := needRootDS(); err != nil {
		return err
	}
	name := args[0]
	runSu(cfg.DS, "--name="+name, "stop") // ignore errors
	runSu(fmt.Sprintf("rm -rf /data/local/Droidspaces/Containers/%s /data/local/Droidspaces/Pids/%s 2>/dev/null", name, name))
	// also remove from the conversion pool if the name matches a pool image (rm as root)
	poolPath := filepath.Join(cfg.RootfsDir, name)
	if out, err := runSu("rm", "-rf", "'"+poolPath+"'"); err == nil {
		_ = out
		fmt.Printf("droidctl: removed rootfs %s\n", poolPath)
	}
	fmt.Printf("droidctl: container '%s' removed\n", name)
	return nil
}

// ---------- version ----------

func cmdVersion(args []string) error {
	if err := needRootDS(); err != nil {
		return err
	}
	fmt.Println("droidctl (crane + droidspaces wrapper) — Go")
	dsv, _ := runSu(cfg.DS, "version")
	if dsv != "" {
		fmt.Println("  droidspaces: " + strings.SplitN(dsv, "\n", 2)[0])
	}
	crane, err := needCrane()
	if err == nil {
		cv, _ := craneRun(crane, "version")
		if cv != "" {
			fmt.Println("  crane:       " + strings.TrimSpace(cv))
		}
	}
	fmt.Printf("  imgdir:      %s\n", cfg.ImgDir)
	fmt.Printf("  rootfs dir:  %s\n", cfg.RootfsDir)
	return nil
}