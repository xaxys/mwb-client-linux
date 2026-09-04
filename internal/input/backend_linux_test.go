package input

import (
	"encoding/binary"
	"testing"
)

func TestInputEventCodec(t *testing.T) {
	raw := make([]byte, 24)
	binary.LittleEndian.PutUint64(raw[0:8], 1711111111)
	binary.LittleEndian.PutUint64(raw[8:16], 222222)
	binary.LittleEndian.PutUint16(raw[16:18], evRel)
	binary.LittleEndian.PutUint16(raw[18:20], relX)
	neg7 := int32(-7)
	binary.LittleEndian.PutUint32(raw[20:24], uint32(neg7))
	e, ok := decodeInputEvent(raw)
	if !ok || e.Type != evRel || e.Code != relX || e.Value != -7 {
		t.Fatalf("%+v %v", e, ok)
	}
	if back := encodeInputEvent(e); string(back) != string(raw) {
		t.Fatal("codec not symmetric")
	}
	if _, ok := decodeInputEvent(raw[:10]); ok {
		t.Fatal("short buffer accepted")
	}
}

func TestRelAccum(t *testing.T) {
	var a relAccum
	a.addMotion(3, -2)
	a.addMotion(-1, 0)
	a.addWheel(120)
	dx, dy, w, has := a.flush()
	if !has || dx != 2 || dy != -2 || w != 120 {
		t.Fatalf("%d %d %d %v", dx, dy, w, has)
	}
	if _, _, _, has := a.flush(); has {
		t.Fatal("flush not clearing")
	}
}

func TestEvdevButtons(t *testing.T) {
	if f, ok := btnToMouseFlag(btnLeft, true); !ok || f != MouseLeftDown {
		t.Fatalf("%#x %v", f, ok)
	}
	if f, ok := btnToMouseFlag(btnRight, false); !ok || f != MouseRightUp {
		t.Fatalf("%#x %v", f, ok)
	}
	if f, ok := btnToMouseFlag(btnMiddle, true); !ok || f != MouseMiddleDown {
		t.Fatalf("%#x %v", f, ok)
	}
	if _, ok := btnToMouseFlag(0x999, true); ok {
		t.Fatal("bogus button accepted")
	}
	// shares the MOUSEEVENTF value space with the x11 backend
	if f, _, _ := mouseFlagToButton(MouseLeftDown); f != 1 {
		t.Fatalf("button %d", f)
	}
}

func TestParseDrmMode(t *testing.T) {
	w, h, ok := parseDrmMode("1920x1080\n1600x900\n")
	if !ok || w != 1920 || h != 1080 {
		t.Fatalf("%d %d %v", w, h, ok)
	}
	if _, _, ok := parseDrmMode(""); ok {
		t.Fatal("empty accepted")
	}
	if _, _, ok := parseDrmMode("unknown\n"); ok {
		t.Fatal("garbage accepted")
	}
}

func TestIoctlNumbers(t *testing.T) {
	// Pinned against linux/input.h + linux/uinput.h derivation
	// (_IOC(dir,type,nr,size) = dir<<30 | size<<16 | type<<8 | nr).
	for name, vw := range map[string]struct{ got, want uint }{
		"create":  {ioctlUIDevCreate, 0x5501},
		"destroy": {ioctlUIDevDestroy, 0x5502},
		"setup":   {ioctlUIDevSetup, 0x405c5503},
		"evbit":   {ioctlUISetEvbit, 0x40045564},
		"grab":    {ioctlEvGrab, 0x40044590},
	} {
		if vw.got != vw.want {
			t.Fatalf("%s=%#x want %#x", name, vw.got, vw.want)
		}
	}
}

func TestDrmBoundsNoCrash(t *testing.T) {
	r, ok := drmBounds()
	t.Logf("drm bounds %+v ok=%v", r, ok)
	if ok && (r.Right <= 0 || r.Bottom <= 0) {
		t.Fatalf("bogus %+v", r)
	}
	// Public Bounds always returns something usable.
	b := (&evdevBackend{}).Bounds()
	if b.Right <= 0 || b.Bottom <= 0 {
		t.Fatalf("fallback %+v", b)
	}
}
