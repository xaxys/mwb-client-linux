# 01. 审计结论：协议是确定的，可照抄 macOS

> 审计方式：只读。未落盘 clone，全经 webfetch 读 `xaxys/mwb-client-macos`
>（README / AGENTS.md / docs/protocol/00–10 / MWBClient/* 目录清单）与
> `microsoft/PowerToys/src/modules/MouseWithoutBorders` 结构。
> 进入 build 模式后允许 `git clone --depth 1 PowerToys` 作 golden reference
>（只读对照，放 `reference/`，不提交）。

## 1.1 PowerToys MWB 线协议（macOS docs/protocol 已验证，直接复用）

### 传输

- 纯 TCP，无 UDP。小端序。包定长二选一：**32B 标准包** / **64B 扩展包**
 （尾 32B 为发送方 MachineName）。
- 端口（基址 `TcpPort` 默认 `15100`）：`15100` = 剪贴板副通道
 （`AcceptConnectionAndSendClipboardData` / `ConnectToRemoteClipboardSocket`），
  `15101` = 消息/输入主通道（`TCPServerThread → MainTCPRoutine` /
  `StartNewTcpClientThread → MainTCPRoutine`）。
- 接收循环：读首字节定 `PackageType` → 读足 32/64B → 验 Checksum + Magic →
  50 窗去重（`RecentProcessedPackageIDs`，进程级全局）→ 按类型分发。

### 包（`DATA.cs`，`[StructLayout(LayoutKind.Explicit)]` 重叠联合）

头 16B（32/64B 包相同）：

| 偏移 | 长 | 类型 | 字段 | 说明 |
|---|---|---|---|---|
| 0 | 1 | uint8 | PackageType | 主类型（矩阵 flag 按位或其上） |
| 1 | 1 | uint8 | Checksum | 字节 2..31 求和 mod256（64B 包也只覆前 32B） |
| 2 | 1 | uint8 | Magic 低 | `(MagicHash >> 16) & 0xFF` |
| 3 | 1 | uint8 | Magic 高 | `(MagicHash >> 24) & 0xFF` |
| 4 | 4 | int32 | Id | 自增序列，去重用 |
| 8 | 4 | uint32 | Src | 发送方 Machine ID |
| 12 | 4 | uint32 | Des | 目标 ID（255 = ALL 广播） |

> 接收方验过后把字节 1..3 置零再上交。

载荷 16..31（按类型重叠）：Keyboard(122) = `DateTime int64`（ticks/10000）
+ `wVk int32` + `dwFlags int32`；Mouse(123/121) = X/Y/WheelDelta/dwFlags 四 int32；
Handshake(126)/Ack(127) = 16B 随机 challenge；Clipboard Ask/Push 初包 =
`PostAction int32`；Heartbeat/Matrix 类 16..31 置零，信息全在扩展区。
扩展 32..63：MachineName，ASCII，不足 32B 用 `0x20` 空格补齐。

PackageType（字节 0）：2 Hi / 3 Hello(64B) / 4 ByeBye / 20 Heartbeat(64B) /
21 Awake(64B) / 50 HideMouse / 51 Heartbeat_ex(64B) / 52、53 / 69 Clipboard(64B) /
76、77 ClipboardDataEnd / 78 ClipboardAsk(64B) / 79 ClipboardPush(64B) /
121 NextMachine / 122 Keyboard / 123 Mouse / 124 ClipboardText(64B) /
125 ClipboardImage(64B) / 126 Handshake(64B) / 127 HandshakeAck(64B) /
128 Matrix(64B，另见 MatrixFlags) / 129 MachineSwitched。

### 加密 current（v0.101+，PR #48742 / #49600）

- 每**方向**独立随机 16B salt + 16B IV；每流开头明文先发 32B 头（salt+IV）。
- Key = PBKDF2-HMAC-SHA512（密码=Security Key UTF-8，salt=本方向随机，
  **100,000 次**）→ 32B；AES-256-CBC，Zeros 补齐（包天然 16 对齐）。
- 线序（每方向）：`[32B 明文头][16B 密文 noise][加密包…]`，CBC 链全流延续。
- 两方向 key 不同（各发各的 salt）。

### 加密 legacy（<v0.101）

- 无明文头；固定 salt = `"18446744073709551615"` 的 UTF-16LE 字节，
  固定 IV = ASCII `"1844674407370955"`（16B）；PBKDF2-HMAC-SHA512 **50,000 次**；
  两方向同 key、各自 CBC 链；仍有 16B noise 交换。
- 线序（每方向）：`[16B noise][加密包…]`。

### Magic（两代相同，不随 KDF 升级变）

32B 缓冲填 Security Key ASCII（零补齐）→ SHA-512 → 迭代 50,000 次 →
`magic = hash[0]<<23 | hash[1]<<16 | hash[63]<<8 | hash[2]`。
发包前取高 16 位嵌字节 2..3。这是 framing/identity，不是 key material。

### 握手（主通道，完全对称，双方跑同一套 `MainTCPRoutine`）

1. 开 socket 即互换 32B 明文头（current）→ 各自初始化出/入 cipher。
2. 双方各写 16B 加密随机（CBC-shift，防首包已知明文攻击），各读 16B。
3. 双方各发 **10×** Handshake(126)：64B 全随机，16..31 即 challenge，
   32..63 为本机名。
4. 收到 Handshake 即回 HandshakeAck(127)：Type 改 127、Src 置 0（NONE）、
   换本机名、载荷按位 NOT。
5. 收到 Ack 校验 16..31 恰为自己 challenge 的 NOT，全过即信任（Trusted+Connected），
   转入 Keyboard/Mouse/Heartbeat 分发。
- Auto 探测：候选 `[current, legacy]`，**每个候选用 fresh TCP**（错代可能已
  搞乱流状态，不可复用），noise+10 轮握手各限 8s（`handshakeAttemptTimeout`，
  连接 5s），失败拆掉试下一个；成功记住 `resolvedProtocol`， inbound 也用它。

### 剪贴板副通道（15100，不走 126 挑战）

1. 照样 header+noise。
2. 直接发 Clipboard(69)/ClipboardPush(79) 初包（含 MachineName + PostAction）。
3. 服务端验 Magic，读 Src：该 MachineID 若已有认证主通道即信任（信任继承），
   否则拒绝。连接短命：传完即关。
- ShakeHand 顺序（两端同逻辑）：发 32B 头并包出加密流 → 写 16B noise →
  写 64B Push 头 → 读对端 32B 头包出解密流 → 读 16B noise → 读对端 64B 头 →
  校验对端名对应已知 ID 且有主连接。**Clipboard 连接上绝不发 126 包**。

### 拓扑

- Machine ID = 矩阵槽位 1..4（1-based），0 = NONE，255 = ALL。
  `MachinePool` 做 `MachineName ↔ ID` 动态映射（扩展包的 Src + 尾部名学习）；
  持久化形 `"HostA:1,HostB:2,,"`；存活判据：socket 连通或心跳在窗口内
  （Windows `HEARTBEAT_TIMEOUT` 25min；macOS 另有 10s liveness 判定，见下）。
- 生命周期包（64B，经 `SendHeartBeat` 广播）：Hello(3，发版/手动连接，收到回
  Heartbeat）、Heartbeat(20)/Heartbeat_ex(51)（保活）、Awake(21，有输入且未锁屏时
  发，对端可据此亮/暗显示器图标）、ByeBye(4，优雅退出）。
- Matrix(128) 包：Type 字节按位或布局 flag（`MatrixSwapFlag=2` wrap 环绕，
  `MatrixTwoRowFlag=4` 2x2，否则 1x4；如 130=wrap 1x4、134=wrap 2x2，以 Src==4
  的包为准）；Src=槽位 1..4，Des=ALL；16..31 不用；32..63 为该槽机器名。
  同步：新机握手完发 Heartbeat_ex → 主机 `AddToMachinePool`，新面孔即广播
  4×Matrix；老面孔可能不广播——客户端必须本地持久化 matrix。
  收齐 Src==4 再提交拓扑并刷新切边逻辑；矩阵包 ID 必须唯一（复用会被 50 窗去重吞掉）。
- **空矩阵永不广播**（Windows 盲拷每槽，fresh server 广播空矩阵会清空对端布局，
  双向路由全断）。Fresh server 做法：首个可信 peer 触发 `[self, peer]` 收养
  （对齐首跑 `UpdateSetupMachineMatrix`），再广播。

### 输入

- 键盘：`KEYBDDATA` 直填 16..31；发完 KEYUP/SYSKEYUP 即 `Sleep(10ms)`（防淹没
  对端输入队列，保证修饰键顺序）；接收方先拦截 CTRL+ALT+DEL（转提权 helper）、
  Win+L（直接锁屏），丢连接/跨边时补注所有修饰键 KEYUP（防粘键）。
- 鼠标：默认相对位移（`MOVE_MOUSE_RELATIVE = 100000`，正发 `dx+100000`、
  负发 `dx-100000`，保本机加速手感）；点击/绝对定位用 65535 网格缩放
  （`(x-left)*65535/width`，对端按自家 bounds 还原；`XY_BY_PIXEL=300000`
  系内部像素跳）；相对模式下收到点击则取本地光标转绝对再注（`Receiver.cs`）。
- **委托切边**：Host 盲发相对量；真正判边的是光标所在机
  （`MoveToMyNeighbourIfNeeded`，`SKIP_PIXELS=1` 触发，查自家 `LiveMachineMatrix`
  定邻居，归一坐标 `0..65535`，跨轴钳 `JUMP_PIXELS=2` 防弹回）。
  判中后发 **NextMachine(121)**（复用 mouse 载荷：X/Y=归一入口，WheelDelta=目标 ID）
  回 Host；Host `PrepareToSwitchToMachine`：发 HideMouse(50) 给旧机 → 切 Des →
  发 MachineSwitched(129) 给新机 →（若切前有复制）触发剪贴板拉取。

## 1.2 macOS 架构（Linux 照此分包）

`App/Coordinator(AppCoordinator)` → `Network(NetworkManager 主动 + ServerListener
被动 + ClipboardListener + Heartbeat + LANScanner + NameResolver + Dedup50)` →
`Protocol(Packet/Handshake/Constants)` → `Crypto(MWBCrypto)` →
`Input(Capture+Taps/Injection+EdgeDetector+KeyCodeMapper WinVK↔Mac)` →
`Clipboard(Codec 分片/压缩 + Manager + DragDrop)` → `State/Persistence`
（UI 5 页 + UserDefaults）。另有 `HelperThread(EvSwitch)` 异步交接、断线重连、
sleep 前主动 `Close()` 等 runtime 铁律——**Linux 必须照抄**：

1. input 回调里绝不做重活（macOS 在 `CGEventTap` 回调里调 hide 会被系统掐 hook；
   Windows 同理），只置状态 + `EvSwitch.Set()`，Helper 线程做 hide/show/切 Des。
2. KEYUP 后 10ms 节流；相对/绝对双模（100000/65535/300000）；
   JUMP_PIXELS=2 / SKIP=1；去重窗 50（`SkSend` 单 ID 扇出，全局一窗）。
3. sleep 前主动关 socket，醒后重拨（否则僵尸 TCP 假装 Connected，卡数分钟）。
4. 空矩阵永不广播（fresh server 先 `[self,peer]` 收养再广播）。
