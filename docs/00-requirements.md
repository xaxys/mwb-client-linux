# 00. 原始设计需求

> 本文档是立项时的原始需求记录，后续实现以它为准，变更需同步更新本文。

## 背景

Windows 侧跑 Microsoft PowerToys 的 Mouse Without Borders（MWB），需要在
Linux 机上跑一个客户端：同一套键鼠跨屏共享、撞边切换、剪贴板同步。
macOS 已有独立实现 `xaxys/mwb-client-macos`（Swift），Linux 侧从零起。

## 已定选择（定稿，不再讨论）

| 项 | 选择 |
|---|---|
| 语言 | Go（单静态二进制；`net`/`crypto`/`encoding/binary` 标准库即够协议） |
| 桌面优先 | Wayland GNOME 优先，同时兼容 X11（见 `02-linux-constraints.md`） |
| MVP | 键鼠 + 切边 + 剪贴板（文本/图片/文件，图片与文件不再是 stub） |
| 拓扑角色 | Client + Server 全对称（每台机器既是 Host 也是 Client，无永久主从） |
| 目标系统 | Ubuntu 22.04 LTS 与 24.04 LTS（`02` 有两代差异表） |
| 协议 | 照抄 PowerToys 线协议 + macOS 已验证结论，不自创（见 `01-audit.md`） |

## 范围

- M0–M1：协议、双加密、握手、拓扑、mock 对端 E2E（Docker 内可验）。
- M2：输入闭环（X11 → Wayland portal → evdev/uinput 兜底），需真机。
- M3：文本/图片/文件剪贴板（主通道小数据 + 副通道 Ask/Push 拉取，全部先经
  mock 回环验证，真机再与 PowerToys 对测）。
- M4：Server 全对称（Windows 反连）。
- M5：`.deb` 包装 + systray + 设置页 + 真机验证矩阵（设置页 GUI 需 apt
  依赖，在真机阶段补；先出无 GUI 的 .deb 骨架 + udev 规则）。

## 非目标

- 自创线协议 / 改 PowerToys 行为（行为不一致时以 PowerToys 为准）。
- Wayland 下绕过 portal 授权的捕获手段。
- 远程桌面视频流、文件管理器集成等 MWB 之外的功能。

## 成功标准

- `go vet` / `go test` 全绿；Docker（`ubuntu:22.04` 基线）构建+单测通过。
- 真机验证矩阵（见 `04-plan.md`）中除 22.04-Wayland 标“受限”外全部打勾。
- 与真实 PowerToys 双机对测：切边往返、键鼠、滚轮、文本粘贴可用，延迟可接受。
