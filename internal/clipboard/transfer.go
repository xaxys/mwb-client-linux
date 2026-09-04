// Secondary-channel clipboard transfer (TCP 15100).
//
// Wire parity with PowerToys Clipboard.cs / SocketStuff.cs:
//
//  1. Stream handshake (32B header on current gen + 16B noise each way;
//     crypto.SecureConn already does this).
//  2. Each side sends one 64B header packet: ClipboardPush (79) means
//     "I am pushing data", Clipboard (69) means "I am pulling". The pusher
//     then sends the payload.
//  3. Payload = 1024B header (UTF-16LE "<decimalSize>*<name>", zero padded)
//     followed by exactly dataSize raw bytes; the sender closes afterwards.
//     Names "text"/"image" (prefix match) go to memory+clipboard, anything
//     else is a file streamed to disk. "0*<message>" reports an error
//     (missing file, too big, folder) without payload.
package clipboard

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// PostAction mirrors ClipboardPostAction (header payload, Mspaint/Desktop).
type PostAction int32

const (
	PostOther   PostAction = 0
	PostDesktop PostAction = 1
	PostMspaint PostAction = 2
)

// TransferKind classifies one secondary transfer.
type TransferKind int

const (
	KindText TransferKind = iota
	KindImage
	KindFile
	KindError
)

// Secondary-channel framing.
const (
	SecondaryHeaderLen = 1024
	// MaxTransferBytes caps one transfer (100MB file parity).
	MaxTransferBytes = 100 << 20
	// HandshakeTimeout bounds the header exchange (30s parity).
	HandshakeTimeout = 30 * time.Second
)

// Header is one decoded 69/79 packet.
type Header struct {
	Push       bool // true = ClipboardPush (pusher), false = Clipboard (puller)
	Name       string
	Src        uint32
	PostAction PostAction
}

// ReadHeaderPacket reads and validates one 69/79 header packet.
func ReadHeaderPacket(sc *mwbcrypto.SecureConn, magic uint32) (Header, error) {
	var h Header
	raw, err := sc.ReadPacket(true)
	if err != nil {
		return h, err
	}
	p, err := protocol.Decode(raw, magic)
	if err != nil {
		return h, err
	}
	switch p.Type {
	case protocol.PtClipboardPush:
		h.Push = true
	case protocol.PtClipboard:
		h.Push = false
	default:
		return h, fmt.Errorf("clipboard: unexpected header type %d", byte(p.Type))
	}
	h.Name = p.MachineName
	h.Src = p.Src
	h.PostAction = PostAction(p.GetPostAction())
	if h.Name == "" {
		return h, fmt.Errorf("clipboard: empty machine name in header")
	}
	return h, nil
}

// WriteHeaderPacket sends our 69/79 header packet.
func WriteHeaderPacket(sc *mwbcrypto.SecureConn, magic uint32, h Header, id int32) error {
	t := protocol.PtClipboard
	if h.Push {
		t = protocol.PtClipboardPush
	}
	p := &protocol.Packet{Type: t, ID: id, Src: h.Src, Des: protocol.IDAll,
		HasName: true, MachineName: h.Name}
	p.SetPostAction(int32(h.PostAction))
	wire, err := p.Encode(magic)
	if err != nil {
		return err
	}
	return sc.WritePacket(wire)
}

// BuildSecondaryHeader renders the 1024B "<size>*<name>" UTF-16LE header.
func BuildSecondaryHeader(dataSize int64, name string) ([]byte, error) {
	raw := encodeUTF16LE(strconv.FormatInt(dataSize, 10) + "*" + name)
	if len(raw) > SecondaryHeaderLen {
		return nil, fmt.Errorf("clipboard: header name too long: %q", name)
	}
	out := make([]byte, SecondaryHeaderLen)
	copy(out, raw)
	return out, nil
}

// ParseSecondaryHeader decodes a 1024B header (split on the FIRST '*': file
// names may legally contain '*', which stock PowerToys mishandles).
func ParseSecondaryHeader(h []byte) (int64, string, error) {
	if len(h) != SecondaryHeaderLen {
		return 0, "", fmt.Errorf("clipboard: header must be %dB", SecondaryHeaderLen)
	}
	s, err := decodeUTF16LE(h)
	if err != nil {
		return 0, "", err
	}
	s = strings.TrimRight(s, "\x00")
	star := strings.IndexByte(s, '*')
	if star < 0 {
		return 0, "", fmt.Errorf("clipboard: malformed header %q", s)
	}
	size, err := strconv.ParseInt(s[:star], 10, 64)
	if err != nil || size < 0 || size > MaxTransferBytes {
		return 0, "", fmt.Errorf("clipboard: bad size in header %q", s)
	}
	return size, s[star+1:], nil
}

// Received is one completed secondary transfer.
type Received struct {
	Kind TransferKind
	Name string // header name ("text"/"image"/file name)
	Text string // KindText (inflated) or KindError (message)
	Data []byte // KindImage raw bytes (nil for files: see Path)
	Path string // KindFile written file path
}

// Provider supplies holder-side payloads: name is "text"/"image" or a file
// name; data is deflated-UTF16LE text, raw image bytes, or raw file bytes.
type Provider func() (kind TransferKind, name string, data []byte, ok bool)

// Sink takes pushed (peer-initiated) payloads.
type Sink func(r *Received) error

// SendTransfer writes one payload: 1024B header + raw bytes. Every cipher
// write is 16B-aligned: only the final tail is zero-padded, so the receiver
// can split reads purely from the announced size.
func SendTransfer(sc *mwbcrypto.SecureConn, name string, data []byte) error {
	if int64(len(data)) > MaxTransferBytes {
		return fmt.Errorf("clipboard: payload %dB exceeds %dB cap", len(data), MaxTransferBytes)
	}
	hdr, err := BuildSecondaryHeader(int64(len(data)), name)
	if err != nil {
		return err
	}
	if err := sc.WriteRaw(hdr); err != nil {
		return err
	}
	const stride = 1 << 20 // 16-aligned
	for off := 0; off < len(data); {
		end := off + stride
		if end > len(data) {
			end = len(data)
		}
		chunk := data[off:end]
		if end == len(data) {
			if rem := len(chunk) % 16; rem != 0 {
				padded := make([]byte, len(chunk)+(16-rem))
				copy(padded, chunk)
				chunk = padded
			}
		}
		if err := sc.WriteRaw(chunk); err != nil {
			return err
		}
		off = end
	}
	return nil
}

// ReceiveTransfer reads one payload; files stream to dir (temp + rename),
// text/image land in memory. It reads exactly dataSize bytes; the sender
// closes afterwards.
func ReceiveTransfer(sc *mwbcrypto.SecureConn, dir string, postAction PostAction, timeout time.Duration) (*Received, error) {
	if err := sc.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	defer sc.SetReadDeadline(time.Time{})
	hdr := make([]byte, SecondaryHeaderLen) // 1024 is 16-aligned
	if err := sc.ReadRaw(hdr); err != nil {
		return nil, err
	}
	size, name, err := ParseSecondaryHeader(hdr)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(name)
	switch {
	case lower == "text" || strings.HasPrefix(lower, "text"):
		return receiveMemory(sc, timeout, KindText, name, size)
	case lower == "image" || strings.HasPrefix(lower, "image"):
		return receiveMemory(sc, timeout, KindImage, name, size)
	case size == 0:
		// "0*<message>": holder-side error report, no payload.
		return &Received{Kind: KindError, Name: name, Text: name}, nil
	default:
		return receiveFile(sc, timeout, dir, postAction, name, size)
	}
}

// receiveMemory reads a bounded payload into memory.
func receiveMemory(sc *mwbcrypto.SecureConn, timeout time.Duration, kind TransferKind, name string, size int64) (*Received, error) {
	if err := sc.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	defer sc.SetReadDeadline(time.Time{})
	data, err := readExact(sc, size)
	if err != nil {
		return nil, err
	}
	r := &Received{Kind: kind, Name: name}
	switch kind {
	case KindText:
		text, err := DecompressText(data)
		if err != nil {
			return nil, fmt.Errorf("clipboard: text inflate: %w", err)
		}
		r.Text = text
	case KindImage:
		r.Data = data
	}
	return r, nil
}

// receiveFile streams a payload to dir with temp-file + rename commit.
func receiveFile(sc *mwbcrypto.SecureConn, timeout time.Duration, dir string, postAction PostAction, name string, size int64) (*Received, error) {
	targetDir := dir
	if postAction == PostDesktop {
		if d, err := desktopReceiveDir(); err == nil {
			targetDir = d
		}
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	if err := sc.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	defer sc.SetReadDeadline(time.Time{})
	tmp, err := os.CreateTemp(targetDir, ".mwb-part-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		tmp.Close()
		if !done {
			os.Remove(tmpName)
		}
	}()
	var received int64
	if err := readStream(sc, size, tmp, &received); err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	final := filepath.Join(targetDir, filepath.Base(name))
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return nil, err
	}
	done = true
	return &Received{Kind: KindFile, Name: name, Path: final}, nil
}

// readExact reads exactly n decrypted bytes. Cipher reads mirror the
// sender: 16B-aligned pieces with only the final tail zero-padded, so no
// read ever crosses into bytes beyond the announced size.
func readExact(sc *mwbcrypto.SecureConn, n int64) ([]byte, error) {
	out := make([]byte, 0, n)
	const stride = 1 << 20
	remaining := n
	for remaining > 0 {
		take := remaining
		if take > stride {
			take = stride
		}
		if aligned := take - take%16; aligned > 0 {
			raw := make([]byte, aligned)
			if err := sc.ReadRaw(raw); err != nil {
				return nil, err
			}
			out = append(out, raw...)
			remaining -= aligned
			continue
		}
		// Final partial tail (remaining < 16): one padded block.
		raw := make([]byte, 16)
		if err := sc.ReadRaw(raw); err != nil {
			return nil, err
		}
		out = append(out, raw[:remaining]...)
		remaining = 0
	}
	return out, nil
}

// Serve runs the holder side of one secondary transfer on an accepted
// stream (stream handshake done, peer header not yet read): reads the peer
// 69/79 header, answers with our Push header, then sends when the peer
// pulls (69) or receives into sink when the peer pushes (79).
func Serve(sc *mwbcrypto.SecureConn, magic uint32, selfName string, selfID uint32, postAction PostAction, nextID int32, provide Provider, sink Sink, dir string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	_ = sc.SetReadDeadline(deadline)
	_ = sc.SetWriteDeadline(deadline)
	defer sc.SetReadDeadline(time.Time{})
	defer sc.SetWriteDeadline(time.Time{})
	peer, err := ReadHeaderPacket(sc, magic)
	if err != nil {
		return err
	}
	ours := Header{Push: true, Name: selfName, Src: selfID, PostAction: postAction}
	if err := WriteHeaderPacket(sc, magic, ours, nextID); err != nil {
		return err
	}
	if peer.Push {
		r, err := ReceiveTransfer(sc, dir, peer.PostAction, timeout)
		if err != nil {
			return err
		}
		if sink == nil {
			return fmt.Errorf("clipboard: pushed data has no sink")
		}
		return sink(r)
	}
	kind, name, data, ok := TransferKind(0), "", []byte(nil), false
	if provide != nil {
		kind, name, data, ok = provide()
	}
	_ = kind
	if !ok {
		return SendTransfer(sc, "No data available", nil)
	}
	return SendTransfer(sc, name, data)
}

// Pull runs the puller side on a dialed stream: sends our 69 header,
// expects the holder's Push header, then receives one transfer.
func Pull(sc *mwbcrypto.SecureConn, magic uint32, selfName string, selfID uint32, postAction PostAction, nextID int32, dir string, timeout time.Duration) (*Received, error) {
	deadline := time.Now().Add(timeout)
	_ = sc.SetReadDeadline(deadline)
	_ = sc.SetWriteDeadline(deadline)
	defer sc.SetReadDeadline(time.Time{})
	defer sc.SetWriteDeadline(time.Time{})
	ours := Header{Push: false, Name: selfName, Src: selfID, PostAction: postAction}
	if err := WriteHeaderPacket(sc, magic, ours, nextID); err != nil {
		return nil, err
	}
	peer, err := ReadHeaderPacket(sc, magic)
	if err != nil {
		return nil, err
	}
	if !peer.Push {
		return nil, fmt.Errorf("clipboard: holder answered %q without push", peer.Name)
	}
	return ReceiveTransfer(sc, dir, postAction, timeout)
}

// ValidateImage reports the image format (png/jpeg/gif/...) or "" when the
// bytes are not a recognized image. Unknown bytes are still storable —
// Windows decoders are more lenient than Go's stdlib.
func ValidateImage(data []byte) string {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return ""
	}
	return format
}

// desktopReceiveDir is ~/Desktop/MouseWithoutBorders (Windows parity).
func desktopReceiveDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, "Desktop")
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		return "", fmt.Errorf("no desktop dir")
	}
	return filepath.Join(d, "MouseWithoutBorders"), nil
}

// DefaultReceiveDir falls back to the XDG data dir when no Desktop exists.
func DefaultReceiveDir() string {
	if d, err := desktopReceiveDir(); err == nil {
		return d
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "mwb-client", "received")
}

// readStream siblings readExact for files: same alignment discipline,
// streaming into w instead of memory.
func readStream(sc *mwbcrypto.SecureConn, n int64, w io.Writer, received *int64) error {
	const stride = 1 << 20
	remaining := n
	for remaining > 0 {
		take := remaining
		if take > stride {
			take = stride
		}
		if aligned := take - take%16; aligned > 0 {
			raw := make([]byte, aligned)
			if err := sc.ReadRaw(raw); err != nil {
				return err
			}
			if _, err := w.Write(raw); err != nil {
				return err
			}
			*received += aligned
			remaining -= aligned
			continue
		}
		raw := make([]byte, 16)
		if err := sc.ReadRaw(raw); err != nil {
			return err
		}
		if _, err := w.Write(raw[:remaining]); err != nil {
			return err
		}
		*received += remaining
		remaining = 0
	}
	return nil
}
