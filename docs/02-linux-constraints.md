# 02. Linux 约束：难点全在输入，与协议无关

> 目标：Ubuntu 24.04 LTS / GNOME 46 / Wayland 默认。实测有
> xdg-desktop-portal 1.18.3 + -gnome 46.0。本设计同时兼容 22.04/X11
>（见 §2）。

## 1. 两代系统差异（已核实）

| 项 | 22.04 Jammy | 24.04 Noble |
|---|---|---|
| GNOME / 会话 | 42，Wayland 默认但 X11 切换成熟 | 46，Wayland 默认 |
| portal | -gnome 42.x，有 RemoteDesktop（老）**无 InputCapture** | -gnome 46.0，有 InputCapture |
| 含义 | Wayland 下 Host 捕获无 portal 可用，只能 evdev 兜底；X11 下满血 | Wayland 下满血 |
| glibc | 2.35 | 2.39 |
| apt Go | 1.18（太老，不用） | 1.22（仍建议官方 tarball ≥1.23） |

## 2. 兼容策略：运行时探测 + 构建时取低

运行时三档自动降级（`XDG_SESSION_TYPE` + portal 能力探测决定，不写死）：

1. **X11 会话**（22.04 主力 / 24.04 可选）：XRecord/XInput2 捕获 + XTest 注入
   + XFixes，两代系统行为一致，最稳，M2 先用它打通闭环。
2. **Wayland + GNOME46+（24.04）**：InputCapture 捕获 + RemoteDesktop/libei
   注入，走 portal 授权；失败落到 3。
3. **Wayland + GNOME42（22.04）/ portal 缺失**：降级 evdev 捕获 + uinput 注入
   （需 input 组 / udev uaccess，安装脚本自检 + 一键加组），Host 在此档标记
   “受限模式”（权限 friction + Wayland 下光标隐藏待真机验证）。

即：22.04 推荐 X11 会话满血；22.04 Wayland 只保证 Client 注入 + evdev Host
受限；24.04 Wayland 满血。UI 如实显示当前后端，不假装 portal 可用。

构建取低：

- Docker 构建镜像固定 `ubuntu:22.04` + 官方 Go tarball（≥1.23），不用
  latest/ubuntu24.04，保证 max GLIBC=2.35，22.04/24.04 通吃；
  CGO_ENABLED=1（X11/libei 必需），另出 CGO_ENABLED=0 纯协议版跑 Docker 内单测。
- libei-dev/libportal 按 22.04 老 API 编写 + capability probe，新接口
  dlopen/弱链接或构建 tag 隔离，避免在 22.04 编译期引用 24.04 才有的符号。
- `.deb` 按 jammy 为 Depends 下限（libx11、libxtst、libxi、libxfixes、libei1、
  libportal1 不卡高版本），22.04/24.04 同包安装。

## 3. 语言定 Go（Rust 备选，.NET 不取）

- 协议：`net` / `crypto(aes/pbkdf2/sha512)` / `encoding/binary` 标准库即够；
  `golang.org/x/crypto` 补 PBKDF2。
- 输入双后端：godbus/dbus + libei-go-bindings（cgo libei-dev）+
  bendahl/uinput + holoplot/go-evdev + BurntSushi/xgb 覆盖 X11；
  单静态二进制 + `golang:1.23-bookworm` Docker 交叉编译最顺。
- Rust 仅备选（lan-mouse 可抄但构建/UI 无优势）；.NET Avalonia 可 1:1 抄结构体
  但输入仍要 P/Invoke 且拖运行时，不取。

## 4. 风险清单

- Wayland portal 版本碎片（<GNOME46 无 InputCapture）。
- uinput 权限 friction（input 组 / udev uaccess）。
- 光标隐藏在 mutter 下的表现（真机验证）。
- 非 US 键位映射（先 US，后续德/法等）。
- 高轮询鼠标 TCP 抖动（上游已知 issue，建议有线网验证）。
