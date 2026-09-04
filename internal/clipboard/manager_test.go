package clipboard_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/clipboard"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

func TestManagerTextBeatGate(t *testing.T) {
	beats := 0
	m := clipboard.NewManager("ME", t.TempDir(), util.NewLogger("test"),
		func() { beats++ }, nil)
	if err := m.SetText("one"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetText("two"); err != nil { // within 1s gate: no second beat
		t.Fatal(err)
	}
	if beats != 1 {
		t.Fatalf("beats=%d want 1", beats)
	}
	if s, ok := m.Text(); !ok || s != "two" {
		t.Fatalf("text=%q", s)
	}
	kind, name, data, ok := m.Provide()
	if !ok || kind != clipboard.KindText || name != "text" || len(data) == 0 {
		t.Fatalf("provide %d %q %d", kind, name, len(data))
	}
	// Sink stores pushed text.
	if err := m.Sink(&clipboard.Received{Kind: clipboard.KindText, Name: "peer", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Text(); s != "hi" {
		t.Fatalf("sink text=%q", s)
	}
}

func TestManagerBeats(t *testing.T) {
	var beats []string
	m := clipboard.NewManager("ME", t.TempDir(), util.NewLogger("test"),
		func() { beats = append(beats, "beat") }, nil)
	m.OnBeat("ME", 1) // own echo ignored
	m.OnBeat("YOU", 2)
	if who, id := m.LastMachine(); who != "YOU" || id != 2 {
		t.Fatalf("last=%q %d", who, id)
	}
	if len(beats) != 0 {
		t.Fatal("OnBeat must not emit")
	}
}

func TestManagerImageAndFile(t *testing.T) {
	m := clipboard.NewManager("ME", t.TempDir(), util.NewLogger("test"), nil, nil)
	raw := onePNG(t)
	m.SetImage(raw)
	if got, f := m.Image(); f != "png" || len(got) != len(raw) {
		t.Fatalf("image %d %q", len(got), f)
	}
	kind, name, _, ok := m.Provide()
	if !ok || kind != clipboard.KindImage || name != "image" {
		t.Fatalf("provide %d %q", kind, name)
	}
	// File replaces image.
	dir := t.TempDir()
	fp := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(fp, []byte("file-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.SetFile(fp); err != nil {
		t.Fatal(err)
	}
	kind, name, data, ok := m.Provide()
	if !ok || kind != clipboard.KindFile || name != "a.txt" || string(data) != "file-bytes" {
		t.Fatalf("provide %d %q %q", kind, name, data)
	}
	// Directories and missing files are rejected.
	if err := m.SetFile(dir); err == nil {
		t.Fatal("directory accepted")
	}
	if err := m.SetFile(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("missing file accepted")
	}
}
