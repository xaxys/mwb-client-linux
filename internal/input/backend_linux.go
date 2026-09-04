//go:build linux

package input

import (
	"errors"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

// portalBackend: InputCapture capture + RemoteDesktop/libei inject (GNOME46+,
// 24.04 Wayland). M2b.
type portalBackend struct{}

func NewPortalBackend() (Backend, error) { return &portalBackend{}, nil }

func (b *portalBackend) Name() string { return string(BackendPortal) }
func (b *portalBackend) StartCapture(cb func(Event)) error {
	return errors.New("input: portal capture not yet implemented (M2b)")
}
func (b *portalBackend) StopCapture() error { return nil }
func (b *portalBackend) Inject(e Event) error {
	return errors.New("input: portal inject not yet implemented (M2b)")
}
func (b *portalBackend) HideCursor() error {
	return errors.New("input: portal hide not yet implemented (M2b)")
}
func (b *portalBackend) ShowCursor() error {
	return errors.New("input: portal show not yet implemented (M2b)")
}
func (b *portalBackend) Bounds() util.Rect { return util.Rect{} }
func (b *portalBackend) Close() error      { return nil }

// evdevBackend: evdev capture + uinput inject fallback (22.04 Wayland
// restricted mode; needs input group / udev uaccess). M2c.
type evdevBackend struct{}

func NewEvdevBackend() (Backend, error) { return &evdevBackend{}, nil }

func (b *evdevBackend) Name() string { return string(BackendEvdev) }
func (b *evdevBackend) StartCapture(cb func(Event)) error {
	return errors.New("input: evdev capture not yet implemented (M2c)")
}
func (b *evdevBackend) StopCapture() error { return nil }
func (b *evdevBackend) Inject(e Event) error {
	return errors.New("input: evdev inject not yet implemented (M2c)")
}
func (b *evdevBackend) HideCursor() error {
	return errors.New("input: evdev hide not yet implemented (M2c)")
}
func (b *evdevBackend) ShowCursor() error {
	return errors.New("input: evdev show not yet implemented (M2c)")
}
func (b *evdevBackend) Bounds() util.Rect { return util.Rect{} }
func (b *evdevBackend) Close() error      { return nil }
