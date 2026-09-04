package keymap_test

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/keymap"
)

func TestRoundTripUS(t *testing.T) {
	for _, vk := range []int{0x41, 0x5A, 0x30, 0x20, 0x0D, 0x1B} {
		ev, ok := keymap.WinVKToEvdev(vk)
		if !ok {
			t.Fatalf("VK %02X unmapped", vk)
		}
		back, ok := keymap.EvdevToWinVK(ev)
		if !ok || back != vk {
			t.Fatalf("round trip VK %02X -> ev %d -> VK %02X", vk, ev, back)
		}
	}
}

func TestModifiers(t *testing.T) {
	if _, ok := keymap.WinVKToEvdev(keymap.VKShift); !ok {
		t.Fatal("shift unmapped")
	}
	if _, ok := keymap.WinVKToEvdev(keymap.VKLWin); !ok {
		t.Fatal("lwin unmapped")
	}
}

// TestFullUSForward covers every mapped US VK (letters, digits, F1-F12,
// numpad, OEM symbols, nav cluster, modifiers, media/browser/launch).
func TestFullUSForward(t *testing.T) {
	vks := []int{
		0x08, 0x09, 0x0D, 0x1B, 0x20,
		0x25, 0x26, 0x27, 0x28,
		0x2D, 0x24, 0x21, 0x23, 0x22, 0x2E,
		0x2C, 0x13, 0x90, 0x91, 0x2A, 0x14,
		0x10, 0x11, 0x12, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5,
		0x5B, 0x5C, 0x5D,
		0xBA, 0xBB, 0xBC, 0xBD, 0xBE, 0xBF, 0xC0, 0xDB, 0xDC, 0xDD, 0xDE,
		0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
		0x6A, 0x6B, 0x6D, 0x6E, 0x6F,
		0xAD, 0xAE, 0xAF, 0xB0, 0xB1, 0xB2, 0xB3,
		0xA6, 0xA7, 0xA8, 0xAA, 0xAB, 0xAC,
		0xB6, 0xB7, 0xB8, 0xB9,
	}
	for vk := 0x30; vk <= 0x39; vk++ {
		vks = append(vks, vk)
	}
	for vk := 0x41; vk <= 0x5A; vk++ {
		vks = append(vks, vk)
	}
	for vk := 0x70; vk <= 0x7B; vk++ {
		vks = append(vks, vk)
	}
	for _, vk := range vks {
		if _, ok := keymap.WinVKToEvdev(vk); !ok {
			t.Fatalf("VK %02X unmapped", vk)
		}
	}
}

// TestFullUSRoundTrip checks VK -> evdev -> VK stability. Generic
// modifiers (10/11/12) intentionally resolve to side-specific VKs.
func TestFullUSRoundTrip(t *testing.T) {
	skip := map[int]bool{0x10: true, 0x11: true, 0x12: true}
	count := 0
	for vk := 0x08; vk <= 0xDE; vk++ {
		ev, ok := keymap.WinVKToEvdev(vk)
		if !ok || skip[vk] {
			continue
		}
		back, ok := keymap.EvdevToWinVK(ev)
		if !ok || back != vk {
			t.Fatalf("round trip VK %02X -> ev %d -> VK %02X", vk, ev, back)
		}
		count++
	}
	if count < 100 {
		t.Fatalf("only %d VKs round-tripped, want >= 100", count)
	}
}

// TestModifierSides pins the deterministic reverse: evdev LR codes map to
// side-specific VKs (A0-A5), never to generic 10/11/12.
func TestModifierSides(t *testing.T) {
	want := map[int]int{
		42: 0xA0, 54: 0xA1,
		29: 0xA2, 97: 0xA3,
		56: 0xA4, 100: 0xA5,
	}
	for ev, vk := range want {
		if back, ok := keymap.EvdevToWinVK(ev); !ok || back != vk {
			t.Fatalf("ev %d -> VK %02X, want %02X", ev, back, vk)
		}
	}
}
