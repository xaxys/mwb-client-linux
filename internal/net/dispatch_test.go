package net

import (
	"sync"
	"testing"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

type sentKey struct {
	vk, flags int32
	src, des  uint32
}
type sentMouse struct {
	m        protocol.MouseEvent
	src, des uint32
}
type sentNext struct {
	src, dest      uint32
	entryX, entryY int
}
type sentHide struct{ src, dest uint32 }

type recordedCalls struct {
	mu       sync.Mutex
	matrix   []protocol.Matrix
	presence []string
	next     []sentNext
	keys     []sentKey
	mice     []sentMouse
	beats    []sentHide
	asks     []sentHide
	hides    int
	sig      chan struct{}
}

func newCalls() *recordedCalls { return &recordedCalls{sig: make(chan struct{}, 256)} }

func (r *recordedCalls) ping() {
	select {
	case r.sig <- struct{}{}:
	default:
	}
}

func (r *recordedCalls) handler() LegHandler {
	return LegHandler{
		OnMatrix: func(m protocol.Matrix) {
			r.mu.Lock()
			r.matrix = append(r.matrix, m)
			r.mu.Unlock()
			r.ping()
		},
		OnPresence: func(name string, id uint32, awake bool) {
			r.mu.Lock()
			r.presence = append(r.presence, name)
			r.mu.Unlock()
			r.ping()
		},
		OnNextMachine: func(x, y int, dest uint32) {
			r.mu.Lock()
			r.next = append(r.next, sentNext{entryX: x, entryY: y, dest: dest})
			r.mu.Unlock()
			r.ping()
		},
		OnKey: func(vk, flags int32, src uint32) {
			r.mu.Lock()
			r.keys = append(r.keys, sentKey{vk, flags, src, 0})
			r.mu.Unlock()
			r.ping()
		},
		OnMouse: func(m protocol.MouseEvent, src uint32) {
			r.mu.Lock()
			r.mice = append(r.mice, sentMouse{m, src, 0})
			r.mu.Unlock()
			r.ping()
		},
		OnHideMouse: func() {
			r.mu.Lock()
			r.hides++
			r.mu.Unlock()
			r.ping()
		},
		OnBeat: func(src uint32, name string, pa int32) {
			r.mu.Lock()
			r.beats = append(r.beats, sentHide{src, 0})
			r.mu.Unlock()
			r.ping()
		},
		OnAsk: func(src uint32, name string, pa int32) {
			r.mu.Lock()
			r.asks = append(r.asks, sentHide{src, 0})
			r.mu.Unlock()
			r.ping()
		},
	}
}

func (r *recordedCalls) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		ok := cond()
		r.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestServeLegDispatch(t *testing.T) {
	const key = "dispatch-test"
	magic := mwbcrypto.Magic24(key)
	s := testServer(key, "LINUX", 0, 0)
	calls := newCalls()
	s.Handler = calls.handler()

	peerSC, servSC := loopbackPair(t, key)
	defer peerSC.Close()
	defer servSC.Close()
	go s.serveLeg(servSC, "WINDOWS", magic)

	sender := NewSender(100)
	send := func(p *protocol.Packet) {
		t.Helper()
		p.ID = sender.Next()
		wire, err := p.Encode(magic)
		if err != nil {
			t.Fatal(err)
		}
		if err := peerSC.WritePacket(wire); err != nil {
			t.Fatal(err)
		}
	}

	// Matrix burst adopts layout [LINUX, WINDOWS] wrap.
	mt := protocol.Matrix{Slots: [4]string{"LINUX", "WINDOWS", "", ""}, Wrap: true}
	for i := 0; i < 4; i++ {
		send(&protocol.Packet{Type: mt.TypeByte(), Src: uint32(i + 1),
			Des: protocol.IDAll, HasName: true, MachineName: mt.Slots[i]})
	}
	calls.waitFor(t, "matrix", func() bool { return len(calls.matrix) > 0 })
	if got := s.pool.IDOf("WINDOWS"); got != 2 {
		t.Fatalf("pool WINDOWS=%d", got)
	}

	// Heartbeat presence.
	send(&protocol.Packet{Type: protocol.PtHeartbeat, Src: 2, Des: protocol.IDAll,
		HasName: true, MachineName: "WINDOWS"})
	calls.waitFor(t, "presence", func() bool { return len(calls.presence) > 0 })

	// NextMachine round trip.
	nm := &protocol.Packet{Src: 2, Des: 1}
	nm.SetNextMachine(100, 200, 1)
	send(nm)
	calls.waitFor(t, "nextmachine", func() bool { return len(calls.next) > 0 })

	// Key + mouse + hide.
	kp := &protocol.Packet{Type: protocol.PtKeyboard, Src: 2, Des: 1}
	kp.SetKey(protocol.KeyEvent{VK: 0x41, Flags: protocol.KeyFlagDown})
	send(kp)
	mp := &protocol.Packet{Type: protocol.PtMouse, Src: 2, Des: 1}
	mp.SetMouse(protocol.MouseEvent{X: 1, Y: 2})
	send(mp)
	send(&protocol.Packet{Type: protocol.PtHideMouse, Src: 2, Des: 1})
	calls.waitFor(t, "input", func() bool {
		return len(calls.keys) > 0 && len(calls.mice) > 0 && calls.hides > 0
	})

	// Beat + ask.
	beat := &protocol.Packet{Type: protocol.PtClipboard, Src: 2, Des: protocol.IDAll,
		HasName: true, MachineName: "WINDOWS"}
	beat.SetPostAction(0)
	send(beat)
	ask := &protocol.Packet{Type: protocol.PtClipboardAsk, Src: 2, Des: 1,
		HasName: true, MachineName: "WINDOWS"}
	ask.SetPostAction(0)
	send(ask)
	calls.waitFor(t, "beat/ask", func() bool {
		return len(calls.beats) > 0 && len(calls.asks) > 0
	})

	// Hello gets a presence reply; bye drops the leg.
	send(&protocol.Packet{Type: protocol.PtHello, Src: 2, Des: protocol.IDAll,
		HasName: true, MachineName: "WINDOWS"})
	raw, err := peerSC.ReadPacket(true)
	if err != nil {
		t.Fatalf("hello reply: %v", err)
	}
	if rp, err := protocol.Decode(raw, magic); err != nil || rp.Type != protocol.PtHeartbeatEx {
		t.Fatalf("hello reply %+v %v", rp, err)
	}
	send(&protocol.Packet{Type: protocol.PtByeBye, Src: 2, Des: 1})
	_ = peerSC.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := peerSC.ReadPacket(false); err == nil {
		t.Fatal("leg survived bye")
	}
}
