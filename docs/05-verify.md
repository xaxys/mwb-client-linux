# 05. 真机验证清单

> Docker 只验 M0/M1。以下全部需 Ubuntu 真机/GNOME 会话。按序打勾，
> 22.04-Wayland 格标“受限”而非失败。

## 连通与拓扑

- [ ] Client 连通（Auto 探测命中对端协议代际，current/legacy 各一遍）
- [ ] 双协议代际分别连通（Windows 装新/老 PowerToys 各一遍，或 mock 对端）
- [ ] Heartbeat/Awake 保活不断（锁屏前后各观察）
- [ ] Matrix 下发与持久化（改 Windows 布局后 Linux 侧跟随）

## 输入

- [ ] 切边往返（Host→Client→Host，归一坐标落点合理，无弹回）
- [ ] 键鼠（含滚轮、修饰键组合、大小写，无丢键/粘键）
- [ ] 文本剪贴板（小文本主通道 + 大文本副通道拉取，对端可粘贴）
- [ ] 光标显隐配对（断开/重连后不残留隐藏光标）

## 韧性

- [ ] 杀网重连（断网→恢复，切边自动恢复）
- [ ] 休眠唤醒（睡前主动关 socket，醒后重拨，无数分钟 hung 住）
- [ ] Server 反连（Windows 填 Linux 的 serverKey 反连成功）
- [ ] 切边 5s watchdog（跨边中停流能强制复位）

## 矩阵记录

| OS | 会话 | 协议 | Client | Server | 备注 |
|---|---|---|---|---|---|
| 22.04 | X11 | current | | | 主力先行 |
| 22.04 | X11 | legacy | | | |
| 22.04 | Wayland | current | | | 受限（evdev） |
| 22.04 | Wayland | legacy | | | 受限（evdev） |
| 24.04 | X11 | current | | | |
| 24.04 | X11 | legacy | | | |
| 24.04 | Wayland | current | | | portal 满血目标 |
| 24.04 | Wayland | legacy | | | |

风险提示（见 `02-linux-constraints.md` §4）：portal 碎片、uinput 权限、
mutter 光标隐藏、非 US 键位、高轮询鼠标 TCP 抖动（建议有线网验证）。
