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
