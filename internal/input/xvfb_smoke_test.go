// Xvfb loopback smoke for the X11 backend (M2a-A).
//
// Runs only with MWB_XVFB_SMOKE=1 and a working $DISPLAY (e.g. Xvfb :99),
// so normal `go test ./...` and CI stay headless-safe. It injects via XTest
// and reads the same events back through the grab-poll capture: a full
// local loopback of inject -> server -> capture -> input.Event.
package input_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/xaxys/mwb-client-linux/internal/input"
)

type eventSink struct {
	mu  sync.Mutex
	evs []input.Event
	ch  chan input.Event
}

func newSink() *eventSink { return &eventSink{ch: make(chan input.Event, 256)} }

func (s *eventSink) cb(e input.Event) {
	s.mu.Lock()
	s.evs = append(s.evs, e)
	s.mu.Unlock()
	select {
	case s.ch <- e:
	default:
	}
}

// waitFor drains events until pred matches or timeout.
func (s *eventSink) waitFor(t *testing.T, what string, timeout time.Duration, pred func(input.Event) bool) input.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case e := <-s.ch:
			if pred(e) {
				return e
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("timeout waiting for %s; got %+v", what, s.all())
	return input.Event{}
}

func (s *eventSink) all() []input.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]input.Event{}, s.evs...)
}

func TestXvfbLoopback(t *testing.T) {
	if os.Getenv("MWB_XVFB_SMOKE") != "1" {
		t.Skip("set MWB_XVFB_SMOKE=1 with an X DISPLAY (e.g. Xvfb :99) to run")
	}
	if os.Getenv("DISPLAY") == "" {
		t.Fatal("DISPLAY is empty; start Xvfb first (Xvfb :99 &)")
	}
	be, err := input.NewX11Backend()
	if err != nil {
		t.Fatalf("connect %s: %v", os.Getenv("DISPLAY"), err)
	}
	defer be.Close()

	b := be.Bounds()
	if b.Right <= 0 || b.Bottom <= 0 {
		t.Fatalf("bogus bounds %+v", b)
	}
	t.Logf("bounds %+v", b)

	if err := be.HideCursor(); err != nil {
		t.Fatalf("hide: %v", err)
	}
	if err := be.ShowCursor(); err != nil {
		t.Fatalf("show: %v", err)
	}

	sink := newSink()
	if err := be.StartCapture(sink.cb); err != nil {
		t.Fatalf("capture: %v", err)
	}
	defer be.StopCapture()

	// Absolute move: poll loop must report the new position.
	if err := be.Inject(input.Event{Kind: input.KindMouseMove, X: 100, Y: 100}); err != nil {
		t.Fatalf("inject move: %v", err)
	}
	mv := sink.waitFor(t, "mouse move near (100,100)", 3*time.Second, func(e input.Event) bool {
		return e.Kind == input.KindMouseMove && abs(e.X-100) < 5 && abs(e.Y-100) < 5
	})
	t.Logf("move: %+v", mv)

	// Key A down/up loopback (X keycode 38 = evdev 30 = VK 0x41).
	if err := be.Inject(input.Event{Kind: input.KindKey, VK: 0x41, KeyDown: true}); err != nil {
		t.Fatalf("inject key down: %v", err)
	}
	kd := sink.waitFor(t, "key A down", 3*time.Second, func(e input.Event) bool {
		return e.Kind == input.KindKey && e.VK == 0x41 && e.KeyDown
	})
	t.Logf("keydown: %+v", kd)
	if err := be.Inject(input.Event{Kind: input.KindKey, VK: 0x41}); err != nil {
		t.Fatalf("inject key up: %v", err)
	}
	ku := sink.waitFor(t, "key A up", 3*time.Second, func(e input.Event) bool {
		return e.Kind == input.KindKey && e.VK == 0x41 && !e.KeyDown
	})
	t.Logf("keyup: %+v", ku)

	// Left button down/up loopback.
	if err := be.Inject(input.Event{Kind: input.KindMouseButton, MouseFlag: input.MouseLeftDown}); err != nil {
		t.Fatalf("inject button down: %v", err)
	}
	bd := sink.waitFor(t, "left down", 3*time.Second, func(e input.Event) bool {
		return e.Kind == input.KindMouseButton && e.MouseFlag == input.MouseLeftDown
	})
	t.Logf("buttondown: %+v", bd)
	if err := be.Inject(input.Event{Kind: input.KindMouseButton, MouseFlag: input.MouseLeftUp}); err != nil {
		t.Fatalf("inject button up: %v", err)
	}
	bu := sink.waitFor(t, "left up", 3*time.Second, func(e input.Event) bool {
		return e.Kind == input.KindMouseButton && e.MouseFlag == input.MouseLeftUp
	})
	t.Logf("buttonup: %+v", bu)

	// Wheel notch loopback (button 4 press -> +120).
	if err := be.Inject(input.Event{Kind: input.KindMouseWheel, Wheel: 120}); err != nil {
		t.Fatalf("inject wheel: %v", err)
	}
	wh := sink.waitFor(t, "wheel +120", 3*time.Second, func(e input.Event) bool {
		return e.Kind == input.KindMouseWheel && e.Wheel == 120
	})
	t.Logf("wheel: %+v", wh)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
