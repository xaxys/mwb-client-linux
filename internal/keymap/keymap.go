// Package keymap translates Windows Virtual Keys <-> Linux evdev/XKB.
//
// M0 ships the US layout; other layouts (DE/FR/...) extend the tables.
// Idea ported from macOS KeyCodeMapper (WinVK <-> Mac virtual key).
package keymap

// Windows Virtual Key codes (subset; full table extended in M2).
const (
	VKBackspace = 0x08
	VKTab       = 0x09
	VKReturn    = 0x0D
	VKShift     = 0x10
	VKControl   = 0x11
	VKMenu      = 0x12 // Alt
	VKCapital   = 0x14 // CapsLock
	VKEscape    = 0x1B
	VKSpace     = 0x20
	VKLeft      = 0x25
	VKUp        = 0x26
	VKRight     = 0x27
	VKDown      = 0x28
	VKDelete    = 0x2E
	VKLWin      = 0x5B
	VKRWin      = 0x5C
)

// evdev key codes (linux/input-event-codes.h subset, US).
const (
	EvEsc        = 1
	Ev1          = 2
	EvQ          = 16
	EvW          = 17
	EvE          = 18
	EvA          = 30
	EvZ          = 44
	EvSpace      = 57
	EvCapsLock   = 58
	EvLeftShift  = 42
	EvRightShift = 54
	EvLeftCtrl   = 29
	EvRightCtrl  = 97
	EvLeftAlt    = 56
	EvRightAlt   = 100
	EvLeftMeta   = 125
	EvRightMeta  = 126
	EvEnter      = 28
	EvTab        = 15
	EvBackspace  = 14
	EvDelete     = 111
	EvLeft       = 105
	EvUp         = 103
	EvRight      = 106
	EvDown       = 108
)

// vkToEvdevUS covers letters/digits + common controls (US layout).
var vkToEvdevUS = map[int]int{
	0x41: EvA, // A (full A-Z filled in init)
	0x20: EvSpace,
	0x0D: EvEnter,
	0x09: EvTab,
	0x08: EvBackspace,
	0x1B: EvEsc,
	0x2E: EvDelete,
	0x25: EvLeft, 0x26: EvUp, 0x27: EvRight, 0x28: EvDown,
	0x10: EvLeftShift, 0x11: EvLeftCtrl, 0x12: EvLeftAlt,
	0x5B: EvLeftMeta, 0x5C: EvRightMeta,
	0x14: EvCapsLock,
}

func init() {
	// A-Z: evdev scancodes are sequential-ish for letters? No — fill known US map.
	az := map[int]int{
		0x41: 30, 0x42: 48, 0x43: 46, 0x44: 32, 0x45: 18, 0x46: 33,
		0x47: 34, 0x48: 35, 0x49: 23, 0x4A: 36, 0x4B: 37, 0x4C: 38,
		0x4D: 50, 0x4E: 49, 0x4F: 24, 0x50: 25, 0x51: 16, 0x52: 19,
		0x53: 31, 0x54: 20, 0x55: 22, 0x56: 47, 0x57: 17, 0x58: 45,
		0x59: 21, 0x5A: 44,
	}
	for k, v := range az {
		vkToEvdevUS[k] = v
	}
	digits := map[int]int{
		0x30: 11, 0x31: 2, 0x32: 3, 0x33: 4, 0x34: 5,
		0x35: 6, 0x36: 7, 0x37: 8, 0x38: 9, 0x39: 10,
	}
	for k, v := range digits {
		vkToEvdevUS[k] = v
	}
}

var evdevToVKUS map[int]int

func init() {
	evdevToVKUS = make(map[int]int, len(vkToEvdevUS))
	for vk, ev := range vkToEvdevUS {
		// prefer left-modifier VK on reverse collisions
		if _, ok := evdevToVKUS[ev]; !ok {
			evdevToVKUS[ev] = vk
		}
	}
}

// WinVKToEvdev maps a Windows VK to evdev (US). ok=false if unmapped.
func WinVKToEvdev(vk int) (ev int, ok bool) {
	ev, ok = vkToEvdevUS[vk]
	return ev, ok
}

// EvdevToWinVK maps evdev back to Windows VK (US). ok=false if unmapped.
func EvdevToWinVK(ev int) (vk int, ok bool) {
	vk, ok = evdevToVKUS[ev]
	return vk, ok
}
