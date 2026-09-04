# 03. 拟定架构（Go）

> 空仓即按此建（现状对照见 README.md；偏离需先改本文）。

```text
cmd/mwb-client/          # daemon + cli (connect/disconnect/status/scan)
internal/protocol/       # DATA 32/64B 编解码、PackageType、MatrixFlags、
                         # MachineID、NextMachine 劫持、challenge/ack、Dedup50
internal/crypto/         # current/legacy 双实现、Magic24、header+noise、
                         # Auto 探测（current→legacy 各 fresh TCP + 8s 超时）
internal/net/            # client 拨 15101 + clipboard 拨 15100 /
                         # server 双监听 + 6×500ms 重绑 + mesh 回拨 +
                         # 心跳/Awake + 50 窗去重（全局，SkSend 单 ID 扇出）
internal/input/          # 接口 + backends：
                         # x11（xrecord + xtest）/
                         # wayland-portal（inputcapture + remotedesktop/libei）/
                         # evdev + uinput 兜底
internal/keymap/         # WinVK ↔ evdev/XKB 双向表（先 US，后续德/法等；
                         # macOS KeyCodeMapper 照搬思路）
internal/clipboard/      # 文本 FastPath（Deflate + 48B 分片）+ Ask/Push 副通道；
                         # 图片/文件 stub 留口
internal/config/         # JSON（~/.config/mwb-client）+ 已知 host IP→名 +
                         # 双 key（client/server 独立）+ protocolVersion
internal/ui/             # M0 先 systray/AppIndicator + CLI；
                         # M3 后 Fyne/GTK 设置页（连接/布局/剪贴板/权限/关于）
internal/util/           # 日志、屏 bounds、多显、LAN 扫
                         #（15101 0.6s × 64 并发 + NetBIOS137 与 PTR 赛跑）、
                         # mDNS xxx.local
tests/                   # 协议向量（与 PowerToys/Mac 同 magic/包）+
                         # mock 对端串起握手/矩阵/NextMachine/剪贴板
Dockerfile + .github/workflows  # offence：Linux 交叉编译 + 单测；
                                # 真机 E2E 不进 Docker
```

## 关键复刻点（对照 `01-audit.md` §1.2 铁律）

1. Helper 异步交接：capture 回调只置状态 + signal，另 goroutine 做
   hide/show/切 Des。
2. KEYUP 后 10ms 节流。
3. 相对/绝对双模（100000 / 65535 / 300000）。
4. JUMP_PIXELS=2 / SKIP=1。
5. 去重窗 50（全局单窗，发送单 ID 扇出）。
6. sleep 前主动关 socket，醒后重拨。
7. 空矩阵永不广播（fresh server 先 `[self,peer]` 收养再广播，
   防清空 Windows 布局）。
