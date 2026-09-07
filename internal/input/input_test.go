package input_test

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/input"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

func TestDetectEdge(t *testing.T) {
	b := util.Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	if got := input.DetectEdge(0, 500, b, 1); got != input.EdgeLeft {
		t.Fatalf("left: %v", got)
	}
	if got := input.DetectEdge(1919, 500, b, 1); got != input.EdgeRight {
		t.Fatalf("right: %v", got)
	}
	if got := input.DetectEdge(960, 540, b, 1); got != input.EdgeNone {
		t.Fatalf("center: %v", got)
	}
}

func TestWMMapping(t *testing.T) {
	// Wire WM_* codes around-trip through the internal MOUSEEVENTF flags.
	for _, wm := range []uint32{0x0200, 0x0201, 0x0202, 0x0204, 0x0205, 0x0207, 0x0208, 0x020A} {
		flag, kind, ok := input.MouseFlagFromWM(wm)
		if !ok {
			t.Fatalf("wm %#x rejected", wm)
		}
		if kind == input.WMButton {
			back, ok := input.MouseFlagToWM(flag)
			if !ok || back != wm {
				t.Fatalf("wm %#x -> flag %#x -> %#x", wm, flag, back)
			}
		}
	}
	if _, _, ok := input.MouseFlagFromWM(0x1234); ok {
		t.Fatal("bogus wm accepted")
	}
	if _, ok := input.MouseFlagToWM(0x1234); ok {
		t.Fatal("bogus flag accepted")
	}
}

func TestEntryNormalization(t *testing.T) {
	from := util.Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	ex, ey := input.EntryForJump(1919, 540, from, input.EdgeRight, 2)
	if ex != 0 {
		t.Fatalf("entryX %d", ex)
	}
	if ey < 32700 || ey > 32800 {
		t.Fatalf("entryY %d want ~32767", ey)
	}
}
