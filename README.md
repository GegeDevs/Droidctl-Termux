# Droidctl-Termux

**Unified CLI for running Docker images as native Droidspaces containers on Android — entirely from Termux, no Docker daemon needed.**

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Termux-green.svg)](https://termux.dev/)
[![Requires](https://img.shields.io/badge/Requires-Root-orange.svg)](https://kernelsu.org/)

## Why?

Running Docker on Android is painful (kernel cgroup gaps, `/bin/mount` issues, resource waste). Instead, **convert OCI images to plain rootfs and run them natively with [Droidspaces](https://github.com/ravindu644/Droidspaces-OSS)** — like LXC, no Docker daemon.

`droidctl` wraps this whole flow — plus every hard-won gotcha — into 2 commands:

```sh
droidctl convert nginx:alpine     # pull (arm64) + extract + auto-prepare init
droidctl start nginx-alpine       # run it (rootfs auto-resolved)
curl http://127.0.0.1:80          # → Welcome to nginx!
```

## Requirements

- **Root** (KernelSU / Magisk / APatch)
- **Droidspaces** installed ([ravindu644/Droidspaces-OSS](https://github.com/ravindu644/Droidspaces-OSS)) with a namespace-capable kernel (e.g. FleurX on garnet)
- **[crane](https://github.com/google/go-containerregistry)** binary — put it at `~/crane` or set `CRANE_BIN`
- **[DNSResolve-Termux](https://github.com/GegeDevs/DNSResolve-Termux)** KSU module — so crane (a Go binary) can resolve DNS directly in Termux
- Termux with `su`

## Install

Download the **static binary** for your architecture from the [Releases](https://github.com/GegeDevs/Droidctl-Termux/releases) page (built by GitHub Actions, `CGO_ENABLED=0` — no dependencies):

```sh
# Termux / Android arm64:
curl -sL <release-url>/droidctl-<ver>-arm64.tar.gz -o droidctl.tar.gz
tar -xzf droidctl.tar.gz
chmod +x droidctl
mv droidctl ~/bin/droidctl
```

Or grab the artifact from the latest CI run (`workflow_dispatch` / push to main → `droidctl-arm64-<sha>`).

> Go not needed locally — all compilation happens in GitHub Actions.

## Usage

```
droidctl pull <image>                 crane export --platform linux/arm64 -> tarball
droidctl convert <image> [--cmd ...]  pull + extract + auto-prepare init (inittab, ds-init)
droidctl images                       list extracted rootfs (convert pool)
droidctl ps                           droidspaces show (running containers)
droidctl start <name> [--net=...]     start container (default --net=host; rootfs auto)
droidctl stop <name>                  stop container
droidctl restart <name>               restart container
droidctl logs <name>                  tail container / daemon logs
droidctl exec <name> <cmd...>         run command inside container (non-interactive)
droidctl info <name>                  container info
droidctl check                        droidspaces kernel diagnostics
droidctl rm <name>                    stop + delete container + its rootfs
droidctl version                      show versions (droidspaces, crane)
```

### Examples

```sh
# nginx web server, accessible on host port 80
droidctl convert nginx:alpine
droidctl start nginx-alpine --net=host
curl http://127.0.0.1:80

# redis
droidctl convert redis:alpine
droidctl start redis-alpine --net=host

# any container, run commands inside
droidctl convert busybox:latest
droidctl start busybox-latest
droidctl exec busybox-latest 'uname -a'      # Linux ... 5.10.260-FleurX ... aarch64
```

## What it automates (the gotchas)

| Problem | Auto-handling |
|---|---|
| crane defaults to x86-64 → `Exec format error` | Always `--platform linux/arm64` on pull |
| Go binaries need `/etc/resolv.conf` (absent on Android) | Requires DNSResolve module (see above) |
| Hardlinks fail on FUSE `/data/media` | Extract as root to `/data/local/tmp` (ext4) with GNU tar `--hard-dereference` |
| Images lack init system (openrc absent, no `/sbin/init`) | Writes minimal `inittab` + `ds-init` entrypoint wrapper; symlinks `/sbin/init` → busybox |
| NAT port-forward silently fails on cellular | Default `--net=host` |
| Remembering which rootfs belongs to which container | `start` auto-resolves rootfs from the pool |

## Env overrides

| Var | Default |
|---|---|
| `DROIDSPACES_BIN` | `/data/local/Droidspaces/bin/droidspaces` |
| `CRANE_BIN` | `$HOME/crane` |
| `DROIDCTL_IMGDIR` | `$HOME/droidimages` (tarballs) |
| `DROIDCTL_ROOTFS_DIR` | `/data/local/tmp/droidimg` (extracted rootfs pool) |
| `DROIDCTL_NET` | `host` |

## How it works

```
crane export --platform linux/arm64 <image> - > tarball
        ↓
tar --hard-dereference -xf → /data/local/tmp/droidimg/<image>
        ↓  (auto: inittab, ds-init, /sbin/init)
droidspaces --name=<image> --rootfs=<pool>/<image> --net=host start
```

## Related

- [Droidspaces-Control-Skill](https://github.com/GegeDevs/Droidspaces-Control-Skill) — full Hermes Agent skill for Droidspaces CLI management
- [DNSResolve-Termux](https://github.com/GegeDevs/DNSResolve-Termux) — KernelSU module fixing Go DNS in Termux

## License

MIT