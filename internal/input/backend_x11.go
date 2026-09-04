//go:build linux

package input

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xfixes"
	"github.com/BurntSushi/xgb/xinerama"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgb/xtest"

	"github.com/xaxys/mwb-client-linux/internal/keymap"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// X11CaptureMode selects the X11 capture strategy.
//
//   - X11CaptureGrabPoll (A): pure-Go xgb passive grabs (GrabKey/GrabButton)
//   - QueryPointer polling. No cgo, no extra apt packages. Known limits:
//     combos already grabbed by the WM cannot be captured, and edge motion
//     has one poll-interval of latency.
//   - X11CaptureRecord (B): cgo XRecord full-fidelity capture (M2a-B).
//     Requesting it now returns an error so the caller falls back to A.
type X11CaptureMode string

const (
	X11CaptureGrabPoll X11CaptureMode = "grab-poll"
	X11CaptureRecord   X11CaptureMode = "record"
)

// Mouse button flags carried in Event.MouseFlag (MOUSEEVENTF parity).
const (
	MouseLeftDown   = 0x0002
	MouseLeftUp     = 0x0004
	MouseRightDown  = 0x0008
	MouseRightUp    = 0x0010
	MouseMiddleDown = 0x0020
	MouseMiddleUp   = 0x0040
	MouseWheelFlag  = 0x0800
)

// XKeycodeOffset maps evdev codes to X11 keycodes (X adds 8).
const XKeycodeOffset = 8

// XKeycodeFromEvdev maps an evdev code to an X11 keycode. ok=false means the
// evdev code has no X11 keycode (out of byte range).
func XKeycodeFromEvdev(ev int) (kc byte, ok bool) {
	if ev < 0 || ev > 247 {
		return 0, false
	}
	return byte(ev + XKeycodeOffset), true
}

// EvdevFromXKeycode is the inverse of XKeycodeFromEvdev.
func EvdevFromXKeycode(kc byte) (ev int, ok bool) {
	if kc < XKeycodeOffset {
		return 0, false
	}
	return int(kc) - XKeycodeOffset, true
}

// hideCounter pairs XFixes Hide/Show calls so Close can restore a visible
// cursor even if Hide was called without a matching Show.
type hideCounter struct{ n int }

func (h *hideCounter) hide() int {
	h.n++
	return h.n
}

func (h *hideCounter) show() int {
	if h.n > 0 {
		h.n--
	}
	return h.n
}

func (h *hideCounter) pending() int { return h.n }

// pollInterval is the QueryPointer cadence for motion capture (A route).
const pollInterval = 16 * time.Millisecond

// x11Backend: XTest inject + XFixes cursor + Xinerama bounds + grab-poll
// capture (A). The record (B) strategy plugs into captureMode later.
type x11Backend struct {
	mu          sync.Mutex
	conn        *xgb.Conn // requests: inject/query/bounds/cursor
	grab        *xgb.Conn // capture events only
	root        xproto.Window
	captureMode X11CaptureMode
	forwarding  atomic.Bool

	hidden hideCounter

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	echoMu sync.Mutex
	echo   echoSlot
}

// echoSlot suppresses the single echo of our own XTest re-injection:
// a grabbed key is re-injected so local apps keep working, and the echo
// would otherwise loop back through the grab.
type echoSlot struct {
	armed  bool
	button bool // false = key, true = button
	code   byte
	down   bool
}

// NewX11Backend connects on $DISPLAY with the default (grab-poll) capture.
// It fails honestly when there is no X server (e.g. Wayland sessions).
func NewX11Backend() (Backend, error) { return NewX11BackendWithMode(X11CaptureGrabPoll) }

// NewX11BackendWithMode selects the capture strategy. The record (B) mode
// is not implemented yet and returns an error so callers fall back to A.
func NewX11BackendWithMode(mode X11CaptureMode) (Backend, error) {
	if mode == X11CaptureRecord {
		return nil, fmt.Errorf("input: x11 record capture not yet implemented (M2a-B), use grab-poll")
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("input: x11 connect: %w", err)
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("input: x11 xtest: %w", err)
	}
	if err := xfixes.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("input: x11 xfixes: %w", err)
	}
	_ = xinerama.Init(conn) // best-effort: Bounds falls back to setup size
	setup := xproto.Setup(conn)
	if len(setup.Roots) == 0 {
		conn.Close()
		return nil, fmt.Errorf("input: x11 no root screens")
	}
	return &x11Backend{conn: conn, root: setup.Roots[0].Root, captureMode: X11CaptureGrabPoll}, nil
}

func (b *x11Backend) Name() string { return string(BackendX11) }

// SetForwarding switches between local use (false: grabbed keys are
// re-injected so local apps keep working) and switched-away (true: events
// are only reported via the capture callback, never re-injected).
func (b *x11Backend) SetForwarding(f bool) { b.forwarding.Store(f) }

func (b *x11Backend) Bounds() util.Rect {
	b.mu.Lock()
	conn := b.conn
	root := b.root
	b.mu.Unlock()
	if conn == nil {
		return util.Rect{}
	}
	if r, ok := xineramaBounds(conn); ok {
		return r
	}
	setup := xproto.Setup(conn)
	for _, s := range setup.Roots {
		if s.Root == root {
			return util.Rect{Right: int(s.WidthInPixels), Bottom: int(s.HeightInPixels)}
		}
	}
	if len(setup.Roots) > 0 {
		s := setup.Roots[0]
		return util.Rect{Right: int(s.WidthInPixels), Bottom: int(s.HeightInPixels)}
	}
	return util.Rect{}
}

// xineramaBounds unions all screens; ok=false when Xinerama is unavailable.
func xineramaBounds(conn *xgb.Conn) (util.Rect, bool) {
	reply, err := xinerama.QueryScreens(conn).Reply()
	if err != nil || reply == nil || len(reply.ScreenInfo) == 0 {
		return util.Rect{}, false
	}
	r := util.Rect{}
	first := true
	for _, s := range reply.ScreenInfo {
		x0, y0 := int(s.XOrg), int(s.YOrg)
		x1, y1 := x0+int(s.Width), y0+int(s.Height)
		if first {
			r = util.Rect{Left: x0, Top: y0, Right: x1, Bottom: y1}
			first = false
			continue
		}
		if x0 < r.Left {
			r.Left = x0
		}
		if y0 < r.Top {
			r.Top = y0
		}
		if x1 > r.Right {
			r.Right = x1
		}
		if y1 > r.Bottom {
			r.Bottom = y1
		}
	}
	return r, true
}

func (b *x11Backend) Inject(e Event) error {
	b.mu.Lock()
	conn := b.conn
	root := b.root
	b.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("input: x11 backend closed")
	}
	switch e.Kind {
	case KindMouseMove:
		if e.Rel {
			q, err := xproto.QueryPointer(conn, root).Reply()
			if err != nil {
				return err
			}
			dx, dy := clamp16(e.X), clamp16(e.Y)
			xproto.WarpPointer(conn, xproto.WindowNone, xproto.WindowNone,
				0, 0, 0, 0, q.RootX+dx, q.RootY+dy)
			return nil
		}
		xproto.WarpPointer(conn, xproto.WindowNone, root,
			0, 0, 0, 0, int16(e.X), int16(e.Y))
		return nil
	case KindMouseButton:
		btn, down, ok := mouseFlagToButton(e.MouseFlag)
		if !ok {
			return fmt.Errorf("input: x11 unknown mouse flag %#x", e.MouseFlag)
		}
		return fakeButton(conn, root, btn, down)
	case KindMouseWheel:
		steps, btn := e.Wheel, byte(4)
		if steps < 0 {
			btn = 5
			steps = -steps
		}
		for s := 0; s < (steps+119)/120; s++ {
			if err := fakeButton(conn, root, btn, true); err != nil {
				return err
			}
			if err := fakeButton(conn, root, btn, false); err != nil {
				return err
			}
		}
		return nil
	case KindKey:
		ev, ok := keymap.WinVKToEvdev(e.VK)
		if !ok {
			return fmt.Errorf("input: x11 VK %#x unmapped", e.VK)
		}
		kc, ok := XKeycodeFromEvdev(ev)
		if !ok {
			return fmt.Errorf("input: x11 evdev %d out of keycode range", ev)
		}
		return fakeKey(conn, root, kc, e.KeyDown)
	}
	return fmt.Errorf("input: x11 unknown event kind %d", e.Kind)
}

func clamp16(v int) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// fakeKey injects one key transition via XTest.

func fakeKey(conn *xgb.Conn, root xproto.Window, kc byte, down bool) error {
	t := byte(xproto.KeyPress)
	if !down {
		t = byte(xproto.KeyRelease)
	}
	xtest.FakeInput(conn, t, kc, 0, root, 0, 0, 0)
	return nil
}

func fakeButton(conn *xgb.Conn, root xproto.Window, btn byte, down bool) error {
	t := byte(xproto.ButtonPress)
	if !down {
		t = byte(xproto.ButtonRelease)
	}
	xtest.FakeInput(conn, t, btn, 0, root, 0, 0, 0)
	return nil
}

// mouseFlagToButton maps MOUSEEVENTF button transitions to X buttons.
func mouseFlagToButton(flag int32) (btn byte, down bool, ok bool) {
	switch flag {
	case MouseLeftDown:
		return 1, true, true
	case MouseLeftUp:
		return 1, false, true
	case MouseRightDown:
		return 3, true, true
	case MouseRightUp:
		return 3, false, true
	case MouseMiddleDown:
		return 2, true, true
	case MouseMiddleUp:
		return 2, false, true
	}
	return 0, false, false
}

func (b *x11Backend) HideCursor() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil {
		return fmt.Errorf("input: x11 backend closed")
	}
	xfixes.HideCursor(b.conn, b.root)
	b.hidden.hide()
	return nil
}

func (b *x11Backend) ShowCursor() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil {
		return fmt.Errorf("input: x11 backend closed")
	}
	if b.hidden.pending() == 0 {
		return nil
	}
	xfixes.ShowCursor(b.conn, b.root)
	b.hidden.show()
	return nil
}

// StartCapture installs passive grabs and starts the event + poll loops.
// The callback must be non-blocking (Switcher.RequestSwitch parity): it is
// invoked directly from the capture goroutines, never with heavy work.
func (b *x11Backend) StartCapture(cb func(Event)) error {
	b.mu.Lock()
	if b.conn == nil {
		b.mu.Unlock()
		return fmt.Errorf("input: x11 backend closed")
	}
	if b.grab != nil {
		b.mu.Unlock()
		return fmt.Errorf("input: x11 capture already started")
	}
	grab, err := xgb.NewConn()
	if err != nil {
		b.mu.Unlock()
		return fmt.Errorf("input: x11 capture connect: %w", err)
	}
	setup := xproto.Setup(grab)
	minKc, maxKc := int(setup.MinKeycode), int(setup.MaxKeycode)
	if minKc < 8 {
		minKc = 8
	}
	root := b.root
	for kc := minKc; kc <= maxKc; kc++ {
		xproto.GrabKey(grab, true, root, xproto.ModMaskAny,
			xproto.Keycode(kc), xproto.GrabModeAsync, xproto.GrabModeAsync)
	}
	for btn := 1; btn <= 7; btn++ {
		xproto.GrabButton(grab, true, root,
			xproto.EventMaskButtonPress|xproto.EventMaskButtonRelease,
			xproto.GrabModeAsync, xproto.GrabModeAsync,
			xproto.WindowNone, xproto.CursorNone, byte(btn), xproto.ModMaskAny)
	}
	b.grab = grab
	b.stopCh = make(chan struct{})
	b.stopOnce = sync.Once{}
	b.wg.Add(2)
	go b.eventLoop(cb)
	go b.pollLoop(cb)
	b.mu.Unlock()
	return nil
}

func (b *x11Backend) StopCapture() error {
	b.mu.Lock()
	g := b.grab
	b.mu.Unlock()
	if g == nil {
		return nil
	}
	b.stopOnce.Do(func() { close(b.stopCh) })
	b.mu.Lock()
	g = b.grab
	// Close first: unblocks WaitForEvent so the event loop can exit.
	if g != nil {
		setup := xproto.Setup(g)
		for kc := int(setup.MinKeycode); kc <= int(setup.MaxKeycode); kc++ {
			xproto.UngrabKey(g, xproto.Keycode(kc), b.root, xproto.ModMaskAny)
		}
		for btn := 1; btn <= 7; btn++ {
			xproto.UngrabButton(g, byte(btn), b.root, xproto.ModMaskAny)
		}
		g.Close()
		b.grab = nil
	}
	b.mu.Unlock()
	b.wg.Wait()
	return nil
}

// eventLoop translates grabbed key/button events. It never does heavy work:
// only translate, echo-suppress, re-inject (local mode), and invoke cb.
func (b *x11Backend) eventLoop(cb func(Event)) {
	defer b.wg.Done()
	b.mu.Lock()
	g := b.grab
	b.mu.Unlock()
	if g == nil {
		return
	}
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}
		ev, err := g.WaitForEvent()
		if err != nil || ev == nil {
			select {
			case <-b.stopCh:
				return
			default:
				return
			}
		}
		b.handleGrabbed(ev, cb)
	}
}

// handleGrabbed maps one X event to an input.Event. Echoes of our own
// re-injection are dropped; everything else is reported and, in local mode,
// re-injected so the focused app still receives it.
func (b *x11Backend) handleGrabbed(ev xgb.Event, cb func(Event)) {
	b.mu.Lock()
	conn := b.conn
	root := b.root
	b.mu.Unlock()
	switch e := ev.(type) {
	case xproto.KeyPressEvent:
		b.keyGrabbed(conn, root, byte(e.Detail), true, cb)
	case xproto.KeyReleaseEvent:
		b.keyGrabbed(conn, root, byte(e.Detail), false, cb)
	case xproto.ButtonPressEvent:
		b.buttonGrabbed(conn, root, byte(e.Detail), true, int(e.RootX), int(e.RootY), cb)
	case xproto.ButtonReleaseEvent:
		b.buttonGrabbed(conn, root, byte(e.Detail), false, int(e.RootX), int(e.RootY), cb)
	default:
		// MappingNotify and friends: nothing to forward in M2a.
	}
}

func (b *x11Backend) keyGrabbed(conn *xgb.Conn, root xproto.Window, kc byte, down bool, cb func(Event)) {
	if b.takeEcho(false, kc, down) {
		return
	}
	if ev, ok := EvdevFromXKeycode(kc); ok {
		if vk, ok := keymap.EvdevToWinVK(ev); ok {
			cb(Event{Kind: KindKey, VK: vk, KeyDown: down})
		}
	}
	if !b.forwarding.Load() && conn != nil {
		b.armEcho(false, kc, down)
		_ = fakeKey(conn, root, kc, down)
	}
}

func (b *x11Backend) buttonGrabbed(conn *xgb.Conn, root xproto.Window, btn byte, down bool, x, y int, cb func(Event)) {
	if b.takeEcho(true, btn, down) {
		return
	}
	switch btn {
	case 4, 5:
		if down {
			delta := 120
			if btn == 5 {
				delta = -120
			}
			cb(Event{Kind: KindMouseWheel, X: x, Y: y, Wheel: delta})
		}
	case 1, 2, 3:
		if flag, ok := buttonToMouseFlag(btn, down); ok {
			cb(Event{Kind: KindMouseButton, X: x, Y: y, MouseFlag: flag})
		}
	default:
		// Buttons 6/7 (horizontal wheel): M2a scope skips them.
	}
	if !b.forwarding.Load() && conn != nil {
		b.armEcho(true, btn, down)
		_ = fakeButton(conn, root, btn, down)
	}
}

// buttonToMouseFlag maps X buttons to MOUSEEVENTF transitions.
func buttonToMouseFlag(btn byte, down bool) (int32, bool) {
	switch btn {
	case 1:
		if down {
			return MouseLeftDown, true
		}
		return MouseLeftUp, true
	case 2:
		if down {
			return MouseMiddleDown, true
		}
		return MouseMiddleUp, true
	case 3:
		if down {
			return MouseRightDown, true
		}
		return MouseRightUp, true
	}
	return 0, false
}

func (b *x11Backend) armEcho(button bool, code byte, down bool) {
	b.echoMu.Lock()
	b.echo = echoSlot{armed: true, button: button, code: code, down: down}
	b.echoMu.Unlock()
}

func (b *x11Backend) takeEcho(button bool, code byte, down bool) bool {
	b.echoMu.Lock()
	defer b.echoMu.Unlock()
	if b.echo.armed && b.echo.button == button && b.echo.code == code && b.echo.down == down {
		b.echo.armed = false
		return true
	}
	return false
}

// pollLoop emits absolute mouse moves for edge detection.
func (b *x11Backend) pollLoop(cb func(Event)) {
	defer b.wg.Done()
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	lastX, lastY := -1, -1
	for {
		select {
		case <-b.stopCh:
			return
		case <-t.C:
		}
		b.mu.Lock()
		conn := b.conn
		root := b.root
		b.mu.Unlock()
		if conn == nil {
			return
		}
		q, err := xproto.QueryPointer(conn, root).Reply()
		if err != nil || q == nil {
			continue
		}
		x, y := int(q.RootX), int(q.RootY)
		if x != lastX || y != lastY {
			lastX, lastY = x, y
			cb(Event{Kind: KindMouseMove, X: x, Y: y})
		}
	}
}

func (b *x11Backend) Close() error {
	_ = b.StopCapture()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		for b.hidden.pending() > 0 {
			xfixes.ShowCursor(b.conn, b.root)
			b.hidden.show()
		}
		b.conn.Close()
		b.conn = nil
	}
	return nil
}
