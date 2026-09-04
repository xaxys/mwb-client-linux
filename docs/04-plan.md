# 04. 里程碑计划

> 切 build 模式后执行。M0/M1 纯协议（Docker mock 可验）；M2+ 必须真机。

- **M0 脚手架 + 协议单测**（Docker 可验）：Go mod + 包骨架 + DATA 编解码 +
  双加密 + Magic + Checksum 向量与 macOS/PowerToys 对齐；`go vet/test` 全绿。
- **M1 Client 握手 + 拓扑**（Docker mock 可验）：Auto 探测 + 10 轮 challenge +
  Heartbeat/Matrix 解析持久化 + LAN 扫 + mDNS；对 mock Windows（Go 写 fake 15101）
  端到端。
- **M2 输入闭环**（需真机）：X11 先通（XTest/XRecord）→ **M2a**；
  再 Wayland portal → **M2b**；uinput/evdev 兜底 → **M2c**。
  双向切边 + NextMachine + 光标显隐 + WinVK↔evdev 表；与真实 PowerToys
  2 机对测延迟。
- **M3 文本剪贴板**（真机）：主通道小文本 + 副通道拉取；图片/文件明确 stub。
- **M4 Server 全对称**（真机）：双监听 + 独立 serverKey + presence 广播 +
  mesh 单拨 + 6 重试 + 切边 5s watchdog；Windows 反连验证。
- **M5 包装发布**：`.deb` + udev（uinput/input）+ systray + Fyne 设置页 +
  权限引导（portal 授权 / input 组自检一键脚本）；README 真机矩阵
  （X11/Wayland × Client/Server × current/legacy）。

M2 拆分（同时兼容 22.04/X11 基线，增量非推翻）：M2a X11（22.04+24.04 X11 会话
真机）→ M2b Wayland-portal（24.04）→ M2c evdev/uinput 兜底（22.04 Wayland）。
M0/M1 不变（纯 Go 协议，Docker `ubuntu:22.04` 镜像内 `go test` 即验双系统兼容性）。

## Docker 定位

只保证 M0/M1（mock 对端）编译 + 单测；M2+ 必须 Ubuntu 真机/GNOME 会话
（含 `/dev/uinput`、portal 弹窗、光标行为），Docker 内无 display/compositor
测不出。真机按 `05-verify.md` 打勾。

## 验证矩阵（M5 固定）

2 OS（22.04/24.04）× 2 会话（X11/Wayland）× 2 协议（current/legacy）×
2 方向（Client/Server）；22.04-Wayland 格标**受限**而非失败。

## 第一步（历史记录）

1. `git clone --depth 1 PowerToys` + `mwb-client-macos` 到 `reference/`
   （只读对照，代理走 `http://127.0.0.1:10808`）；
2. 按 `03` 建 Go 骨架，先写 protocol/crypto 单测对齐向量；
3. 起 Docker `golang:1.23` 验证交叉编译（当时本机 Docker daemon 未启动）。
