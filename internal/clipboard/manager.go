// Clipboard manager: local store, beat fan-out, pull/serve glue.
//
// The manager is transport-agnostic: beat/ask packets go out through the
// injected beat/ask funcs (wired to net.Client by the daemon), and the
// secondary transfer runs over caller-supplied SecureConns. System clipboard
// integration (x11/clipboard backends) plugs in later; until then text and
// images live in memory and files stage under the receive dir.
package clipboard

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

// beatGate suppresses beat storms from clipboard echo (1s parity).
const beatGate = time.Second

// Manager holds the local clipboard state.
type Manager struct {
	mu          sync.Mutex
	log         *util.Logger
	self        string
	dir         string
	beat        func()
	ask         func(dest uint32)
	lastBeat    time.Time
	text        string
	hasText     bool
	image       []byte
	imageFormat string
	filePath    string
	lastMachine string
	lastID      uint32
}

// NewManager creates a manager. dir stages received files ("" → default),
// beat/ask may be nil (no network yet: state still tracks locally).
func NewManager(self, dir string, log *util.Logger, beat func(), ask func(dest uint32)) *Manager {
	if dir == "" {
		dir = DefaultReceiveDir()
	}
	return &Manager{self: self, dir: dir, log: log, beat: beat, ask: ask}
}

// SetText stores deflated text and announces it.
func (m *Manager) SetText(s string) error {
	comp, err := CompressText(s)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.text, m.hasText = s, true
	m.image, m.imageFormat, m.filePath = nil, "", ""
	m.mu.Unlock()
	_ = comp
	m.emitBeat()
	return nil
}

// SetImage stores raw image bytes (format sniffed, unknown kept anyway)
// and announces them.
func (m *Manager) SetImage(data []byte) {
	m.mu.Lock()
	m.image = append([]byte{}, data...)
	m.imageFormat = ValidateImage(data)
	m.text, m.hasText, m.filePath = "", false, ""
	m.mu.Unlock()
	m.emitBeat()
}

// SetFile stores a file path (size-capped) and announces it.
func (m *Manager) SetFile(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() || st.Size() > MaxTransferBytes {
		return fmt.Errorf("clipboard: file %q not transferable (dir or >%dB)", path, MaxTransferBytes)
	}
	m.mu.Lock()
	m.filePath = path
	m.text, m.hasText, m.image, m.imageFormat = "", false, nil, ""
	m.mu.Unlock()
	m.emitBeat()
	return nil
}

func (m *Manager) emitBeat() {
	m.mu.Lock()
	since := time.Since(m.lastBeat)
	if since < beatGate {
		m.mu.Unlock()
		return
	}
	m.lastBeat = time.Now()
	beat := m.beat
	m.mu.Unlock()
	if beat != nil {
		beat()
	}
}

// OnBeat records a peer beat (ignored when it is our own echo).
func (m *Manager) OnBeat(machine string, id uint32) {
	if machine == "" || machine == m.self {
		return
	}
	m.mu.Lock()
	m.lastMachine, m.lastID = machine, id
	m.mu.Unlock()
}

// LastMachine reports the most recent beat source.
func (m *Manager) LastMachine() (string, uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastMachine, m.lastID
}

// Text returns the stored text, if any.
func (m *Manager) Text() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.text, m.hasText
}

// Image returns the stored image bytes and sniffed format.
func (m *Manager) Image() ([]byte, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte{}, m.image...), m.imageFormat
}

// Provide serves the holder side: text as ("text", deflated), image as
// ("image", raw), file as (base name, raw bytes).
func (m *Manager) Provide() (TransferKind, string, []byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.filePath != "":
		data, err := os.ReadFile(m.filePath)
		// Empty files ride as "0*" error headers (Windows never sends a
		// zero-length real file either), so they report as no-data here.
		if err != nil || len(data) == 0 || int64(len(data)) > MaxTransferBytes {
			return KindError, "", nil, false
		}
		return KindFile, filepath.Base(m.filePath), data, true
	case len(m.image) > 0:
		return KindImage, "image", append([]byte{}, m.image...), true
	case m.hasText:
		comp, err := CompressText(m.text)
		if err != nil {
			return KindError, "", nil, false
		}
		return KindText, "text", comp, true
	}
	return KindError, "", nil, false
}

// Sink stores pushed payloads: text inflated, images kept raw, files are
// already on disk (path recorded).
func (m *Manager) Sink(r *Received) error {
	if r == nil {
		return fmt.Errorf("clipboard: nil received")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch r.Kind {
	case KindText:
		m.text, m.hasText = r.Text, true
		m.image, m.imageFormat, m.filePath = nil, "", ""
	case KindImage:
		m.image = append([]byte{}, r.Data...)
		m.imageFormat = ValidateImage(r.Data)
		m.text, m.hasText, m.filePath = "", false, ""
	case KindFile:
		m.filePath = r.Path
		m.text, m.hasText, m.image, m.imageFormat = "", false, nil, ""
	case KindError:
		if m.log != nil {
			m.log.Warnf("clipboard: peer reported %q", r.Text)
		}
	}
	m.lastMachine, m.lastID = r.Name, 0
	return nil
}
