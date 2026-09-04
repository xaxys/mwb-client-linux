# MWB Client for Linux (Go)

Mouse Without Borders Linux client — speaks the PowerToys MWB wire protocol
(current v0.101+ and legacy pre-v0.101) as Client and Server (full symmetric
mesh). Wayland GNOME-first, X11 fully supported, evdev+uinput fallback.

Golden reference: `microsoft/PowerToys/src/modules/MouseWithoutBorders`
(protocol logic) + `xaxys/mwb-client-macos` (`docs/protocol/00-10`, Swift
implementation). Protocol layer is a straight port; difficulty is all in
Linux input backends.

## Design docs (设计需求与方案，中文)

- [00 原始设计需求](docs/00-requirements.md)
- [01 审计结论（协议可照抄）](docs/01-audit.md)
- [02 Linux 约束（难点全在输入）](docs/02-linux-constraints.md)
- [03 拟定架构](docs/03-architecture.md)
- [04 里程碑计划](docs/04-plan.md)
- [05 真机验证清单](docs/05-verify.md)

## Layout

```
cmd/mwb-client/          daemon + CLI (connect/disconnect/status/scan)
internal/protocol/       DATA 32/64B codec, PackageType, MatrixFlags, dedup-50,
                         MachineID, NextMachine hijack, challenge/ack helpers
internal/crypto/         current/legacy KDF+stream, Magic24, header+noise
internal/net/            client dial 15101+clipboard 15100, dual listeners
                         (6x500ms rebind), mesh dial-back, heartbeat/Awake,
                         pool, LAN scan (15101 0.6s x64), mDNS/PTR resolve
internal/input/          Backend iface + x11 / wayland-portal / evdev-uinput
                         (build-tag isolated) + edge detector + Helper handoff
internal/keymap/         WinVK <-> evdev/XKB (US first, DE/FR staged)
internal/clipboard/      text FastPath (Deflate+48B chunks) + Ask/Push stubs
internal/config/         JSON (~/.config/mwb-client) + known hosts + dual keys
internal/ui/             M0 CLI/tray stub; Fyne/GTK settings in M3+
internal/util/           log, bounds/normalize, LAN candidates
tests/                   mock-peer E2E (handshake/matrix/NextMachine, both gens)
```

## Compatibility baseline

- One binary runs **Ubuntu 22.04 + 24.04 × X11 + Wayland** (glibc floor 2.35).
- Build image pinned to **ubuntu:22.04 + official Go tarball ≥1.23**;
  `CGO_ENABLED=1` for X11/libei, plus a `CGO_ENABLED=0` pure-protocol
  variant for Docker unit tests.
- Runtime backend probe (`XDG_SESSION_TYPE` + portal capability):
  1. **X11** → XRecord/XInput2 + XTest + XFixes (full fidelity, both OSes).
  2. **Wayland GNOME46+ (24.04)** → InputCapture + RemoteDesktop/libei (portal auth).
  3. **Wayland GNOME42 (22.04) / no portal** → evdev+uinput **restricted mode**
     (needs `input` group / udev `uaccess`; cursor-hide TBD on hardware).
- 22.04 Wayland guarantees Client inject + restricted Host; 24.04 Wayland is
  full. UI always shows the active backend honestly.

## Quick start (dev on Windows, verify in Docker/Ubuntu)

```bash
go vet ./...
go test ./...
go build ./...
./mwb-client status
```

Docker (protocol-only, no display/compositor):

```bash
docker build -f Dockerfile -t mwb-linux:dev .
docker run --rm mwb-linux:dev
```

True hardware E2E (M2+) needs Ubuntu GNOME sessions (`/dev/uinput`,
portal prompts, cursor behavior) — Docker can't cover it. See
`docs/VERIFY.md` (M5 matrix: 2 OS × 2 sessions × 2 protos × 2 directions).

## Runtime iron rules (ported, do not regress)

- Capture callback: state + signal only; Helper goroutine does hide/show/cross.
- KEYUP followed by 10ms throttle; relative/absolute dual mode
  (100000 / 65535 / 300000); JUMP_PIXELS=2 / SKIP=1; global dedup window 50
  with single-ID fan-out (`SkSend` parity).
- Sleep: proactively `Close()` sockets pre-suspend, redial on resume.
- Empty matrix is never broadcast (fresh server adopts `[self,peer]` first).
- Clipboard chunks share the global dedup window (intentional deviation:
  we accumulate every leg into one transfer).
```

## Status

M0 scaffold + M1 mock-peer E2E. X11/portal/evdev injection lands in M2
(requires Ubuntu hardware); image/file clipboard staged as stubs.
