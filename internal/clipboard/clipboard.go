// Package clipboard implements the MWB clipboard paths (M0: text fast path).
//
// Fast path (<1MB): Deflate-compress text, split into 48B chunks carried in
// bytes 16..63 of ClipboardText(124)/ClipboardImage(125) packets, terminated
// by ClipboardDataEnd (76 in-band / 77 secondary). Receiver concatenates the
// trailing 48B of each packet until the terminator.
//
// Pull path (>1MB / files): Clipboard(69) notify → ClipboardAsk(78) →
// secondary TCP to 15100 + Clipboard ShakeHand (header+noise+Push header).
// Image/file payload transfer is stubbed (M3) — interfaces left in place.
package clipboard

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"

	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// MaxInstantBytes mirrors MAX_CLIPBOARD_DATA_SIZE_CAN_BE_SENT_INSTANTLY_TCP.
const MaxInstantBytes = 1 << 20 // 1MB

// ChunkSize is DATA_SIZE: payload bytes per packet (offsets 16..63).
const ChunkSize = 48

// CompressText deflates text before chunking (DeflateStream parity).
func CompressText(s string) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte(s)); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecompressText inflates a fast-path text payload.
func DecompressText(b []byte) (string, error) {
	r := flate.NewReader(bytes.NewReader(b))
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// EncodeFastPath splits compressed bytes into 48B-chunk packets.
// typ is ClipboardText or ClipboardImage; ids increment from startID.
func EncodeFastPath(typ protocol.PackageType, data []byte, src, des uint32, startID int32) []*protocol.Packet {
	var out []*protocol.Packet
	id := startID
	for off := 0; off < len(data); off += ChunkSize {
		end := off + ChunkSize
		if end > len(data) {
			end = len(data)
		}
		p := &protocol.Packet{Type: typ, ID: id, Src: src, Des: des, HasName: true}
		// Chunk occupies bytes 16..63; first 16B overlap header-union, rest
		// extend into MachineName area. Model: payload[0:16] + name-area reuse.
		// For testability we store chunk in an ancillary field via encoding:
		// EncodeChunkPacket handles the wire layout (see below).
		p.Payload = chunkHead(data[off:end])
		p.MachineName = chunkTailName(data[off:end])
		id++
		out = append(out, p)
	}
	term := &protocol.Packet{Type: protocol.PtClipboardDataEndFast, ID: id, Src: src, Des: des}
	out = append(out, term)
	return out
}

// chunkHead returns first up-to-16B of a chunk for Payload.
func chunkHead(chunk []byte) [16]byte {
	var a [16]byte
	copy(a[:], chunk)
	return a
}

// chunkTailName packs remaining up-to-32B into the MachineName slot as raw
// bytes (Latin-1 round-trip). Wire-accurate: bytes 32..63 are raw chunk data.
func chunkTailName(chunk []byte) string {
	if len(chunk) <= 16 {
		return ""
	}
	tail := chunk[16:]
	if len(tail) > 32 {
		tail = tail[:32]
	}
	// store as string preserving bytes
	var buf bytes.Buffer
	buf.Write(tail)
	// pad is implicit (Encode pads); keep raw
	return string(buf.Bytes())
}

// EncodeChunkPacket renders one fast-path chunk to exact 64B wire form.
// Layout: header(16B) + chunk[0:48] at 16..63.
func EncodeChunkPacket(typ protocol.PackageType, id int32, src, des uint32, chunk []byte, magic uint32) ([]byte, error) {
	if len(chunk) > ChunkSize {
		return nil, fmt.Errorf("chunk too big: %d", len(chunk))
	}
	// Build raw directly to keep bytes 32..63 raw (not space-padded name).
	p := &protocol.Packet{Type: typ, ID: id, Src: src, Des: des, HasName: true}
	copy(p.Payload[:], chunk)
	if len(chunk) > 16 {
		// stash tail so Encode() writes it; then overwrite padding effect by
		// post-filling raw bytes (Encode space-pads short names).
		p.MachineName = string(chunk[16:])
	}
	wire, err := p.Encode(magic)
	if err != nil {
		return nil, err
	}
	// Fix tail area to raw bytes (Encode pads with 0x20; short final chunks
	// must be zero-padded per Zeros semantics — keep Encode output for the
	// bytes beyond the chunk, but ensure chunk bytes are exact).
	copy(wire[16:16+len(chunk)], chunk)
	return wire, nil
}

// Accumulator buffers fast-path chunks until ClipboardDataEnd.
type Accumulator struct {
	buf []byte
}

// Feed consumes a decoded packet; done=true on terminator (76/77).
func (a *Accumulator) Feed(p *protocol.Packet, wire []byte) (done bool, err error) {
	switch p.Type {
	case protocol.PtClipboardText, protocol.PtClipboardImage:
		if len(wire) != protocol.PackageSizeEx {
			return false, fmt.Errorf("clipboard chunk must be 64B")
		}
		a.buf = append(a.buf, wire[16:64]...)
		// Trim padding: only the final chunk is short; exact length unknown
		// until terminator — but fast path pads final chunk with zeros/spaces.
		// PowerToys pads with zeros (Zeros CBC padding parity); receiver knows
		// total from... in practice trailing zeros of Deflate stream are NOT
		// significant — inflate stops at stream end. We keep raw and let
		// Bytes()/Text() trim trailing 0x00/0x20 added by the final chunk.
		return false, nil
	case protocol.PtClipboardDataEndFast, protocol.PtClipboardDataEnd:
		return true, nil
	default:
		return false, fmt.Errorf("not a clipboard packet: %d", byte(p.Type))
	}
}

// Bytes returns accumulated raw bytes with final-chunk padding trimmed.
func (a *Accumulator) Bytes() []byte {
	b := bytes.TrimRight(a.buf, "\x00 ")
	return append([]byte{}, b...)
}

// Text inflates accumulated bytes to text.
func (a *Accumulator) Text() (string, error) { return DecompressText(a.Bytes()) }

// Reset clears the accumulator.
func (a *Accumulator) Reset() { a.buf = nil }
