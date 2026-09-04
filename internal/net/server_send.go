package net

import (
	"fmt"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// Fan-out senders for server mode (SkSend parity: one ID per packet,
// broadcast over every message leg; the peer dedups globally).
// Clipboard legs (15100) never carry input traffic.

// broadcast encodes once and writes to all non-clip legs.
func (s *Server) broadcast(p *protocol.Packet) error {
	p.ID = s.sender.Next()
	wire, err := p.Encode(s.magic)
	if err != nil {
		return err
	}
	s.mu.Lock()
	var legs []*mwbcrypto.SecureConn
	for _, e := range s.legs {
		if !e.clip {
			legs = append(legs, e.sc)
		}
	}
	s.mu.Unlock()
	if len(legs) == 0 {
		return fmt.Errorf("net: no message legs")
	}
	var lastErr error
	sent := 0
	for _, sc := range legs {
		if err := sc.WritePacket(wire); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	if sent == 0 {
		return lastErr
	}
	return nil
}

// NextID allocates a sender ID from the shared domain (all packets our
// server emits — including clipboard headers built by the daemon — must
// draw from one source or the peer drops them as duplicates).
func (s *Server) NextID() int32 { return s.sender.Next() }

// Layout snapshots our slot and the matrix for daemon wiring.
func (s *Server) Layout() (self uint32, m protocol.Matrix) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slotLocked(), s.matrix
}

func (s *Server) slotLocked() uint32 {
	if slot := s.matrix.SlotOf(s.self); slot != 0 {
		return slot
	}
	return 1
}

// SendKey sends one keyboard packet; releases sleep KeyUpThrottle.
func (s *Server) SendKey(vk, flags int32, src, des uint32) error {
	p := &protocol.Packet{Type: protocol.PtKeyboard, Src: src, Des: des}
	p.SetKey(protocol.KeyEvent{DateTime: nowTicks(), VK: vk, Flags: flags})
	if err := s.broadcast(p); err != nil {
		return err
	}
	if flags&protocol.KeyFlagUp != 0 {
		time.Sleep(protocol.KeyUpThrottle)
	}
	return nil
}

// SendMouse sends one mouse packet.
func (s *Server) SendMouse(m protocol.MouseEvent, src, des uint32) error {
	p := &protocol.Packet{Type: protocol.PtMouse, Src: src, Des: des}
	p.SetMouse(m)
	return s.broadcast(p)
}

// SendNextMachine delegates the switch to dest.
func (s *Server) SendNextMachine(src, dest uint32, entryX, entryY int) error {
	p := &protocol.Packet{Src: src, Des: dest}
	p.SetNextMachine(entryX, entryY, dest)
	return s.broadcast(p)
}

// SendHideMouse hides the cursor on the machine being left.
func (s *Server) SendHideMouse(src, dest uint32) error {
	return s.broadcast(&protocol.Packet{Type: protocol.PtHideMouse, Src: src, Des: dest})
}

// SendSwitched announces arrival at the new machine.
func (s *Server) SendSwitched(src, dest uint32) error {
	return s.broadcast(&protocol.Packet{Type: protocol.PtMachineSwitched, Src: src, Des: dest})
}

// SendBeat announces new local clipboard data.
func (s *Server) SendBeat(src uint32, name string, postAction int32) error {
	p := &protocol.Packet{Type: protocol.PtClipboard, Src: src, Des: protocol.IDAll,
		HasName: true, MachineName: name}
	p.SetPostAction(postAction)
	return s.broadcast(p)
}

// SendAsk requests a push-back from the data holder.
func (s *Server) SendAsk(dest, src uint32, name string, postAction int32) error {
	p := &protocol.Packet{Type: protocol.PtClipboardAsk, Src: src, Des: dest,
		HasName: true, MachineName: name}
	p.SetPostAction(postAction)
	return s.broadcast(p)
}
