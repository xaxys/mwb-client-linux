// Package host runs the local-host direction of the KVM switch: edge
// detection on captured input, delegation to the machine under the cursor
// (NextMachine), and forwarding of keys/mouse while switched away.
//
// Iron rule (from Windows HelperThread/EvSwitch): the capture callback only
// classifies and signals; all network IO and cursor work happens on the Run
// goroutine via the Switcher handoff plus a buffered forward queue.
package host

import (
	"sync"
	"sync/atomic"

	"github.com/xaxys/mwb-client-linux/internal/input"
	mwbnet "github.com/xaxys/mwb-client-linux/internal/net"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// Sender ships input packets to the peer. *net.Client satisfies it.
type Sender interface {
	SendKey(vk, flags int32, src, des uint32) error
	SendMouse(m protocol.MouseEvent, src, des uint32) error
	SendNextMachine(src, dest uint32, entryX, entryY int) error
	SendHideMouse(src, dest uint32) error
	SendSwitched(src, dest uint32) error
}

var _ Sender = (*mwbnet.Client)(nil)

// modifierVKs are released on every switch to avoid stuck modifiers.
var modifierVKs = []int32{0x10, 0x11, 0x12, 0x5B}

// fwdJob is one captured event to forward while switched away. For motion,
// dx/dy are pixel deltas computed on the capture path (the tracker always
// holds the newest seen position).
type fwdJob struct {
	e        input.Event
	dx, dy   int
	hasDelta bool
}

// Host owns one switch direction: this machine as the edge source.
type Host struct {
	backend  input.Backend
	send     Sender
	log      *util.Logger
	self     uint32
	name     string
	switcher *input.Switcher
	fwdCh    chan fwdJob

	mu      sync.Mutex
	matrix  protocol.Matrix
	bounds  util.Rect
	current atomic.Uint32 // machine holding the focus; starts at self
	px, py  int
	hasPos  bool
}

// New creates a Host. matrix is the current layout, self the local slot ID.
func New(backend input.Backend, send Sender, log *util.Logger, self uint32, name string, m protocol.Matrix) *Host {
	h := &Host{backend: backend, send: send, log: log, self: self, name: name,
		switcher: input.NewSwitcher(), fwdCh: make(chan fwdJob, 64), matrix: m}
	h.current.Store(self)
	return h
}

// SetMatrix updates the layout (called when matrix traffic arrives).
func (h *Host) SetMatrix(m protocol.Matrix) {
	h.mu.Lock()
	h.matrix = m
	h.mu.Unlock()
}

// Current reports which machine holds the focus.
func (h *Host) Current() uint32 { return h.current.Load() }

// Run captures input and processes switches until stop closes.
func (h *Host) Run(stop <-chan struct{}) error {
	h.mu.Lock()
	h.bounds = h.backend.Bounds()
	h.mu.Unlock()
	if err := h.backend.StartCapture(h.onCapture); err != nil {
		return err
	}
	defer h.backend.StopCapture()
	for {
		select {
		case <-stop:
			return nil
		case f := <-h.fwdCh:
			h.forward(f)
		case r := <-h.switcher.Chan():
			h.doSwitch(r)
		}
	}
}

// onCapture runs on the capture hot path: classify and signal only.
func (h *Host) onCapture(e input.Event) {
	if e.Kind == input.KindMouseMove {
		h.mu.Lock()
		dx, dy, had := e.X-h.px, e.Y-h.py, h.hasPos
		h.px, h.py, h.hasPos = e.X, e.Y, true
		b := h.bounds
		m := h.matrix
		cur := h.current.Load()
		self := h.self
		h.mu.Unlock()
		if cur != self {
			h.enqueue(fwdJob{e: e, dx: dx, dy: dy, hasDelta: had})
			return
		}
		edge := input.DetectEdge(e.X, e.Y, b, protocol.SkipPixels)
		if edge == input.EdgeNone {
			return
		}
		dest := m.Neighbor(self, toDir(edge))
		if dest == protocol.IDNone || dest == self {
			return
		}
		ex, ey := input.EntryForJump(e.X, e.Y, b, edge, protocol.JumpPixels)
		h.switcher.RequestSwitch(input.SwitchRequest{Edge: edge, EntryX: ex, EntryY: ey, DestID: dest})
		return
	}
	// Keys/buttons/wheel only matter while switched away (local mode lets
	// the backend re-inject them to the focused app).
	if h.current.Load() != h.self {
		h.enqueue(fwdJob{e: e})
	}
}

func (h *Host) enqueue(j fwdJob) {
	select {
	case h.fwdCh <- j:
	default:
		if h.log != nil {
			h.log.Warnf("host: forward queue full, dropping event")
		}
	}
}
func toDir(e input.Edge) protocol.Direction {
	switch e {
	case input.EdgeLeft:
		return protocol.DirLeft
	case input.EdgeRight:
		return protocol.DirRight
	case input.EdgeTop:
		return protocol.DirTop
	case input.EdgeBottom:
		return protocol.DirBottom
	}
	return protocol.DirNone
}

// slotOccupied reports whether slot holds a machine name.
func slotOccupied(m protocol.Matrix, slot uint32) bool {
	return slot >= 1 && slot <= protocol.MaxMachine && m.Slots[slot-1] != ""
}

// doSwitch runs on the Run goroutine: release modifiers, delegate the
// switch, hide locally, and start forwarding.
func (h *Host) doSwitch(r input.SwitchRequest) {
	h.mu.Lock()
	m := h.matrix
	self := h.self
	h.mu.Unlock()
	dest := r.DestID
	if dest == self || !slotOccupied(m, dest) {
		// Layout changed mid-flight: recompute once before giving up.
		dest = m.Neighbor(self, toDir(r.Edge))
		if dest == protocol.IDNone || dest == self {
			return
		}
	}
	for _, vk := range modifierVKs {
		_ = h.send.SendKey(vk, protocol.KeyFlagUp, self, h.current.Load())
	}
	if err := h.send.SendNextMachine(self, dest, r.EntryX, r.EntryY); err != nil {
		if h.log != nil {
			h.log.Warnf("host: nextmachine: %v", err)
		}
		return
	}
	_ = h.send.SendSwitched(self, dest)
	_ = h.backend.HideCursor()
	if f, ok := h.backend.(interface{ SetForwarding(bool) }); ok {
		f.SetForwarding(true)
	}
	h.current.Store(dest)
}

// forward ships one captured event to the machine holding the focus.
func (h *Host) forward(j fwdJob) {
	dest := h.current.Load()
	if dest == h.self {
		return
	}
	e := j.e
	switch e.Kind {
	case input.KindKey:
		flags := protocol.KeyFlagDown
		if !e.KeyDown {
			flags = protocol.KeyFlagUp
		}
		_ = h.send.SendKey(int32(e.VK), flags, h.self, dest)
	case input.KindMouseMove:
		if !j.hasDelta {
			return
		}
		_ = h.send.SendMouse(protocol.MouseEvent{
			X: protocol.RelativeDelta(j.dx),
			Y: protocol.RelativeDelta(j.dy),
		}, h.self, dest)
	case input.KindMouseButton:
		wm, ok := input.MouseFlagToWM(e.MouseFlag)
		if !ok {
			return
		}
		_ = h.send.SendMouse(protocol.MouseEvent{
			X: protocol.RelativeDelta(0), Y: protocol.RelativeDelta(0),
			Flags: int32(wm),
		}, h.self, dest)
	case input.KindMouseWheel:
		_ = h.send.SendMouse(protocol.MouseEvent{
			WheelDelta: int32(e.Wheel), Flags: int32(protocol.WMMouseWheel),
		}, h.self, dest)
	}
}

// OnNextMachine handles an inbound NextMachine addressed to us: take focus
// back, show the cursor, and warp to the normalized entry point.
func (h *Host) OnNextMachine(entryX, entryY int) {
	h.current.Store(h.self)
	if f, ok := h.backend.(interface{ SetForwarding(bool) }); ok {
		f.SetForwarding(false)
	}
	_ = h.backend.ShowCursor()
	h.mu.Lock()
	b := h.bounds
	h.px = util.Denormalize(entryX, b.Left, b.Right)
	h.py = util.Denormalize(entryY, b.Top, b.Bottom)
	h.hasPos = true
	h.mu.Unlock()
	_ = h.backend.Inject(input.Event{Kind: input.KindMouseMove, X: h.px, Y: h.py})
}
