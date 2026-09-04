package protocol_test

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

func TestIsExtendedTable(t *testing.T) {
	big := map[byte]bool{
		3: true, 20: true, 21: true, 51: true,
		69: true, 76: true, 78: true, 79: true,
		124: true, 125: true, 126: true, 127: true,
		128: true, 130: true, 132: true, 134: true,
	}
	for typ := 0; typ <= 134; typ++ {
		got := protocol.PackageType(byte(typ)).IsExtended()
		if got != big[byte(typ)] {
			t.Errorf("type %d IsExtended=%v want %v", typ, got, big[byte(typ)])
		}
	}
}

func TestEncodeSizes(t *testing.T) {
	const magic = 0x01020304
	for typ := 0; typ <= 134; typ++ {
		p := &protocol.Packet{Type: protocol.PackageType(byte(typ)), ID: 1, Src: 1, Des: 2}
		wire, err := p.Encode(magic)
		if err != nil {
			t.Fatal(err)
		}
		want := protocol.PackageSize
		if protocol.PackageType(byte(typ)).IsExtended() {
			want = protocol.PackageSizeEx
		}
		if len(wire) != want {
			t.Errorf("type %d wire=%d want %d", typ, len(wire), want)
		}
	}
}
