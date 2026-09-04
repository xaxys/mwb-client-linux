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
