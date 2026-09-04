package input

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

func TestXKeycodeRoundTrip(t *testing.T) {
	for ev := 0; ev <= 247; ev++ {
		kc, ok := XKeycodeFromEvdev(ev)
		if !ok {
			t.Fatalf("evdev %d rejected", ev)
		}
		if int(kc) != ev+8 {
			t.Fatalf("evdev %d -> keycode %d", ev, kc)
		}
		back, ok := EvdevFromXKeycode(kc)
		if !ok || back != ev {
			t.Fatalf("keycode %d -> evdev %d", kc, back)
		}
	}
	if _, ok := XKeycodeFromEvdev(-1); ok {
		t.Fatal("negative evdev accepted")
	}
	if _, ok := XKeycodeFromEvdev(248); ok {
		t.Fatal("evdev 248 accepted")
	}
	if _, ok := EvdevFromXKeycode(7); ok {
		t.Fatal("keycode 7 accepted")
	}
}

func TestHideCounterPairing(t *testing.T) {
	var h hideCounter
	h.hide()
	h.hide()
	if h.pending() != 2 {
		t.Fatalf("pending=%d want 2", h.pending())
	}
	h.show()
	if h.pending() != 1 {
		t.Fatalf("pending=%d want 1", h.pending())
	}
	h.show()
	h.show() // extra show never goes negative
	if h.pending() != 0 {
		t.Fatalf("pending=%d want 0", h.pending())
	}
}

func TestMouseFlagButtonRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		flag int32
		btn  byte
		down bool
	}{
		{MouseLeftDown, 1, true}, {MouseLeftUp, 1, false},
		{MouseRightDown, 3, true}, {MouseRightUp, 3, false},
		{MouseMiddleDown, 2, true}, {MouseMiddleUp, 2, false},
	} {
		btn, down, ok := mouseFlagToButton(tc.flag)
		if !ok || btn != tc.btn || down != tc.down {
			t.Fatalf("flag %#x -> (%d,%v)", tc.flag, btn, down)
		}
		flag, ok := buttonToMouseFlag(tc.btn, tc.down)
		if !ok || flag != tc.flag {
			t.Fatalf("button (%d,%v) -> %#x", tc.btn, tc.down, flag)
		}
	}
	if _, _, ok := mouseFlagToButton(0x1234); ok {
		t.Fatal("bogus flag accepted")
	}
}

func TestEchoSuppress(t *testing.T) {
	b := &x11Backend{}
	b.armEcho(false, 42, true)
	if !b.takeEcho(false, 42, true) {
		t.Fatal("matching echo not taken")
	}
	if b.takeEcho(false, 42, true) {
		t.Fatal("echo taken twice")
	}
	b.armEcho(true, 3, false)
	if b.takeEcho(true, 3, true) {
		t.Fatal("press/release mismatch taken")
	}
	if !b.takeEcho(true, 3, false) {
		t.Fatal("matching button echo not taken")
	}
}

func TestClosedBackendErrors(t *testing.T) {
	b := &x11Backend{}
	if err := b.Inject(Event{Kind: KindKey, VK: 0x41}); err == nil {
		t.Fatal("inject on closed backend succeeded")
	}
	if err := b.HideCursor(); err == nil {
		t.Fatal("hide on closed backend succeeded")
	}
	if err := b.ShowCursor(); err == nil {
		t.Fatal("show on closed backend succeeded")
	}
	if r := b.Bounds(); r != (util.Rect{}) {
		t.Fatalf("bounds on closed backend = %+v", r)
	}
}

func TestNoDisplayFailsHonestly(t *testing.T) {
	t.Setenv("DISPLAY", "")
	if _, err := NewX11Backend(); err == nil {
		t.Fatal("expected error without X server")
	}
}

func TestRecordModeFallsBack(t *testing.T) {
	if _, err := NewX11BackendWithMode(X11CaptureRecord); err == nil {
		t.Fatal("record mode should report unimplemented")
	}
}
