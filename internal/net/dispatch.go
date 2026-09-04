package net

import (
	"log"
	"os"
	"sync/atomic"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// netDebug caps env-gated packet tracing (MWB_DEBUG_NET=1).
var netDebugCount atomic.Int32

func tracePacket(dir string, p *protocol.Packet) {
	if os.Getenv("MWB_DEBUG_NET") == "" {
		return
	}
	if netDebugCount.Add(1) > 60 {
		return
	}
	log.Printf("net-%s type=%d src=%d des=%d id=%d name=%q", dir, byte(p.Type), p.Src, p.Des, p.ID, p.MachineName)
}

// LegHandler receives decoded inbound-leg events. Every field is optional;
// the daemon wires host/clipboard/UI here, the raw server still maintains
// pool/matrix presence on its own.
type LegHandler struct {
	OnMatrix      func(m protocol.Matrix)
	OnPresence    func(name string, id uint32, awake bool)
	OnNextMachine func(entryX, entryY int, dest uint32)
	OnKey         func(vk, flags int32, src uint32)
	OnMouse       func(m protocol.MouseEvent, src uint32)
	OnHideMouse   func()
	OnBeat        func(src uint32, name string, postAction int32)
	OnAsk         func(src uint32, name string, postAction int32)
}

// readAuto reads one packet with PowerToys framing: 32B first, then 32B
// more when the type is extended (checksum covers bytes 2..31 either way).
func readAuto(sc *mwbcrypto.SecureConn) ([]byte, error) {
	head := make([]byte, protocol.PackageSize)
	if err := sc.ReadRaw(head); err != nil {
		return nil, err
	}
	if !protocol.PackageType(head[0]).IsExtended() {
		return head, nil
	}
	tail := make([]byte, protocol.PackageSize)
	if err := sc.ReadRaw(tail); err != nil {
		return nil, err
	}
	return append(head, tail...), nil
}

// serveLeg runs the MainTCPRoutine read loop for one trusted leg until the
// socket closes. It shares the process-wide dedup window (SkSend parity:
// one ID fans out over every leg, so duplicates arrive here).
func (s *Server) serveLeg(sc *mwbcrypto.SecureConn, peer string, magic uint32) {
	defer sc.Close()
	defer s.dropLeg(peer)
	for {
		raw, err := readAuto(sc)
		if err != nil {
			return
		}
		p, err := protocol.Decode(raw, magic)
		if err != nil {
			s.log.Warnf("leg %q: bad packet: %v", peer, err)
			return
		}
		if s.dedup.Seen(p.ID) {
			continue
		}
		tracePacket("rx", p)
		if s.handlePacket(sc, magic, peer, p) {
			return
		}
	}
}

// handlePacket routes one packet; true asks the loop to close the leg.
func (s *Server) handlePacket(sc *mwbcrypto.SecureConn, magic uint32, peer string, p *protocol.Packet) bool {
	// Pool learning from every named packet (MachinePool parity).
	if p.HasName && p.MachineName != "" && p.Src >= 1 && p.Src <= protocol.MaxMachine {
		s.pool.Learn(p.MachineName, p.Src)
	}
	h := s.Handler
	switch p.Type.Base() {
	case protocol.PtHello:
		// Greet back with presence so the newcomer sees us alive.
		slot := s.selfSlot()
		_ = s.sendPresence(sc, magic, slot)
		if h.OnPresence != nil {
			h.OnPresence(p.MachineName, p.Src, false)
		}
	case protocol.PtHeartbeat, protocol.PtAwake, protocol.PtHeartbeatEx:
		s.pool.Touch(p.MachineName)
		if h.OnPresence != nil {
			h.OnPresence(p.MachineName, p.Src, p.Type.Base() == protocol.PtAwake)
		}
	case protocol.PtByeBye:
		s.log.Infof("leg %q: bye", peer)
		return true
	case protocol.PtMatrix:
		s.mergeMatrix(p, h)
	case protocol.PtNextMachine:
		// No Des gate (Receiver parity: whoever gets it switches).
		x, y, dest := p.GetNextMachine()
		if h.OnNextMachine != nil {
			h.OnNextMachine(x, y, dest)
		}
	case protocol.PtKeyboard:
		k := p.GetKey()
		if h.OnKey != nil {
			h.OnKey(k.VK, k.Flags, p.Src)
		}
	case protocol.PtMouse:
		if h.OnMouse != nil {
			h.OnMouse(p.GetMouse(), p.Src)
		}
	case protocol.PtHideMouse:
		if h.OnHideMouse != nil {
			h.OnHideMouse()
		}
	case protocol.PtClipboard:
		if h.OnBeat != nil {
			h.OnBeat(p.Src, p.MachineName, p.GetPostAction())
		}
	case protocol.PtClipboardAsk:
		if h.OnAsk != nil {
			h.OnAsk(p.Src, p.MachineName, p.GetPostAction())
		}
	}
	return false
}

// mergeMatrix folds one matrix slot packet into the server layout; the
// Src==4 packet carries the authoritative flags and commits the view.
func (s *Server) mergeMatrix(p *protocol.Packet, h LegHandler) {
	s.mu.Lock()
	if p.Src >= 1 && p.Src <= protocol.MaxMachine {
		s.matrix.Slots[p.Src-1] = p.MachineName
	}
	if p.Src == protocol.MaxMachine {
		wrap, twoRow := protocol.ParseFlags(p.Type)
		s.matrix.Wrap, s.matrix.TwoRow = wrap, twoRow
	}
	m := s.matrix
	s.mu.Unlock()
	if p.Src == protocol.MaxMachine && h.OnMatrix != nil {
		h.OnMatrix(m)
	}
}

// selfSlot resolves our slot, defaulting to 1 (fresh-adopt parity).
func (s *Server) selfSlot() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot := s.matrix.SlotOf(s.self); slot != 0 {
		return slot
	}
	return 1
}
