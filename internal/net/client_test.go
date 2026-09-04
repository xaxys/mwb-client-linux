package net

import (
	"net"
	"testing"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// loopbackPair builds two authenticated current-gen legs over TCP loopback.
func loopbackPair(t *testing.T, key string) (*mwbcrypto.SecureConn, *mwbcrypto.SecureConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type hr struct {
		sc  *mwbcrypto.SecureConn
		err error
	}
	accepted := make(chan hr, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- hr{nil, err}
			return
		}
		sc, err := mwbcrypto.HandshakeCurrent(c, key)
		accepted <- hr{sc, err}
	}()
	dialed, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	sca, err := mwbcrypto.HandshakeCurrent(dialed, key)
	if err != nil {
		t.Fatalf("dial setup: %v", err)
	}
	select {
	case r := <-accepted:
		if r.err != nil {
			t.Fatalf("accept setup: %v", r.err)
		}
		return sca, r.sc
	case <-time.After(10 * time.Second):
		t.Fatal("setup timeout")
	}
	return nil, nil
}

func testClient(t *testing.T, sc *mwbcrypto.SecureConn, key string) *Client {
	t.Helper()
	c := NewClient(util.NewLogger("test"))
	c.msgConn = sc
	c.magic = mwbcrypto.Magic24(key)
	return c
}

func readOne(t *testing.T, sc *mwbcrypto.SecureConn, magic uint32, ext bool) *protocol.Packet {
	t.Helper()
	raw, err := sc.ReadPacket(ext)
	if err != nil {
		t.Fatal(err)
	}
	p, err := protocol.Decode(raw, magic)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSendKeyUpThrottles(t *testing.T) {
	const key = "throttle-test"
	sca, scb := loopbackPair(t, key)
	defer sca.Close()
	defer scb.Close()
	magic := mwbcrypto.Magic24(key)
	c := testClient(t, sca, key)

	start := time.Now()
	if err := c.SendKey(0x41, protocol.KeyFlagUp, 1, 2); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < protocol.KeyUpThrottle {
		t.Fatalf("keyup returned after %v, want >= %v", elapsed, protocol.KeyUpThrottle)
	}
	p := readOne(t, scb, magic, false)
	if p.Type != protocol.PtKeyboard || p.Src != 1 || p.Des != 2 {
		t.Fatalf("packet %+v", p)
	}
	if k := p.GetKey(); k.VK != 0x41 || k.Flags != protocol.KeyFlagUp {
		t.Fatalf("key %+v", k)
	}

	start = time.Now()
	if err := c.SendKey(0x41, protocol.KeyFlagDown, 1, 2); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= protocol.KeyUpThrottle {
		t.Fatalf("keydown slept %v, want no throttle", elapsed)
	}
	p = readOne(t, scb, magic, false)
	if k := p.GetKey(); k.Flags != protocol.KeyFlagDown {
		t.Fatalf("key %+v", k)
	}
}

func TestSendMouseNextHideSwitched(t *testing.T) {
	const key = "send-test"
	sca, scb := loopbackPair(t, key)
	defer sca.Close()
	defer scb.Close()
	magic := mwbcrypto.Magic24(key)
	c := testClient(t, sca, key)

	if err := c.SendMouse(protocol.MouseEvent{X: protocol.RelativeDelta(5), Y: protocol.RelativeDelta(-3)}, 1, 2); err != nil {
		t.Fatal(err)
	}
	if p := readOne(t, scb, magic, false); p.Type != protocol.PtMouse {
		t.Fatalf("type %d want mouse", byte(p.Type))
	}

	if err := c.SendNextMachine(1, 2, 0, 32767); err != nil {
		t.Fatal(err)
	}
	p := readOne(t, scb, magic, false)
	if p.Type != protocol.PtNextMachine || p.Src != 1 || p.Des != 2 {
		t.Fatalf("next %+v", p)
	}
	if x, y, id := p.GetNextMachine(); x != 0 || y != 32767 || id != 2 {
		t.Fatalf("next payload %d %d %d", x, y, id)
	}

	if err := c.SendHideMouse(1, 2); err != nil {
		t.Fatal(err)
	}
	if p := readOne(t, scb, magic, false); p.Type != protocol.PtHideMouse {
		t.Fatalf("type %d want hidemouse", byte(p.Type))
	}

	if err := c.SendSwitched(1, 2); err != nil {
		t.Fatal(err)
	}
	if p := readOne(t, scb, magic, false); p.Type != protocol.PtMachineSwitched {
		t.Fatalf("type %d want switched", byte(p.Type))
	}
}
