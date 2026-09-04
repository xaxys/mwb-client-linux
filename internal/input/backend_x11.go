//go:build linux

package input

import (
	"errors"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

// x11Backend: XRecord/XInput2 capture + XTest inject + XFixes cursor.
// Full implementation lands in M2a (needs libX11/libXtst/libXi/libXfixes via
// cgo or xgb). This stub keeps the linux build graph honest.
type x11Backend struct{}

func NewX11Backend() (Backend, error) { return &x11Backend{}, nil }

func (b *x11Backend) Name() string { return string(BackendX11) }
func (b *x11Backend) StartCapture(cb func(Event)) error {
	return errors.New("input: x11 capture not yet implemented (M2a)")
}
func (b *x11Backend) StopCapture() error { return nil }
func (b *x11Backend) Inject(e Event) error {
	return errors.New("input: x11 inject not yet implemented (M2a)")
}
func (b *x11Backend) HideCursor() error {
	return errors.New("input: x11 hide not yet implemented (M2a)")
}
func (b *x11Backend) ShowCursor() error {
	return errors.New("input: x11 show not yet implemented (M2a)")
}
func (b *x11Backend) Bounds() util.Rect { return util.Rect{} }
func (b *x11Backend) Close() error      { return nil }
