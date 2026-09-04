package net

import (
	"net"
	"strconv"
	"testing"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

func testServer(key, self string, msgPort, clipPort int) *Server {
	return NewServer(util.NewLogger("test"), msgPort, clipPort, key, self, protocol.ProtoCurrent)
}

// mockAcceptor runs one server-side handshake on ln and reports the peer.
func mockAcceptor(t *testing.T, ln net.Listener, key string, magic uint32) chan string {
	t.Helper()
	out := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		sc, err := mwbcrypto.HandshakeCurrent(c, key)
		if err != nil {
			return
		}
		defer sc.Close()
		peer, err := ServerHandshake(sc, magic, NewSender(9000), 9, "MOCK")
		if err != nil {
			return
		}
		out <- peer
	}()
	return out
}

func waitLegs(t *testing.T, s *Server, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		_, ok := s.legs[name]
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for leg %q", name)
}

func TestTrustBurstAndDialBack(t *testing.T) {
	const key = "m4-test"
	magic := mwbcrypto.Magic24(key)

	// Dial-back target: a mock MWB acceptor.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	mockPort := ln.Addr().(*net.TCPAddr).Port
	peerCh := mockAcceptor(t, ln, key, magic)

	s := testServer(key, "LINUX", mockPort, 0)
	inbound, peerSC := loopbackPair(t, key)
	defer peerSC.Close()
	defer inbound.Close()

	// trustPeer ends by becoming the leg reader; run it async.
	trusted := make(chan struct{})
	go func() {
		defer close(trusted)
		s.trustPeer(inbound, "WINDOWS", "127.0.0.1", magic)
	}()

	// Presence: Heartbeat_ex from slot 1 as LINUX.
	p := readOne(t, peerSC, magic, true)
	if p.Type != protocol.PtHeartbeatEx || p.Src != 1 || p.MachineName != "LINUX" {
		t.Fatalf("presence %+v", p)
	}
	// Matrix burst: Src 1..4, adopted [LINUX, WINDOWS].
	var slots [4]string
	for i := 0; i < 4; i++ {
		p = readOne(t, peerSC, magic, true)
		if p.Src != uint32(i+1) {
			t.Fatalf("burst %d Src=%d", i, p.Src)
		}
		slots[i] = p.MachineName
	}
	if slots[0] != "LINUX" || slots[1] != "WINDOWS" {
		t.Fatalf("burst slots %q", slots)
	}
	if got := s.pool.IDOf("WINDOWS"); got != 2 {
		t.Fatalf("pool WINDOWS=%d want 2", got)
	}

	// Mesh dial-back reaches the mock and handshakes as LINUX.
	select {
	case peer := <-peerCh:
		if peer != "LINUX" {
			t.Fatalf("dial-back peer %q", peer)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("dial-back never arrived")
	}
	waitLegs(t, s, "WINDOWS#out")
}

func TestClipboardLegValidation(t *testing.T) {
	const key = "clip-test"
	magic := mwbcrypto.Magic24(key)
	s := testServer(key, "LINUX", 0, 0)
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	msgPort := s.MsgAddr().(*net.TCPAddr).Port
	clipPort := s.ClipAddr().(*net.TCPAddr).Port
	dial := func(port int) net.Conn {
		c, err := net.Dial("tcp", joinPort(port))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// Message leg as WINDOWS (drains presence + burst).
	mc := dial(msgPort)
	msc, err := mwbcrypto.HandshakeCurrent(mc, key)
	if err != nil {
		t.Fatal(err)
	}
	sender := NewSender(0)
	if _, err := ClientHandshake(msc, magic, sender, 2, "WINDOWS"); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := msc.ReadPacket(true); err != nil {
			t.Fatalf("burst drain: %v", err)
		}
	}

	push := func(name string) *mwbcrypto.SecureConn {
		cc := dial(clipPort)
		csc, err := mwbcrypto.HandshakeCurrent(cc, key)
		if err != nil {
			t.Fatal(err)
		}
		p := &protocol.Packet{Type: protocol.PtClipboardPush, ID: sender.Next(),
			Src: 2, Des: 0, HasName: true, MachineName: name}
		p.SetPostAction(0)
		wire, _ := p.Encode(magic)
		if err := csc.WritePacket(wire); err != nil {
			t.Fatal(err)
		}
		return csc
	}

	// Known peer stages a clip leg.
	known := push("WINDOWS")
	defer known.Close()
	waitLegs(t, s, "clip:WINDOWS")

	// Stranger is dropped.
	stranger := push("STRANGER")
	defer stranger.Close()
	_ = stranger.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := stranger.ReadPacket(true); err == nil {
		t.Fatal("stranger clip leg accepted")
	}
	s.mu.Lock()
	_, staged := s.legs["clip:STRANGER"]
	s.mu.Unlock()
	if staged {
		t.Fatal("stranger clip leg staged")
	}
}

func joinPort(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
