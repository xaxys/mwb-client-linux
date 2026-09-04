package net

import (
	"testing"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

func TestServerBroadcast(t *testing.T) {
	const key = "bcast-test"
	magic := mwbcrypto.Magic24(key)
	s := testServer(key, "LINUX", 0, 0)

	msgA, msgB := loopbackPair(t, key)
	defer msgA.Close()
	defer msgB.Close()
	clipA, clipB := loopbackPair(t, key)
	defer clipA.Close()
	defer clipB.Close()

	s.mu.Lock()
	s.legs["WINDOWS"] = &legEntry{sc: msgB}
	s.legs["clip:WINDOWS"] = &legEntry{sc: clipB, clip: true}
	s.mu.Unlock()

	if err := s.SendKey(0x41, protocol.KeyFlagDown, 1, 2); err != nil {
		t.Fatal(err)
	}
	_ = msgA.SetReadDeadline(time.Now().Add(3 * time.Second))
	raw, err := msgA.ReadPacket(false)
	if err != nil {
		t.Fatalf("msg leg got nothing: %v", err)
	}
	p, err := protocol.Decode(raw, magic)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != protocol.PtKeyboard {
		t.Fatalf("type %d", byte(p.Type))
	}
	// Clip legs must stay silent on input traffic.
	_ = clipA.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := clipA.ReadPacket(false); err == nil {
		t.Fatal("clip leg received input traffic")
	}

	if err := s.SendNextMachine(1, 2, 10, 20); err != nil {
		t.Fatal(err)
	}
	_ = msgA.SetReadDeadline(time.Now().Add(3 * time.Second))
	raw, err = msgA.ReadPacket(false)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := protocol.Decode(raw, magic); err != nil || p.Type != protocol.PtNextMachine {
		t.Fatalf("next %+v %v", p, err)
	}

	if self, m := s.Layout(); self != 1 || !m.IsEmpty() {
		t.Fatalf("layout self=%d %+v", self, m)
	}
	if id := s.NextID(); id == 0 {
		t.Fatal("zero ID allocated")
	}
}
