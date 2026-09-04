//go:build !linux

package input

import (
	"errors"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

// Non-linux (CI/Windows dev): no-op backends so go vet/test pass.

type noopBackend struct{ name BackendKind }

func newNoop(k BackendKind) Backend { return &noopBackend{name: k} }

func NewX11Backend() (Backend, error)    { return newNoop(BackendX11), nil }
func NewPortalBackend() (Backend, error) { return newNoop(BackendPortal), nil }
func NewEvdevBackend() (Backend, error)  { return newNoop(BackendEvdev), nil }

func (b *noopBackend) Name() string { return string(b.name) }
func (b *noopBackend) StartCapture(cb func(Event)) error {
	return errors.New("input: capture unavailable on this OS")
}
func (b *noopBackend) StopCapture() error { return nil }
func (b *noopBackend) Inject(e Event) error {
	return errors.New("input: inject unavailable on this OS")
}
func (b *noopBackend) HideCursor() error { return nil }
func (b *noopBackend) ShowCursor() error { return nil }
func (b *noopBackend) Bounds() util.Rect { return util.Rect{Right: 1920, Bottom: 1080} }
func (b *noopBackend) Close() error      { return nil }
