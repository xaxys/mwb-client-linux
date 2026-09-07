package host

import (
	"sync"
	"testing"
	"time"

	"github.com/xaxys/mwb-client-linux/internal/input"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

type fakeBackend struct {
	mu         sync.Mutex
	cb         func(input.Event)
	injected   []input.Event
	hidden     int
	forwarding bool
	started    bool
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) StartCapture(cb func(input.Event)) error {
	f.mu.Lock()
	f.cb = cb
	f.started = true
	f.mu.Unlock()
	return nil
}
func (f *fakeBackend) StopCapture() error { return nil }
func (f *fakeBackend) Inject(e input.Event) error {
	f.mu.Lock()
	f.injected = append(f.injected, e)
	f.mu.Unlock()
	return nil
}
func (f *fakeBackend) HideCursor() error {
	f.mu.Lock()
	f.hidden++
	f.mu.Unlock()
	return nil
}
func (f *fakeBackend) ShowCursor() error {
	f.mu.Lock()
	if f.hidden > 0 {
		f.hidden--
	}
	f.mu.Unlock()
	return nil
}
func (f *fakeBackend) Bounds() util.Rect { return util.Rect{Right: 1920, Bottom: 1080} }
func (f *fakeBackend) Close() error      { return nil }
func (f *fakeBackend) SetForwarding(b bool) {
	f.mu.Lock()
	f.forwarding = b
	f.mu.Unlock()
}
func (f *fakeBackend) emit(e input.Event) {
	f.mu.Lock()
	cb := f.cb
	f.mu.Unlock()
	if cb != nil {
		cb(e)
	}
}

type sentKey struct {
	vk, flags int32
	src, des  uint32
}
type sentMouse struct {
	m        protocol.MouseEvent
	src, des uint32
}
type sentNext struct {
	src, dest      uint32
	entryX, entryY int
}
type sentHide struct{ src, dest uint32 }

type fakeSender struct {
	mu       sync.Mutex
	keys     []sentKey
	mice     []sentMouse
	nexts    []sentNext
	hides    []sentHide
	switched []sentHide
	sig      chan struct{}
}

func newFakeSender() *fakeSender { return &fakeSender{sig: make(chan struct{}, 256)} }

func (s *fakeSender) ping() {
	select {
	case s.sig <- struct{}{}:
	default:
	}
}
func (s *fakeSender) SendKey(vk, flags int32, src, des uint32) error {
	s.mu.Lock()
	s.keys = append(s.keys, sentKey{vk, flags, src, des})
	s.mu.Unlock()
	s.ping()
	return nil
}
func (s *fakeSender) SendMouse(m protocol.MouseEvent, src, des uint32) error {
	s.mu.Lock()
	s.mice = append(s.mice, sentMouse{m, src, des})
	s.mu.Unlock()
	s.ping()
	return nil
}
func (s *fakeSender) SendNextMachine(src, dest uint32, entryX, entryY int) error {
	s.mu.Lock()
	s.nexts = append(s.nexts, sentNext{src, dest, entryX, entryY})
	s.mu.Unlock()
	s.ping()
	return nil
}
func (s *fakeSender) SendHideMouse(src, dest uint32) error {
	s.mu.Lock()
	s.hides = append(s.hides, sentHide{src, dest})
	s.mu.Unlock()
	s.ping()
	return nil
}
func (s *fakeSender) SendSwitched(src, dest uint32) error {
	s.mu.Lock()
	s.switched = append(s.switched, sentHide{src, dest})
	s.mu.Unlock()
	s.ping()
	return nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestHostSwitchAwayAndBack(t *testing.T) {
	fb := &fakeBackend{}
	fs := newFakeSender()
	m := protocol.Matrix{Slots: [4]string{"LINUX", "WINDOWS", "", ""}}
	h := New(fb, fs, util.NewLogger("test"), 1, "LINUX", m)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- h.Run(stop) }()
	defer func() { close(stop); <-done }()

	waitFor(t, "capture started", func() bool {
		fb.mu.Lock()
		defer fb.mu.Unlock()
		return fb.started
	})

	// Hit the right edge: delegate to slot 2.
	fb.emit(input.Event{Kind: input.KindMouseMove, X: 1919, Y: 540})
	waitFor(t, "nextmachine", func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return len(fs.nexts) > 0
	})
	fs.mu.Lock()
	nm := fs.nexts[0]
	nkeys := len(fs.keys)
	fs.mu.Unlock()
	if nm.src != 1 || nm.dest != 2 {
		t.Fatalf("nextmachine %+v want src1 des2", nm)
	}
	if nm.entryX != 0 {
		t.Fatalf("entryX=%d want 0", nm.entryX)
	}
	if nm.entryY < 32000 || nm.entryY > 33500 {
		t.Fatalf("entryY=%d want ~32767", nm.entryY)
	}
	if nkeys != 4 {
		t.Fatalf("want 4 modifier releases before switch, got %d", nkeys)
	}
	waitFor(t, "focus away", func() bool { return h.Current() == 2 })
	fb.mu.Lock()
	hidden, fwd := fb.hidden, fb.forwarding
	fb.mu.Unlock()
	if hidden != 1 || !fwd {
		t.Fatalf("hidden=%d fwd=%v want 1/true", hidden, fwd)
	}

	// Key A while away is forwarded, not injected.
	fb.emit(input.Event{Kind: input.KindKey, VK: 0x41, KeyDown: true})
	waitFor(t, "forwarded key", func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return len(fs.keys) > nkeys
	})
	fs.mu.Lock()
	k := fs.keys[len(fs.keys)-1]
	fs.mu.Unlock()
	if k.vk != 0x41 || k.flags != protocol.KeyFlagDown || k.des != 2 {
		t.Fatalf("forwarded key %+v", k)
	}
	fb.mu.Lock()
	ninj := len(fb.injected)
	fb.mu.Unlock()
	if ninj != 0 {
		t.Fatalf("%d local injects while away, want 0", ninj)
	}

	// Motion while away goes relative.
	fb.emit(input.Event{Kind: input.KindMouseMove, X: 1900, Y: 540})
	waitFor(t, "relative mouse", func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return len(fs.mice) > 0
	})
	fs.mu.Lock()
	mm := fs.mice[0]
	nmice := len(fs.mice)
	fs.mu.Unlock()
	if !mm.m.IsRelative() {
		t.Fatalf("mouse %+v not relative", mm.m)
	}

	// Button forwarding goes out as WM_* codes, not MOUSEEVENTF.
	fb.emit(input.Event{Kind: input.KindMouseButton, MouseFlag: input.MouseLeftDown})
	waitFor(t, "forwarded button", func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return len(fs.mice) > nmice
	})
	fs.mu.Lock()
	bm := fs.mice[len(fs.mice)-1]
	fs.mu.Unlock()
	if bm.m.Flags != 0x0201 {
		t.Fatalf("button flags %#x want WM_LBUTTONDOWN", uint32(bm.m.Flags))
	}

	// Peer sends us back: show cursor and warp to entry.
	h.OnNextMachine(32767, 1000)
	waitFor(t, "focus back", func() bool { return h.Current() == 1 })
	fb.mu.Lock()
	hidden, fwd = fb.hidden, fb.forwarding
	ninj = len(fb.injected)
	var warp input.Event
	if ninj > 0 {
		warp = fb.injected[ninj-1]
	}
	fb.mu.Unlock()
	if hidden != 0 || fwd {
		t.Fatalf("hidden=%d fwd=%v want 0/false", hidden, fwd)
	}
	if ninj == 0 {
		t.Fatal("no warp inject on return")
	}
	if warp.X < 900 || warp.X > 1020 || warp.Y < 0 || warp.Y > 40 {
		t.Fatalf("warp %+v want ~(959,16)", warp)
	}
}
