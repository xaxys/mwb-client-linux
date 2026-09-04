// Package protocol implements the Mouse Without Borders wire format.
//
// Golden reference: microsoft/PowerToys src/modules/MouseWithoutBorders
// (App/Core/DATA.cs, App/Core/Encryption.cs, App/Class/SocketStuff.cs)
// mirrored via xaxys/mwb-client-macos docs/protocol/00-10.
//
// All multi-byte integers are little-endian. Every packet is exactly
// 32 bytes (standard) or 64 bytes (extended, trailing 32B MachineName).
package protocol

import "time"

// TCP ports derived from base TcpPort (default 15100).
const (
	// ClipboardPort is TcpPort: secondary clipboard / drag-drop channel.
	ClipboardPort = 15100
	// MessagePort is TcpPort+1: main input / control channel.
	MessagePort = 15101
)

// Packet sizes.
const (
	PackageSize   = 32 // standard packet
	PackageSizeEx = 64 // extended packet (with MachineName)
	DataSize      = 48 // clipboard chunk payload (bytes 16..63)
)

// PackageType is byte 0 of every packet (see DATA.cs).
type PackageType byte

const (
	PtHi                   PackageType = 2
	PtHello                PackageType = 3
	PtByeBye               PackageType = 4
	PtHeartbeat            PackageType = 20
	PtAwake                PackageType = 21
	PtHideMouse            PackageType = 50
	PtHeartbeatEx          PackageType = 51
	PtHeartbeatExL2        PackageType = 52
	PtHeartbeatExL3        PackageType = 53
	PtClipboard            PackageType = 69
	PtClipboardDragDrop    PackageType = 70
	PtClipboardDragDropEnd PackageType = 71
	PtExplorerDragDrop     PackageType = 72
	PtClipboardCapture     PackageType = 73
	PtCaptureScreenCommand PackageType = 74
	PtClipboardDragDropOp  PackageType = 75
	PtClipboardDataEnd     PackageType = 76 // fast-path terminator (in-band)
	PtMachineSwitched      PackageType = 77
	PtClipboardAsk         PackageType = 78
	PtClipboardPush        PackageType = 79
	PtNextMachine          PackageType = 121
	PtKeyboard             PackageType = 122
	PtMouse                PackageType = 123
	PtClipboardText        PackageType = 124
	PtClipboardImage       PackageType = 125
	PtHandshake            PackageType = 126
	PtHandshakeAck         PackageType = 127
	PtMatrix               PackageType = 128 // base; OR with MatrixFlags
)

// IsExtended reports whether a package type uses the 64-byte form.
// Mirrors DATA.IsBigPackage/MWBPacket.isBig: explicit list, plus the
// matrix family (128 | wrap | twoRow). NOTE: matrix flags must ONLY be
// stripped for types with bit7 set — stripping unconditionally mangles
// heartbeat (20→16), clipboard (69→65), data-end (76→72), etc.
func (t PackageType) IsExtended() bool {
	if byte(t)&0x80 != 0 {
		return byte(t)&^0x06 == byte(PtMatrix)
	}
	switch t {
	case PtHello, PtHeartbeat, PtAwake,
		PtHeartbeatEx,
		PtClipboard, PtClipboardAsk, PtClipboardPush,
		PtClipboardText, PtClipboardImage, PtClipboardDataEnd,
		PtHandshake, PtHandshakeAck:
		return true
	}
	return false
}

// Base strips matrix layout flags to the underlying type.
func (t PackageType) Base() PackageType {
	if byte(t)&0x80 != 0 {
		return PackageType(byte(t) &^ 0x06)
	}
	return t
}

// Matrix layout flags OR-ed into the Type byte (authoritative in Src==4).
const (
	MatrixSwapFlag   byte = 2 // wrap mouse around (circle mode)
	MatrixTwoRowFlag byte = 4 // 2x2 layout; absent = 1x4 single row
)

// Machine IDs are 1-based matrix slots; 0 = NONE, 255 = ALL broadcast.
const (
	IDNone     uint32 = 0
	IDAll      uint32 = 255
	MaxMachine        = 4
)

// Input / edge constants (see docs/protocol 04/05).
const (
	MoveMouseRelative = 100000 // relative-delta marker offset
	XYByPixel         = 300000 // pixel-perfect jump marker
	CoordMax          = 65535  // absolute normalization grid
	SkipPixels        = 1      // edge trigger threshold
	JumpPixels        = 2      // re-entry clamp (anti-bounce)
	KeyUpThrottle     = 10 * time.Millisecond
)

// Network / runtime constants.
const (
	ConnectAttemptTimeout   = 5 * time.Second
	HandshakeAttemptTimeout = 8 * time.Second
	HandshakeRounds         = 10
	DedupWindow             = 50
	RebindRetries           = 6
	RebindDelay             = 500 * time.Millisecond
	LANScanTimeout          = 600 * time.Millisecond
	LANScanConcurrency      = 64
	CrossingWatchdog        = 5 * time.Second
)

// KEYBDDATA dwFlags (KEYEVENTF parity, see DATA.cs).
// Releases (KEYUP and SYSKEYUP alike) carry KeyFlagUp; the sender must
// sleep KeyUpThrottle (10ms) after each release.
const (
	KeyFlagDown     int32 = 0
	KeyFlagExtended int32 = 0x0001
	KeyFlagUp       int32 = 0x0002
)

// ProtocolVersion selects the wire encryption generation.
type ProtocolVersion string

const (
	ProtoAuto    ProtocolVersion = "auto"    // probe current -> legacy
	ProtoCurrent ProtocolVersion = "current" // PowerToys >= v0.101
	ProtoLegacy  ProtocolVersion = "legacy"  // PowerToys < v0.101
)
