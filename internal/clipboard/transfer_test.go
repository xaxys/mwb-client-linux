package clipboard_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xaxys/mwb-client-linux/internal/clipboard"
	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
)

const clipKey = "clip-transfer-test"

func clipPair(t *testing.T) (*mwbcrypto.SecureConn, *mwbcrypto.SecureConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type hr struct {
		sc  *mwbcrypto.SecureConn
		err error
	}
	accepted := make(chan hr, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- hr{nil, err}
			return
		}
		sc, err := mwbcrypto.HandshakeCurrent(c, clipKey)
		accepted <- hr{sc, err}
	}()
	dialed, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	sca, err := mwbcrypto.HandshakeCurrent(dialed, clipKey)
	if err != nil {
		t.Fatalf("dial setup: %v", err)
	}
	select {
	case r := <-accepted:
		if r.err != nil {
			t.Fatalf("accept setup: %v", r.err)
		}
		return sca, r.sc
	case <-time.After(10 * time.Second):
		t.Fatal("setup timeout")
	}
	return nil, nil
}

func clipMagic() uint32 { return mwbcrypto.Magic24(clipKey) }

func TestSecondaryHeader(t *testing.T) {
	h, err := clipboard.BuildSecondaryHeader(12345, "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != clipboard.SecondaryHeaderLen {
		t.Fatalf("len=%d", len(h))
	}
	size, name, err := clipboard.ParseSecondaryHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if size != 12345 || name != "notes.txt" {
		t.Fatalf("%d %q", size, name)
	}
	// '*' inside file names survives (split on first star only).
	h, _ = clipboard.BuildSecondaryHeader(7, "a*b.txt")
	if _, name, err = clipboard.ParseSecondaryHeader(h); err != nil || name != "a*b.txt" {
		t.Fatalf("star name: %q %v", name, err)
	}
	// Error report shape.
	h, _ = clipboard.BuildSecondaryHeader(0, "gone.txt not found!")
	if size, _, err = clipboard.ParseSecondaryHeader(h); err != nil || size != 0 {
		t.Fatalf("zero header: %d %v", size, err)
	}
	if _, _, err = clipboard.ParseSecondaryHeader(make([]byte, 100)); err == nil {
		t.Fatal("short header accepted")
	}
	if _, err = clipboard.BuildSecondaryHeader(1, string(make([]byte, 2000))); err == nil {
		t.Fatal("oversize name accepted")
	}
}

// pullAgainst runs Serve in the background and Pulls one transfer.
func pullAgainst(t *testing.T, provide clipboard.Provider, postAction clipboard.PostAction, dir string) *clipboard.Received {
	t.Helper()
	sca, scb := clipPair(t)
	defer sca.Close()
	defer scb.Close()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- clipboard.Serve(scb, clipMagic(), "HOLDER", 2, clipboard.PostOther, 41, provide, nil, dir, 10*time.Second)
	}()
	r, err := clipboard.Pull(sca, clipMagic(), "PULLER", 1, postAction, 42, dir, 10*time.Second)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("serve: %v", err)
	}
	return r
}

func TestPullText(t *testing.T) {
	comp, err := clipboard.CompressText("hello secondary — 你好")
	if err != nil {
		t.Fatal(err)
	}
	r := pullAgainst(t, func() (clipboard.TransferKind, string, []byte, bool) {
		return clipboard.KindText, "text", comp, true
	}, clipboard.PostOther, t.TempDir())
	if r.Kind != clipboard.KindText || r.Text != "hello secondary — 你好" {
		t.Fatalf("%+v", r)
	}
}

func onePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPullImage(t *testing.T) {
	raw := onePNG(t)
	if f := clipboard.ValidateImage(raw); f != "png" {
		t.Fatalf("format %q", f)
	}
	r := pullAgainst(t, func() (clipboard.TransferKind, string, []byte, bool) {
		return clipboard.KindImage, "image", raw, true
	}, clipboard.PostOther, t.TempDir())
	if r.Kind != clipboard.KindImage || !bytes.Equal(r.Data, raw) {
		t.Fatalf("kind=%d len=%d", r.Kind, len(r.Data))
	}
	if f := clipboard.ValidateImage([]byte("garbage")); f != "" {
		t.Fatalf("garbage validated as %q", f)
	}
}

func TestPullFile(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789abcdef"), 7) // 112B, not 16-aligned
	content = append(content, []byte("tail!")...)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	r := pullAgainst(t, func() (clipboard.TransferKind, string, []byte, bool) {
		return clipboard.KindFile, "doc.bin", content, true
	}, clipboard.PostDesktop, dir)
	if r.Kind != clipboard.KindFile {
		t.Fatalf("kind=%d", r.Kind)
	}
	back, err := os.ReadFile(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, content) {
		t.Fatal("file bytes mismatch (alignment pad corrupted?)")
	}
}

func TestServeReceivesPush(t *testing.T) {
	sca, scb := clipPair(t)
	defer sca.Close()
	defer scb.Close()
	comp, _ := clipboard.CompressText("pushed text")
	var got *clipboard.Received
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- clipboard.Serve(scb, clipMagic(), "HOLDER", 2, clipboard.PostOther, 41,
			nil, func(r *clipboard.Received) error { got = r; return nil },
			t.TempDir(), 10*time.Second)
	}()
	// Pusher side: Push header + payload.
	h := clipboard.Header{Push: true, Name: "PUSHER", Src: 1}
	if err := clipboard.WriteHeaderPacket(sca, clipMagic(), h, 42); err != nil {
		t.Fatal(err)
	}
	if err := clipboard.SendTransfer(sca, "text", comp); err != nil {
		t.Fatal(err)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if got == nil || got.Kind != clipboard.KindText || got.Text != "pushed text" {
		t.Fatalf("sink got %+v", got)
	}
}

func TestServeNoData(t *testing.T) {
	sca, scb := clipPair(t)
	defer sca.Close()
	defer scb.Close()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- clipboard.Serve(scb, clipMagic(), "HOLDER", 2, clipboard.PostOther, 41,
			nil, nil, t.TempDir(), 10*time.Second)
	}()
	r, err := clipboard.Pull(sca, clipMagic(), "PULLER", 1, clipboard.PostOther, 42, t.TempDir(), 10*time.Second)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if r.Kind != clipboard.KindError {
		t.Fatalf("kind=%d want error report", r.Kind)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("serve: %v", err)
	}
}
