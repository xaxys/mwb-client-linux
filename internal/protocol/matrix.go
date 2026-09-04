package protocol

import (
	"crypto/rand"
	"strings"
)

// Matrix is the 4-slot spatial layout (indices 0..3 ↔ IDs 1..4).
type Matrix struct {
	Slots  [MaxMachine]string // empty string = vacant slot
	Wrap   bool               // MatrixSwapFlag: circle mode
	TwoRow bool               // MatrixTwoRowFlag: 2x2; false = 1x4
}

// TypeByte renders 128 | flags.
func (m *Matrix) TypeByte() PackageType {
	t := byte(PtMatrix)
	if m.Wrap {
		t |= MatrixSwapFlag
	}
	if m.TwoRow {
		t |= MatrixTwoRowFlag
	}
	return PackageType(t)
}

// ParseFlags decodes wrap/twoRow from a received Type byte.
func ParseFlags(t PackageType) (wrap, twoRow bool) {
	b := byte(t)
	return b&MatrixSwapFlag != 0, b&MatrixTwoRowFlag != 0
}

// IsEmpty reports an all-vacant matrix — must NEVER be broadcast
// (Windows blind-copies every slot; a fresh server would wipe the peer).
func (m *Matrix) IsEmpty() bool {
	for _, s := range m.Slots {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// AdoptFresh mirrors first-run UpdateSetupMachineMatrix: [self, peer].
func AdoptFresh(selfName, peerName string) Matrix {
	var m Matrix
	m.Slots[0] = selfName
	if peerName != "" && !strings.EqualFold(peerName, selfName) {
		m.Slots[1] = peerName
	}
	return m
}

// Direction is a screen-edge direction for neighbor lookup.
type Direction int

const (
	DirNone Direction = iota
	DirLeft
	DirRight
	DirTop
	DirBottom
)

// Neighbor returns the occupied slot ID adjacent to self in direction d
// (IDNone when no machine lies that way). Vacant slots are skipped;
// horizontal edges wrap only when the matrix has Wrap set.
func (m *Matrix) Neighbor(self uint32, d Direction) uint32 {
	if self < 1 || self > MaxMachine || d == DirNone {
		return IDNone
	}
	occupied := func(slot uint32) bool {
		return slot >= 1 && slot <= MaxMachine && m.Slots[slot-1] != ""
	}
	if !m.TwoRow {
		i := int(self) // 1-based position = slot in 1x4
		step := 0
		switch d {
		case DirRight:
			step = 1
		case DirLeft:
			step = -1
		default:
			return IDNone
		}
		for n := 1; n <= MaxMachine; n++ {
			i += step
			if m.Wrap {
				i = (i-1+MaxMachine*8)%MaxMachine + 1
			} else if i < 1 || i > MaxMachine {
				return IDNone
			}
			if uint32(i) != self && occupied(uint32(i)) {
				return uint32(i)
			}
		}
		return IDNone
	}
	// 2x2: slots [[1,2],[3,4]].
	row, col := (int(self)-1)/2, (int(self)-1)%2
	at := func(r, c int) uint32 {
		if r < 0 || r > 1 || c < 0 || c > 1 {
			return IDNone
		}
		return uint32(r*2 + c + 1)
	}
	switch d {
	case DirRight:
		if s := at(row, col+1); occupied(s) {
			return s
		}
		if m.Wrap {
			if s := at(row, 0); occupied(s) && s != self {
				return s
			}
		}
	case DirLeft:
		if s := at(row, col-1); occupied(s) {
			return s
		}
		if m.Wrap {
			if s := at(row, 1); occupied(s) && s != self {
				return s
			}
		}
	case DirBottom:
		if s := at(row+1, col); occupied(s) {
			return s
		}
	case DirTop:
		if s := at(row-1, col); occupied(s) {
			return s
		}
	}
	return IDNone
}

// SlotOf returns the 1-based ID for name, or 0 if absent (case-insensitive,
// Windows hostnames are case-insensitive).
func (m *Matrix) SlotOf(name string) uint32 {
	for i, s := range m.Slots {
		if s != "" && strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(name)) {
			return uint32(i + 1)
		}
	}
	return IDNone
}

// NewChallenge builds a 64B Handshake packet with a fresh 16B random
// challenge at payload 16..31 and MachineName in the extended area.
func NewChallenge(id int32, src uint32, machineName string) (*Packet, []byte, error) {
	ch := make([]byte, 16)
	if _, err := rand.Read(ch); err != nil {
		return nil, nil, err
	}
	p := &Packet{Type: PtHandshake, ID: id, Src: src, Des: IDAll, HasName: true, MachineName: machineName}
	copy(p.Payload[:], ch)
	return p, ch, nil
}

// AckChallenge transforms a received Handshake into its HandshakeAck:
// Type 127, Src=0 (ID.NONE), own MachineName, payload = bitwise NOT.
func AckChallenge(req *Packet, ownName string, ackID int32) *Packet {
	ack := &Packet{Type: PtHandshakeAck, ID: ackID, Src: IDNone, Des: req.Src, HasName: true, MachineName: ownName}
	for i, b := range req.Payload {
		ack.Payload[i] = ^b
	}
	return ack
}

// VerifyAck checks bytes 16..31 are bitwise NOT of the original challenge.
func VerifyAck(ack *Packet, challenge []byte) bool {
	if len(challenge) != 16 {
		return false
	}
	for i := 0; i < 16; i++ {
		if ack.Payload[i] != ^challenge[i] {
			return false
		}
	}
	return true
}
