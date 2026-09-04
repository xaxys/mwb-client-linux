//go:build linux

package input

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/xaxys/mwb-client-linux/internal/keymap"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// portalBackend: InputCapture capture + RemoteDesktop/libei inject (GNOME46+,
// 24.04 Wayland). M2b (needs portal D-Bus APIs + 24.04 hardware).
type portalBackend struct{}

func NewPortalBackend() (Backend, error) { return &portalBackend{}, nil }

func (b *portalBackend) Name() string { return string(BackendPortal) }
func (b *portalBackend) StartCapture(cb func(Event)) error {
	return fmt.Errorf("input: portal capture not yet implemented (M2b)")
}
func (b *portalBackend) StopCapture() error { return nil }
func (b *portalBackend) Inject(e Event) error {
	return fmt.Errorf("input: portal inject not yet implemented (M2b)")
}
func (b *portalBackend) HideCursor() error {
	return fmt.Errorf("input: portal hide not yet implemented (M2b)")
}
func (b *portalBackend) ShowCursor() error {
	return fmt.Errorf("input: portal show not yet implemented (M2b)")
}
func (b *portalBackend) Bounds() util.Rect { return util.Rect{} }
func (b *portalBackend) Close() error      { return nil }

// evdev ioctl numbers, derived from linux/input.h + linux/uinput.h
// (_IOC(dir,type,nr,size) = dir<<30 | size<<16 | type<<8 | nr).
const (
	ioctlUIDevCreate  = 0x5501
	ioctlUIDevDestroy = 0x5502
	ioctlUIDevSetup   = 0x405c5503 // _IOW('U', 3, struct uinput_setup[92])
	ioctlUISetEvbit   = 0x40045564 // _IOW('U', 100, int)
	ioctlUISetKeybit  = 0x40045565 // _IOW('U', 101, int)
	ioctlUISetRelbit  = 0x40045566 // _IOW('U', 102, int)
	ioctlEvGrab       = 0x40044590 // _IOW('E', 0x90, int)
	ioctlEvName64     = 0x80404506 // _IOR('E', 0x06, 64)
	ioctlEvBitRel     = 0x80084522 // _IOR('E', 0x22, 8)
	ioctlEvBitKey     = 0x80604521 // _IOR('E', 0x21, 96)
	ioctlEvBitTypes   = 0x80084520 // _IOR('E', 0x20, 8)
)

// evdev event / code constants (linux/input-event-codes.h).
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	synReport = 0

	relX        = 0x00
	relY        = 0x01
	relHWheel   = 0x06
	relWheel    = 0x08
	relWheelHR  = 0x0b // high-res: already 120ths of a detent
	relHWheelHR = 0x0c

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
	btnTouch  = 0x14a

	keyA = 30 // keyboards have the letter block; mice do not
)

// inputEvent is the 24B kernel struct (LP64 timeval + u16 + u16 + s32).
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

func decodeInputEvent(b []byte) (inputEvent, bool) {
	var e inputEvent
	if len(b) < 24 {
		return e, false
	}
	e.Sec = int64(binary.LittleEndian.Uint64(b[0:8]))
	e.Usec = int64(binary.LittleEndian.Uint64(b[8:16]))
	e.Type = binary.LittleEndian.Uint16(b[16:18])
	e.Code = binary.LittleEndian.Uint16(b[18:20])
	e.Value = int32(binary.LittleEndian.Uint32(b[20:24]))
	return e, true
}

func encodeInputEvent(e inputEvent) []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint64(b[0:8], uint64(e.Sec))
	binary.LittleEndian.PutUint64(b[8:16], uint64(e.Usec))
	binary.LittleEndian.PutUint16(b[16:18], e.Type)
	binary.LittleEndian.PutUint16(b[18:20], e.Code)
	binary.LittleEndian.PutUint32(b[20:24], uint32(e.Value))
	return b
}

// ioctl issues a raw ioctl with an optional pointer argument.
func ioctl(fd uintptr, req uint, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(req), arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// btnToMouseFlag maps kernel buttons to MOUSEEVENTF transitions.
func btnToMouseFlag(btn uint16, down bool) (int32, bool) {
	switch btn {
	case btnLeft:
		if down {
			return MouseLeftDown, true
		}
		return MouseLeftUp, true
	case btnMiddle:
		if down {
			return MouseMiddleDown, true
		}
		return MouseMiddleUp, true
	case btnRight:
		if down {
			return MouseRightDown, true
		}
		return MouseRightUp, true
	}
	return 0, false
}

// relAccum coalesces REL motion between SYN_REPORTs (one Event per sync,
// matching the kernel's own framing).
type relAccum struct {
	dx, dy, wheel int32
	has           bool
}

func (a *relAccum) addMotion(dx, dy int32) { a.dx += dx; a.dy += dy; a.has = true }
func (a *relAccum) addWheel(w int32)       { a.wheel += w; a.has = true }
func (a *relAccum) flush() (dx, dy, wheel int32, has bool) {
	dx, dy, wheel, has = a.dx, a.dy, a.wheel, a.has
	*a = relAccum{}
	return dx, dy, wheel, has
}

// evdevDev is one opened /dev/input/event* node.
type evdevDev struct {
	f     *os.File
	path  string
	mouse bool // has REL_X/Y
	kbd   bool // has KEY_A (letter block)
}

// probeDev opens a node and classifies it; nil means unusable here.
func probeDev(path string) *evdevDev {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil
	}
	types := make([]byte, 8)
	if err := ioctl(f.Fd(), ioctlEvBitTypes, uintptr(unsafe.Pointer(&types[0]))); err != nil {
		f.Close()
		return nil
	}
	has := func(bit int) bool { return types[bit/8]&(1<<(uint(bit)%8)) != 0 }
	d := &evdevDev{f: f, path: path}
	if has(evRel) {
		rel := make([]byte, 8)
		if ioctl(f.Fd(), ioctlEvBitRel, uintptr(unsafe.Pointer(&rel[0]))) == nil {
			if rel[0]&0x03 == 0x03 { // REL_X | REL_Y
				d.mouse = true
			}
		}
	}
	if has(evKey) {
		keys := make([]byte, 96)
		if ioctl(f.Fd(), ioctlEvBitKey, uintptr(unsafe.Pointer(&keys[0]))) == nil {
			if keys[keyA/8]&(1<<(keyA%8)) != 0 {
				d.kbd = true
			}
		}
		// Button-only devices (mice without wheel bits probed above still
		// count via REL; pure button nodes without REL are ignored).
	}
	if !d.mouse && !d.kbd {
		f.Close()
		return nil
	}
	return d
}

// evdevBackend: passive evdev capture + uinput injection (restricted mode).
//
// Capture is passive (no grab) while local: the compositor keeps receiving
// everything and we only observe. SetForwarding(true) grabs the nodes so
// local delivery stops while switched away (grab parity with X11 re-inject
// suppression). ABS-only touchpads are skipped (documented gap).
type evdevBackend struct {
	mu         sync.Mutex
	devs       []*evdevDev
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
	forwarding atomic.Bool

	uinMu sync.Mutex
	uin   *os.File // lazy uinput handle (nil until first Inject)

	posMu  sync.Mutex
	px, py int
	hasPos bool
}

func NewEvdevBackend() (Backend, error) {
	devs := openUsableDevs()
	if len(devs) == 0 {
		return nil, fmt.Errorf("input: no readable evdev nodes (need input group or root; see scripts/check-input-perms.sh)")
	}
	return &evdevBackend{devs: devs}, nil
}

// openUsableDevs probes every event node, keeping readable mice/keyboards.
func openUsableDevs() []*evdevDev {
	matches, _ := filepath.Glob("/dev/input/event*")
	var devs []*evdevDev
	for _, m := range matches {
		if d := probeDev(m); d != nil {
			devs = append(devs, d)
		}
	}
	return devs
}

func (b *evdevBackend) Name() string { return string(BackendEvdev) }

// SetForwarding grabs (away: swallow local) or releases the nodes.
func (b *evdevBackend) SetForwarding(f bool) {
	b.forwarding.Store(f)
	b.mu.Lock()
	defer b.mu.Unlock()
	one := uintptr(1)
	if !f {
		one = uintptr(0)
	}
	for _, d := range b.devs {
		_ = ioctl(d.f.Fd(), ioctlEvGrab, one)
	}
}

func (b *evdevBackend) Bounds() util.Rect {
	if r, ok := drmBounds(); ok {
		return r
	}
	return util.Rect{Right: 1920, Bottom: 1080}
}

// drmBounds unions connected DRM connectors side-by-side (layout unknown:
// documented approximation for restricted mode; portal/X11 are exact).
func drmBounds() (util.Rect, bool) {
	cards, _ := filepath.Glob("/sys/class/drm/card*-*/modes")
	var r util.Rect
	found := false
	x := 0
	for _, m := range cards {
		dir := filepath.Dir(m)
		st, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil || string(st)[:9] != "connected" {
			continue
		}
		raw, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		w, h, ok := parseDrmMode(string(raw))
		if !ok {
			continue
		}
		if !found {
			r = util.Rect{Left: x, Top: 0, Right: x + w, Bottom: h}
			found = true
		} else {
			if x+w > r.Right {
				r.Right = x + w
			}
			if h > r.Bottom {
				r.Bottom = h
			}
		}
		x += w
	}
	return r, found
}

// parseDrmMode parses the first "WxH" line of a modes sysfs file.
func parseDrmMode(s string) (w, h int, ok bool) {
	line := s
	for i, c := range s {
		if c == '\n' {
			line = s[:i]
			break
		}
	}
	var ww, hh int
	n, _ := fmt.Sscanf(line, "%dx%d", &ww, &hh)
	if n != 2 || ww <= 0 || hh <= 0 {
		return 0, 0, false
	}
	return ww, hh, true
}

func (b *evdevBackend) Inject(e Event) error {
	u, err := b.uinput()
	if err != nil {
		return err
	}
	switch e.Kind {
	case KindMouseMove:
		dx, dy := e.X, e.Y
		if !e.Rel {
			dx, dy = b.toDelta(e.X, e.Y)
		}
		return uemit(u, []inputEvent{
			{Type: evRel, Code: relX, Value: int32(dx)},
			{Type: evRel, Code: relY, Value: int32(dy)},
		})
	case KindMouseButton:
		btn, down, ok := mouseFlagToButton(e.MouseFlag)
		if !ok {
			return fmt.Errorf("input: evdev unknown mouse flag %#x", e.MouseFlag)
		}
		return uemit(u, []inputEvent{{Type: evKey, Code: uint16(btn), Value: boolTo32(down)}})
	case KindMouseWheel:
		steps := (e.Wheel + 119) / 120
		if e.Wheel < 0 {
			steps = -((-e.Wheel + 119) / 120)
		}
		var evs []inputEvent
		count := steps
		if count < 0 {
			count = -count
		}
		v := int32(1)
		if steps < 0 {
			v = -1
		}
		for i := 0; i < count; i++ {
			evs = append(evs, inputEvent{Type: evRel, Code: relWheel, Value: v})
		}
		return uemit(u, evs)
	case KindKey:
		ev, ok := keymap.WinVKToEvdev(e.VK)
		if !ok {
			return fmt.Errorf("input: evdev VK %#x unmapped", e.VK)
		}
		return uemit(u, []inputEvent{{Type: evKey, Code: uint16(ev), Value: boolTo32(e.KeyDown)}})
	}
	return fmt.Errorf("input: evdev unknown event kind %d", e.Kind)
}

func boolTo32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// toDelta converts an absolute pixel target to a relative step from the
// tracked position (capture + inject both advance the tracker).
func (b *evdevBackend) toDelta(x, y int) (dx, dy int) {
	b.posMu.Lock()
	defer b.posMu.Unlock()
	if b.hasPos {
		dx, dy = x-b.px, y-b.py
	}
	b.px, b.py, b.hasPos = x, y, true
	return dx, dy
}

func (b *evdevBackend) trackRel(dx, dy int) {
	b.posMu.Lock()
	b.px += dx
	b.py += dy
	b.hasPos = true
	b.posMu.Unlock()
}

// uinput lazily creates the virtual keyboard+mouse.
func (b *evdevBackend) uinput() (*os.File, error) {
	b.uinMu.Lock()
	defer b.uinMu.Unlock()
	if b.uin != nil {
		return b.uin, nil
	}
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("input: uinput unavailable (%v; need input group)", err)
	}
	setbit := func(req uint, bit int) error {
		return ioctl(f.Fd(), req, uintptr(bit))
	}
	for _, e := range []int{evKey, evRel, evSyn} {
		if err := setbit(ioctlUISetEvbit, e); err != nil {
			f.Close()
			return nil, err
		}
	}
	for code := 1; code <= 248; code++ {
		_ = setbit(ioctlUISetKeybit, code) // best-effort across layouts
	}
	for _, btn := range []int{btnLeft, btnRight, btnMiddle} {
		if err := setbit(ioctlUISetKeybit, btn); err != nil {
			f.Close()
			return nil, err
		}
	}
	for _, rel := range []int{relX, relY, relWheel} {
		if err := setbit(ioctlUISetRelbit, rel); err != nil {
			f.Close()
			return nil, err
		}
	}
	if err := uinputSetup(f, "mwb-client"); err != nil {
		f.Close()
		return nil, err
	}
	if err := ioctl(f.Fd(), ioctlUIDevCreate, 0); err != nil {
		f.Close()
		return nil, err
	}
	time.Sleep(100 * time.Millisecond) // let the compositor add the node
	b.uin = f
	return f, nil
}

// uinputSetup writes struct uinput_setup (id[8] + name[80] + ffmax[4]).
func uinputSetup(f *os.File, name string) error {
	buf := make([]byte, 92)
	binary.LittleEndian.PutUint16(buf[0:2], 0x03) // BUS_USB
	binary.LittleEndian.PutUint16(buf[2:4], 0x1234)
	binary.LittleEndian.PutUint16(buf[4:6], 0x5678)
	binary.LittleEndian.PutUint16(buf[6:8], 0x0001)
	copy(buf[8:88], name)
	var ptr unsafe.Pointer
	if len(buf) > 0 {
		ptr = unsafe.Pointer(&buf[0])
	}
	return ioctl(f.Fd(), ioctlUIDevSetup, uintptr(ptr))
}

// uemit writes events + trailing SYN_REPORT.
func uemit(f *os.File, evs []inputEvent) error {
	for _, e := range evs {
		if _, err := f.Write(encodeInputEvent(e)); err != nil {
			return err
		}
	}
	_, err := f.Write(encodeInputEvent(inputEvent{Type: evSyn, Code: synReport}))
	return err
}

func (b *evdevBackend) HideCursor() error {
	return fmt.Errorf("input: cursor hide unsupported on Wayland compositor (restricted mode; see docs)")
}

func (b *evdevBackend) ShowCursor() error { return nil }

// StartCapture reads all nodes; callback must be non-blocking.
func (b *evdevBackend) StartCapture(cb func(Event)) error {
	b.mu.Lock()
	if b.stopCh != nil {
		b.mu.Unlock()
		return fmt.Errorf("input: evdev capture already started")
	}
	b.stopCh = make(chan struct{})
	b.stopOnce = sync.Once{}
	if os.Getenv("MWB_DEBUG_INPUT") != "" {
		orig := cb
		var n atomic.Int32
		cb = func(e Event) {
			if n.Add(1) <= 30 {
				log.Printf("input-event kind=%d x=%d y=%d rel=%v vk=%d down=%v flag=%#x wheel=%d",
					e.Kind, e.X, e.Y, e.Rel, e.VK, e.KeyDown, e.MouseFlag, e.Wheel)
			}
			orig(e)
		}
	}
	if len(b.devs) == 0 {
		// Reopen after a stop (StopCapture closes fds to unblock readers).
		b.devs = openUsableDevs()
		if len(b.devs) == 0 {
			b.stopCh = nil
			b.mu.Unlock()
			return fmt.Errorf("input: no readable evdev nodes (need input group or root)")
		}
	}
	for _, d := range b.devs {
		b.wg.Add(1)
		go b.readLoop(d, cb)
	}
	b.mu.Unlock()
	return nil
}

func (b *evdevBackend) StopCapture() error {
	b.mu.Lock()
	ch := b.stopCh
	devs := b.devs
	b.mu.Unlock()
	if ch == nil {
		return nil
	}
	b.stopOnce.Do(func() { close(ch) })
	// Closing the fds unblocks the readers (they exit on stopCh); the
	// grab is released implicitly and re-applied on next StartCapture.
	for _, d := range devs {
		_ = ioctl(d.f.Fd(), ioctlEvGrab, uintptr(0))
		d.f.Close()
	}
	b.wg.Wait()
	b.mu.Lock()
	b.stopCh = nil
	b.stopOnce = sync.Once{}
	b.devs = nil
	b.mu.Unlock()
	b.forwarding.Store(false)
	return nil
}

// readLoop parses one node, coalescing REL motion per SYN frame.
func (b *evdevBackend) readLoop(d *evdevDev, cb func(Event)) {
	defer b.wg.Done()
	var acc relAccum
	buf := make([]byte, 24)
	for {
		select {
		case <-b.stopCh:
			return
		default:
		}
		n, err := d.f.Read(buf)
		if err != nil || n != 24 {
			select {
			case <-b.stopCh:
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		e, ok := decodeInputEvent(buf)
		if !ok {
			continue
		}
		b.handleKernelEvent(d, e, &acc, cb)
	}
}

// handleKernelEvent maps one kernel event; pure except cb/track.
func (b *evdevBackend) handleKernelEvent(d *evdevDev, e inputEvent, acc *relAccum, cb func(Event)) {
	switch e.Type {
	case evRel:
		switch e.Code {
		case relX:
			acc.addMotion(e.Value, 0)
		case relY:
			acc.addMotion(0, e.Value)
		case relWheel:
			acc.addWheel(e.Value * 120)
		case relWheelHR:
			acc.addWheel(e.Value) // already 120ths
		case relHWheel, relHWheelHR:
			// Horizontal wheel: M2c scope skips it.
		}
	case evKey:
		if e.Code >= btnLeft && e.Code <= btnMiddle+16 {
			if flag, ok := btnToMouseFlag(e.Code, e.Value != 0); ok {
				if e.Value == 0 || e.Value == 1 {
					cb(Event{Kind: KindMouseButton, MouseFlag: flag})
				}
				return
			}
		}
		if d.kbd && (e.Value == 0 || e.Value == 1) {
			if vk, ok := keymap.EvdevToWinVK(int(e.Code)); ok {
				cb(Event{Kind: KindKey, VK: vk, KeyDown: e.Value == 1})
			}
		}
	case evSyn:
		if e.Code == synReport {
			if dx, dy, w, has := acc.flush(); has {
				if dx != 0 || dy != 0 {
					b.trackRel(int(dx), int(dy))
					cb(Event{Kind: KindMouseMove, X: int(dx), Y: int(dy), Rel: true})
				}
				if w != 0 {
					cb(Event{Kind: KindMouseWheel, Wheel: int(w)})
				}
			}
		}
	}
}

func (b *evdevBackend) Close() error {
	_ = b.StopCapture()
	b.uinMu.Lock()
	if b.uin != nil {
		_ = ioctl(b.uin.Fd(), ioctlUIDevDestroy, 0)
		b.uin.Close()
		b.uin = nil
	}
	b.uinMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range b.devs {
		d.f.Close()
	}
	b.devs = nil
	return nil
}
