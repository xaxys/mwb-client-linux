package protocol_test

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

func TestNeighborRow(t *testing.T) {
	m := protocol.Matrix{Slots: [4]string{"A", "B", "C", "D"}}
	if got := m.Neighbor(1, protocol.DirRight); got != 2 {
		t.Fatalf("1 right = %d want 2", got)
	}
	if got := m.Neighbor(2, protocol.DirLeft); got != 1 {
		t.Fatalf("2 left = %d want 1", got)
	}
	// No wrap: edges have no neighbor.
	if got := m.Neighbor(4, protocol.DirRight); got != protocol.IDNone {
		t.Fatalf("4 right = %d want none", got)
	}
	if got := m.Neighbor(1, protocol.DirTop); got != protocol.IDNone {
		t.Fatalf("1 top = %d want none", got)
	}
	m.Wrap = true
	if got := m.Neighbor(4, protocol.DirRight); got != 1 {
		t.Fatalf("wrap 4 right = %d want 1", got)
	}
	if got := m.Neighbor(1, protocol.DirLeft); got != 4 {
		t.Fatalf("wrap 1 left = %d want 4", got)
	}
}

func TestNeighborSkipsVacant(t *testing.T) {
	m := protocol.Matrix{Slots: [4]string{"A", "", "", "D"}}
	if got := m.Neighbor(1, protocol.DirRight); got != 4 {
		t.Fatalf("1 right = %d want 4 (vacant slots skipped)", got)
	}
	m.Wrap = true
	if got := m.Neighbor(1, protocol.DirRight); got != 4 {
		t.Fatalf("wrap 1 right = %d want 4", got)
	}
	// Lone machine never neighbors itself.
	solo := protocol.Matrix{Slots: [4]string{"", "B", "", ""}, Wrap: true}
	if got := solo.Neighbor(2, protocol.DirRight); got != protocol.IDNone {
		t.Fatalf("solo 2 right = %d want none", got)
	}
}

func TestNeighborTwoRow(t *testing.T) {
	m := protocol.Matrix{Slots: [4]string{"A", "B", "C", "D"}, TwoRow: true}
	if got := m.Neighbor(1, protocol.DirRight); got != 2 {
		t.Fatalf("1 right = %d want 2", got)
	}
	if got := m.Neighbor(1, protocol.DirBottom); got != 3 {
		t.Fatalf("1 bottom = %d want 3", got)
	}
	if got := m.Neighbor(4, protocol.DirTop); got != 2 {
		t.Fatalf("4 top = %d want 2", got)
	}
	if got := m.Neighbor(2, protocol.DirRight); got != protocol.IDNone {
		t.Fatalf("2 right = %d want none (no wrap)", got)
	}
}

func TestRelativeDelta(t *testing.T) {
	for _, d := range []int{0, 1, -1, 100, -100, 5000} {
		enc := protocol.RelativeDelta(d)
		m := protocol.MouseEvent{X: enc}
		if !m.IsRelative() {
			t.Fatalf("delta %d encoded %d not relative", d, enc)
		}
		var back int
		if enc >= protocol.MoveMouseRelative {
			back = int(enc) - protocol.MoveMouseRelative
		} else {
			back = int(enc) + protocol.MoveMouseRelative
		}
		if back != d {
			t.Fatalf("delta %d round trip = %d", d, back)
		}
	}
}
