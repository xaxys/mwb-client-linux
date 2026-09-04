package protocol_test

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

func TestChecksumCoversBytes2To31(t *testing.T) {
	p := &protocol.Packet{Type: protocol.PtMouse, ID: 7, Src: 1, Des: 2}
	p.SetMouse(protocol.MouseEvent{X: 100, Y: -50, WheelDelta: 0, Flags: 1})
	wire, err := p.Encode(0x01020304)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) != 32 {
		t.Fatalf("want 32B, got %d", len(wire))
	}
	// magic embedding: top 16 bits of 0x01020304 → [2]=0x02 [3]=0x01
	if wire[2] != 0x02 || wire[3] != 0x01 {
		t.Fatalf("bad magic embed: %02x %02x", wire[2], wire[3])
	}
	var sum uint32
	for _, b := range wire[2:32] {
		sum += uint32(b)
	}
	if byte(sum&0xFF) != wire[1] {
		t.Fatal("checksum mismatch")
	}
	dec, err := protocol.Decode(wire, 0x01020304)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Type != protocol.PtMouse || dec.ID != 7 || dec.Src != 1 || dec.Des != 2 {
		t.Fatalf("decode mismatch: %+v", dec)
	}
	m := dec.GetMouse()
	if m.X != 100 || m.Y != -50 || m.Flags != 1 {
		t.Fatalf("mouse payload mismatch: %+v", m)
	}
}

func TestMachineNamePadding(t *testing.T) {
	p := &protocol.Packet{Type: protocol.PtHeartbeat, ID: 1, Src: 1, Des: 255, HasName: true, MachineName: "LINUX-01"}
	wire, err := p.Encode(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) != 64 {
		t.Fatalf("want 64B, got %d", len(wire))
	}
	for i, b := range wire[32:] {
		want := byte(' ')
		if i < len("LINUX-01") {
			want = "LINUX-01"[i]
		}
		if b != want {
			t.Fatalf("pad mismatch at %d: %02x want %02x", i, b, want)
		}
	}
	dec, err := protocol.Decode(wire, 0)
	if err != nil {
		t.Fatal(err)
	}
	if dec.MachineName != "LINUX-01" {
		t.Fatalf("name mismatch: %q", dec.MachineName)
	}
}

func TestKeyboardRoundTrip(t *testing.T) {
	p := &protocol.Packet{Type: protocol.PtKeyboard, ID: 42, Src: 2, Des: 1}
	p.SetKey(protocol.KeyEvent{VK: 0x41, Flags: 0})
	wire, _ := p.Encode(0xABCD1234)
	dec, err := protocol.Decode(wire, 0xABCD1234)
	if err != nil {
		t.Fatal(err)
	}
	k := dec.GetKey()
	if k.VK != 0x41 {
		t.Fatalf("VK mismatch: %d", k.VK)
	}
}

func TestBadMagicRejected(t *testing.T) {
	p := &protocol.Packet{Type: protocol.PtMouse, ID: 1}
	wire, _ := p.Encode(0x11111111)
	if _, err := protocol.Decode(wire, 0x22222222); err != protocol.ErrBadMagic {
		t.Fatalf("want ErrBadMagic, got %v", err)
	}
}

func TestNextMachineHijack(t *testing.T) {
	p := &protocol.Packet{ID: 9, Src: 1, Des: 255}
	p.SetNextMachine(32767, 1000, 2)
	if p.Type != protocol.PtNextMachine {
		t.Fatal("type not NextMachine")
	}
	x, y, id := p.GetNextMachine()
	if x != 32767 || y != 1000 || id != 2 {
		t.Fatalf("hijack mismatch: %d %d %d", x, y, id)
	}
}

func TestRelativeMarker(t *testing.T) {
	rel := protocol.MouseEvent{X: 100 + protocol.MoveMouseRelative, Y: 5}
	if !rel.IsRelative() {
		t.Fatal("should be relative")
	}
	abs := protocol.MouseEvent{X: 30000, Y: 1000}
	if abs.IsRelative() {
		t.Fatal("should be absolute")
	}
}

func TestMatrixFlags(t *testing.T) {
	var m protocol.Matrix
	m.Slots[0] = "A"
	m.Wrap = true
	if byte(m.TypeByte()) != 130 {
		t.Fatalf("want 130, got %d", byte(m.TypeByte()))
	}
	m.TwoRow = true
	if byte(m.TypeByte()) != 134 {
		t.Fatalf("want 134, got %d", byte(m.TypeByte()))
	}
	wrap, two := protocol.ParseFlags(m.TypeByte())
	if !wrap || !two {
		t.Fatal("flag parse failed")
	}
	var empty protocol.Matrix
	if !empty.IsEmpty() {
		t.Fatal("empty matrix must report empty")
	}
	if m.IsEmpty() {
		t.Fatal("non-empty matrix reported empty")
	}
	adopted := protocol.AdoptFresh("SELF", "PEER")
	if adopted.Slots[0] != "SELF" || adopted.Slots[1] != "PEER" {
		t.Fatalf("adopt mismatch: %+v", adopted)
	}
}

func TestHandshakeAckNOT(t *testing.T) {
	req, ch, err := protocol.NewChallenge(1, 1, "LINUX")
	if err != nil {
		t.Fatal(err)
	}
	ack := protocol.AckChallenge(req, "WIN", 2)
	if ack.Type != protocol.PtHandshakeAck || ack.Src != protocol.IDNone {
		t.Fatalf("ack header mismatch: %+v", ack)
	}
	if !protocol.VerifyAck(ack, ch) {
		t.Fatal("verify failed")
	}
	ack.Payload[0] ^= 0xFF
	if protocol.VerifyAck(ack, ch) {
		t.Fatal("corrupt ack must not verify")
	}
}

func TestDedupWindow50(t *testing.T) {
	var d protocol.Dedup
	for i := int32(1); i <= 50; i++ {
		if d.Seen(i) {
			t.Fatalf("id %d wrongly dup", i)
		}
	}
	if !d.Seen(1) {
		t.Fatal("id 1 should be dup")
	}
	// push 50 new IDs → evicts the first window
	for i := int32(51); i <= 100; i++ {
		d.Seen(i)
	}
	if d.Seen(1) != false {
		t.Fatal("id 1 should have been evicted after 50 new ids")
	}
}
