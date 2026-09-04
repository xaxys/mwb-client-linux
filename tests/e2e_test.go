package tests

import (
	"net"
	"testing"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	mwbnet "github.com/xaxys/mwb-client-linux/internal/net"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// pipePair runs the MWB stream setup symmetrically over TCP loopback,
// mirroring two peers doing header+noise at once. (net.Pipe is unbuffered
// and deadlocks the full-duplex handshake; real TCP has kernel buffers.)
func pipePair(t *testing.T, key string, current bool) (*mwbcrypto.SecureConn, *mwbcrypto.SecureConn) {
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
		var sc *mwbcrypto.SecureConn
		if current {
			sc, err = mwbcrypto.HandshakeCurrent(c, key)
		} else {
			sc, err = mwbcrypto.HandshakeLegacy(c, key)
		}
		accepted <- hr{sc, err}
	}()
	dialed, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var sca *mwbcrypto.SecureConn
	if current {
		sca, err = mwbcrypto.HandshakeCurrent(dialed, key)
	} else {
		sca, err = mwbcrypto.HandshakeLegacy(dialed, key)
	}
	if err != nil {
		t.Fatalf("dial side setup: %v", err)
	}
	select {
	case r := <-accepted:
		if r.err != nil {
			t.Fatalf("accept side setup: %v", r.err)
		}
		return sca, r.sc
	case <-time.After(10 * time.Second):
		t.Fatal("stream setup timeout")
	}
	return nil, nil
}

func TestE2ECurrentHandshakeMatrixNextMachine(t *testing.T) {
	const key = "e2e-test-key"
	const magic = 0 // computed below
	_ = magic
	sca, scb := pipePair(t, key, true)
	defer sca.Close()
	defer scb.Close()
	m := mwbcrypto.Magic24(key)
	sa := mwbnet.NewSender(0)
	sb := mwbnet.NewSender(1000)

	// 10-round mutual handshake both directions at once.
	type res struct {
		peer string
		err  error
	}
	ca := make(chan res, 1)
	cb := make(chan res, 1)
	go func() {
		p, err := mwbnet.ClientHandshake(sca, m, sa, 1, "LINUX")
		ca <- res{p, err}
	}()
	go func() {
		p, err := mwbnet.ServerHandshake(scb, m, sb, 2, "WINDOWS")
		cb <- res{p, err}
	}()
	var ra, rb res
	timeout := time.After(15 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case r := <-ca:
			ra = r
		case r := <-cb:
			rb = r
		case <-timeout:
			t.Fatal("handshake timeout")
		}
	}
	// drain remaining
	select {
	case r := <-ca:
		ra = r
	default:
	}
	select {
	case r := <-cb:
		rb = r
	default:
	}
	if ra.err != nil || rb.err != nil {
		t.Fatalf("handshake failed: %v %v", ra.err, rb.err)
	}
	if ra.peer != "WINDOWS" || rb.peer != "LINUX" {
		t.Fatalf("peer names: %q %q", ra.peer, rb.peer)
	}

	// Matrix burst (4 packets, Src 1..4, flags 130 = wrap 1x4).
	sendMatrix := func(sc *mwbcrypto.SecureConn, s *mwbnet.Sender) {
		mt := protocol.Matrix{Slots: [4]string{"LINUX", "WINDOWS", "", ""}, Wrap: true}
		ty := mt.TypeByte()
		if byte(ty) != 130 {
			t.Errorf("want type 130, got %d", byte(ty))
		}
		for i := 0; i < 4; i++ {
			p := &protocol.Packet{Type: ty, ID: s.Next(), Src: uint32(i + 1), Des: protocol.IDAll, HasName: true, MachineName: mt.Slots[i]}
			w, _ := p.Encode(m)
			if err := sc.WritePacket(w); err != nil {
				t.Errorf("matrix write: %v", err)
			}
		}
	}
	sendMatrix(scb, sb)
	// read 4 on sca
	var got protocol.Matrix
	for i := 0; i < 4; i++ {
		raw, err := sca.ReadPacket(true)
		if err != nil {
			t.Fatal(err)
		}
		p, err := protocol.Decode(raw, m)
		if err != nil {
			t.Fatal(err)
		}
		if p.Src != uint32(i+1) {
			t.Fatalf("matrix slot %d, Src=%d", i, p.Src)
		}
		wrap, two := protocol.ParseFlags(p.Type)
		if !wrap || two {
			t.Fatalf("flags: wrap=%v two=%v", wrap, two)
		}
		got.Slots[i] = p.MachineName
		got.Wrap = wrap
		got.TwoRow = two
	}
	if got.Slots[0] != "LINUX" || got.Slots[1] != "WINDOWS" {
		t.Fatalf("matrix: %+v", got)
	}

	// NextMachine hijack round trip.
	nm := &protocol.Packet{ID: sa.Next(), Src: 2, Des: 1}
	nm.SetNextMachine(32767, 1000, 2)
	w, _ := nm.Encode(m)
	go func() {
		_ = scb.WritePacket(w)
	}()
	raw, err := sca.ReadPacket(false)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := protocol.Decode(raw, m)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Type != protocol.PtNextMachine {
		t.Fatalf("want NextMachine, got %d", byte(dec.Type))
	}
	x, y, id := dec.GetNextMachine()
	if x != 32767 || y != 1000 || id != 2 {
		t.Fatalf("nextmachine: %d %d %d", x, y, id)
	}
}

func TestE2ELegacyHandshake(t *testing.T) {
	const key = "legacy-key"
	sca, scb := pipePair(t, key, false)
	defer sca.Close()
	defer scb.Close()
	m := mwbcrypto.Magic24(key)
	sa := mwbnet.NewSender(0)
	sb := mwbnet.NewSender(5000)
	ca := make(chan error, 1)
	cb := make(chan error, 1)
	go func() { _, err := mwbnet.ClientHandshake(sca, m, sa, 1, "LINUX"); ca <- err }()
	go func() { _, err := mwbnet.ServerHandshake(scb, m, sb, 2, "WIN7"); cb <- err }()
	timeout := time.After(15 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case err := <-ca:
			if err != nil {
				t.Fatalf("legacy client: %v", err)
			}
		case err := <-cb:
			if err != nil {
				t.Fatalf("legacy server: %v", err)
			}
		case <-timeout:
			t.Fatal("legacy timeout")
		}
	}
}
