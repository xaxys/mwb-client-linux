// Package protocol: DATA packet encode/decode, checksum, magic framing.
package protocol

import (
	"encoding/binary"
	"errors"
	"strings"
)

var (
	ErrBadSize     = errors.New("protocol: packet must be 32 or 64 bytes")
	ErrBadChecksum = errors.New("protocol: checksum mismatch")
	ErrBadMagic    = errors.New("protocol: magic mismatch")
	ErrNotExtended = errors.New("protocol: type requires 64-byte extended packet")
)

// Packet is the decoded DATA struct.
type Packet struct {
	Type PackageType
	ID   int32
	Src  uint32
	Des  uint32
	// Raw payload bytes 16..31 (16 bytes, overlapping union).
	Payload [16]byte
	// MachineName is only set for 64-byte extended packets.
	MachineName string
	// HasName reports whether the wire form was 64 bytes.
	HasName bool
}

// Encode serializes the packet to 32 or 64 bytes, embedding checksum and
// the top 16 bits of magic. magic is the 32-bit value from crypto.Magic24.
func (p *Packet) Encode(magic uint32) ([]byte, error) {
	extended := p.Type.IsExtended() || p.HasName || p.MachineName != ""
	size := PackageSize
	if extended {
		size = PackageSizeEx
	}
	buf := make([]byte, size)
	buf[0] = byte(p.Type)
	// bytes 1..3 filled below
	binary.LittleEndian.PutUint32(buf[4:8], uint32(p.ID))
	binary.LittleEndian.PutUint32(buf[8:12], p.Src)
	binary.LittleEndian.PutUint32(buf[12:16], p.Des)
	copy(buf[16:32], p.Payload[:])
	if extended {
		copy(buf[32:64], PadMachineName(p.MachineName))
	}
	buf[3] = byte((magic >> 24) & 0xFF)
	buf[2] = byte((magic >> 16) & 0xFF)
	buf[1] = Checksum(buf[2:32])
	return buf, nil
}

// Decode parses and validates a raw packet. magic is the expected 32-bit
// magic; checksum covers bytes 2..31 (even for 64B packets).
// On success bytes 1..3 are conceptually zeroed (not returned).
func Decode(raw []byte, magic uint32) (*Packet, error) {
	if len(raw) != PackageSize && len(raw) != PackageSizeEx {
		return nil, ErrBadSize
	}
	wantMagicHi := byte((magic >> 24) & 0xFF)
	wantMagicLo := byte((magic >> 16) & 0xFF)
	if raw[3] != wantMagicHi || raw[2] != wantMagicLo {
		return nil, ErrBadMagic
	}
	if Checksum(raw[2:32]) != raw[1] {
		return nil, ErrBadChecksum
	}
	p := &Packet{
		Type: PackageType(raw[0]),
		ID:   int32(binary.LittleEndian.Uint32(raw[4:8])),
		Src:  binary.LittleEndian.Uint32(raw[8:12]),
		Des:  binary.LittleEndian.Uint32(raw[12:16]),
	}
	copy(p.Payload[:], raw[16:32])
	if len(raw) == PackageSizeEx {
		p.HasName = true
		p.MachineName = UnpadMachineName(raw[32:64])
	}
	return p, nil
}

// Checksum sums bytes mod 256 (covers offsets 2..31 pre-injection).
func Checksum(b []byte) byte {
	var s uint32
	for _, v := range b {
		s += uint32(v)
	}
	return byte(s & 0xFF)
}

// PadMachineName encodes to 32B ASCII space-padded (0x20).
func PadMachineName(name string) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = 0x20
	}
	// ASCII only: non-ASCII bytes are passed through truncated; PowerToys
	// uses ASCII host names. Truncate to 32 bytes.
	n := copy(out, name)
	_ = n
	return out
}

// UnpadMachineName trims trailing spaces and NULs.
func UnpadMachineName(b []byte) string {
	s := string(b)
	s = strings.TrimRight(s, " \x00")
	// also trim embedded trailing zeros defensively
	s = strings.TrimRight(s, "\x00")
	return s
}

// --- Payload helpers (overlapping union at bytes 16..31) ---

// KeyEvent payload: DateTime int64 + wVk int32 + dwFlags int32.
type KeyEvent struct {
	DateTime int64 // ticks/10000 (ms since 1/1/0001); informational
	VK       int32 // Windows Virtual Key code
	Flags    int32 // key state (KEYUP etc.)
}

func (p *Packet) SetKey(k KeyEvent) {
	binary.LittleEndian.PutUint64(p.Payload[0:8], uint64(k.DateTime))
	binary.LittleEndian.PutUint32(p.Payload[8:12], uint32(k.VK))
	binary.LittleEndian.PutUint32(p.Payload[12:16], uint32(k.Flags))
}

func (p *Packet) GetKey() KeyEvent {
	return KeyEvent{
		DateTime: int64(binary.LittleEndian.Uint64(p.Payload[0:8])),
		VK:       int32(binary.LittleEndian.Uint32(p.Payload[8:12])),
		Flags:    int32(binary.LittleEndian.Uint32(p.Payload[12:16])),
	}
}

// MouseEvent payload: X, Y, WheelDelta, dwFlags int32.
type MouseEvent struct {
	X          int32
	Y          int32
	WheelDelta int32
	Flags      int32
}

func (p *Packet) SetMouse(m MouseEvent) {
	binary.LittleEndian.PutUint32(p.Payload[0:4], uint32(m.X))
	binary.LittleEndian.PutUint32(p.Payload[4:8], uint32(m.Y))
	binary.LittleEndian.PutUint32(p.Payload[8:12], uint32(m.WheelDelta))
	binary.LittleEndian.PutUint32(p.Payload[12:16], uint32(m.Flags))
}

func (p *Packet) GetMouse() MouseEvent {
	return MouseEvent{
		X:          int32(binary.LittleEndian.Uint32(p.Payload[0:4])),
		Y:          int32(binary.LittleEndian.Uint32(p.Payload[4:8])),
		WheelDelta: int32(binary.LittleEndian.Uint32(p.Payload[8:12])),
		Flags:      int32(binary.LittleEndian.Uint32(p.Payload[12:16])),
	}
}

// IsRelative reports the 100000-offset relative mode marker.
func (m MouseEvent) IsRelative() bool {
	return m.X >= MoveMouseRelative || m.X <= -MoveMouseRelative ||
		m.Y >= MoveMouseRelative || m.Y <= -MoveMouseRelative
}

// NextMachine hijacks the mouse payload: X/Y = 0..65535 entry coords,
// WheelDelta = destination machine ID (1..4).
func (p *Packet) SetNextMachine(entryX, entryY int, destID uint32) {
	p.Type = PtNextMachine
	p.SetMouse(MouseEvent{X: int32(entryX), Y: int32(entryY), WheelDelta: int32(destID)})
}

func (p *Packet) GetNextMachine() (entryX, entryY int, destID uint32) {
	m := p.GetMouse()
	return int(m.X), int(m.Y), uint32(m.WheelDelta)
}

// PostAction for clipboard Ask/Push initial payload (int32 at 16..19).
func (p *Packet) SetPostAction(a int32) {
	binary.LittleEndian.PutUint32(p.Payload[0:4], uint32(a))
	for i := 4; i < 16; i++ {
		p.Payload[i] = 0
	}
}

func (p *Packet) GetPostAction() int32 {
	return int32(binary.LittleEndian.Uint32(p.Payload[0:4]))
}
