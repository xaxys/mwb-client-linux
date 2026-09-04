package clipboard_test

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/clipboard"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

func TestTextFastPathRoundTrip(t *testing.T) {
	const magic = 0x12345678
	orig := "hello MWB clipboard — 你好"
	comp, err := clipboard.CompressText(orig)
	if err != nil {
		t.Fatal(err)
	}
	var wires [][]byte
	id := int32(100)
	for off := 0; off < len(comp); off += clipboard.ChunkSize {
		end := off + clipboard.ChunkSize
		if end > len(comp) {
			end = len(comp)
		}
		// pad final chunk to 48B like the sender (zeros)
		chunk := make([]byte, clipboard.ChunkSize)
		copy(chunk, comp[off:end])
		w, err := clipboard.EncodeChunkPacket(protocol.PtClipboardText, id, 1, 2, chunk, magic)
		if err != nil {
			t.Fatal(err)
		}
		wires = append(wires, w)
		id++
	}
	var acc clipboard.Accumulator
	for _, w := range wires {
		p, err := protocol.Decode(w, magic)
		if err != nil {
			t.Fatal(err)
		}
		done, err := acc.Feed(p, w)
		if err != nil || done {
			t.Fatalf("feed err=%v done=%v", err, done)
		}
	}
	// terminator
	term := &protocol.Packet{Type: protocol.PtClipboardDataEnd, ID: id, Src: 1, Des: 2}
	tw, _ := term.Encode(magic)
	tp, _ := protocol.Decode(tw, magic)
	done, err := acc.Feed(tp, tw)
	if err != nil || !done {
		t.Fatalf("terminator err=%v done=%v", err, done)
	}
	back, err := acc.Text()
	if err != nil {
		t.Fatal(err)
	}
	if back != orig {
		t.Fatalf("round trip mismatch: %q", back)
	}
}

func TestSmallTextSingleChunk(t *testing.T) {
	comp, _ := clipboard.CompressText("hi")
	if len(comp) > clipboard.ChunkSize {
		t.Skip("compressed larger than one chunk on this platform")
	}
}
