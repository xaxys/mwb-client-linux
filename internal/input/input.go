// Package input abstracts capture/injection across X11 / Wayland / evdev.
//
// Runtime iron rule (ported from macOS/Windows): NEVER do heavy work inside
// the capture callback — only set state + signal; a separate goroutine
// (Helper) performs hide/show/crossing. See Switcher below.
//
// Backend selection at runtime (see backend_probe.go):
//  1. X11 session → x11 (XRecord/XInput2 capture + XTest inject + XFixes)
//  2. Wayland GNOME46+ → portal (InputCapture + RemoteDesktop/libei)
//  3. Wayland GNOME42 / portal missing → evdev+uinput (restricted mode)
package input

import (
	"sync"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

// Event is a normalized local input event from any backend.
type Event struct {
	Kind      Kind
	X, Y      int // absolute pixels (for moves) or deltas (relative)
	Rel       bool
	VK        int // Windows VK for keys (mapped by backend via keymap)
	KeyDown   bool
	MouseFlag int32
	Wheel     int
}

type Kind int

const (
	KindMouseMove Kind = iota
	KindMouseButton
	KindMouseWheel
	KindKey
)

// Backend is implemented per platform/session type.
type Backend interface {
	Name() string
	StartCapture(cb func(Event)) error
	StopCapture() error
	Inject(e Event) error
	HideCursor() error
	ShowCursor() error
	Bounds() util.Rect
	Close() error
}

// Edge identifies which screen border was hit.
type Edge int

const (
	EdgeNone Edge = iota
	EdgeLeft
	EdgeRight
	EdgeTop
	EdgeBottom
)

// DetectEdge applies SKIP_PIXELS=1 delegated detection on local bounds.
func DetectEdge(x, y int, bounds util.Rect, skip int) Edge {
	if x < bounds.Left+skip {
		return EdgeLeft
	}
	if x >= bounds.Right-skip {
		return EdgeRight
	}
	if y < bounds.Top+skip {
		return EdgeTop
	}
	if y >= bounds.Bottom-skip {
		return EdgeBottom
	}
	return EdgeNone
}

// EntryForJump computes the 0..65535 normalized entry coord for a horizontal
// or vertical jump, clamping the crossed axis to JUMP_PIXELS=2 re-entry.
func EntryForJump(x, y int, from util.Rect, edge Edge, jump int) (entryX, entryY int) {
	switch edge {
	case EdgeRight, EdgeLeft:
		entryY = util.Normalize(y, from.Top, from.Bottom)
		if edge == EdgeRight {
			entryX = 0
		} else {
			entryX = 65535
		}
	case EdgeTop, EdgeBottom:
		entryX = util.Normalize(x, from.Left, from.Right)
		if edge == EdgeBottom {
			entryY = 0
		} else {
			entryY = 65535
		}
	}
	_ = jump
	return entryX, entryY
}

// Switcher implements the Helper async handoff: the capture callback calls
// RequestSwitch (non-blocking, state+signal only); Run executes the heavy
// hide/show/crossing work on its own goroutine.
type SwitchRequest struct {
	Edge      Edge
	EntryX    int
	EntryY    int
	DestID    uint32
	Returning bool
}

type Switcher struct {
	ch   chan SwitchRequest
	once sync.Once
}

// NewSwitcher creates a handoff channel (depth 1, coalescing).
func NewSwitcher() *Switcher { return &Switcher{ch: make(chan SwitchRequest, 1)} }

// RequestSwitch is safe to call from the capture hot path.
func (s *Switcher) RequestSwitch(r SwitchRequest) {
	select {
	case s.ch <- r:
	default:
		// coalesce: drop stale, keep newest
		select {
		case <-s.ch:
		default:
		}
		select {
		case s.ch <- r:
		default:
		}
	}
}

// Run blocks processing requests until stop is closed.
func (s *Switcher) Run(handle func(SwitchRequest), stop <-chan struct{}) {
	for {
		select {
		case r := <-s.ch:
			handle(r)
		case <-stop:
			return
		}
	}
}
