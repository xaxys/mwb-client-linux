// Package keymap translates Windows Virtual Keys <-> Linux evdev/XKB.
//
// M0 ships the US layout; other layouts (DE/FR/...) extend the tables.
// Idea ported from macOS KeyCodeMapper (WinVK <-> Mac virtual key).
package keymap

// Windows Virtual Key codes (US layout; WinUser.h).
const (
	VKBackspace        = 0x08
	VKTab              = 0x09
	VKReturn           = 0x0D
	VKShift            = 0x10
	VKControl          = 0x11
	VKMenu             = 0x12 // Alt
	VKCapital          = 0x14 // CapsLock
	VKEscape           = 0x1B
	VKSpace            = 0x20
	VKLeft             = 0x25
	VKUp               = 0x26
	VKRight            = 0x27
	VKDown             = 0x28
	VKDelete           = 0x2E
	VKLWin             = 0x5B
	VKRWin             = 0x5C
	VKApps             = 0x5D
	VKNumPad0          = 0x60
	VKNumPad1          = 0x61
	VKNumPad2          = 0x62
	VKNumPad3          = 0x63
	VKNumPad4          = 0x64
	VKNumPad5          = 0x65
	VKNumPad6          = 0x66
	VKNumPad7          = 0x67
	VKNumPad8          = 0x68
	VKNumPad9          = 0x69
	VKMultiply         = 0x6A
	VKAdd              = 0x6B
	VKSeparator        = 0x6C
	VKSubtract         = 0x6D
	VKDecimal          = 0x6E
	VKDivide           = 0x6F
	VKPrior            = 0x21 // PageUp
	VKNext             = 0x22 // PageDown
	VKEnd              = 0x23
	VKHome             = 0x24
	VKInsert           = 0x2D
	VKPrint            = 0x2A
	VKSnapshot         = 0x2C // PrintScreen
	VKNumlock          = 0x90
	VKScroll           = 0x91
	VKPause            = 0x13
	VKLShift           = 0xA0
	VKRShift           = 0xA1
	VKLControl         = 0xA2
	VKRControl         = 0xA3
	VKLMenu            = 0xA4
	VKRMenu            = 0xA5
	VKBrowserBack      = 0xA6
	VKBrowserForward   = 0xA7
	VKBrowserRefresh   = 0xA8
	VKBrowserStop      = 0xA9
	VKBrowserSearch    = 0xAA
	VKBrowserFavorites = 0xAB
	VKBrowserHome      = 0xAC
	VKVolumeMute       = 0xAD
	VKVolumeDown       = 0xAE
	VKVolumeUp         = 0xAF
	VKMediaNext        = 0xB0
	VKMediaPrev        = 0xB1
	VKMediaStop        = 0xB2
	VKMediaPlayPause   = 0xB3
	VKLaunchMail       = 0xB8
	VKLaunchMedia      = 0xB9
	VKLaunchApp1       = 0xB6
	VKLaunchApp2       = 0xB7
	VKOEM1             = 0xBA // ;:
	VKOEMPlus          = 0xBB // =+
	VKOEMComma         = 0xBC // ,<
	VKOEMMinus         = 0xBD // -_
	VKOEMPeriod        = 0xBE // .>
	VKOEM2             = 0xBF // /?
	VKOEM3             = 0xC0 // `~
	VKOEM4             = 0xDB // [{
	VKOEM5             = 0xDC // \|
	VKOEM6             = 0xDD // ]}
	VKOEM7             = 0xDE // '"
)

// evdev key codes (linux/input-event-codes.h, US).
const (
	EvEsc            = 1
	Ev1              = 2
	Ev2              = 3
	Ev3              = 4
	Ev4              = 5
	Ev5              = 6
	Ev6              = 7
	Ev7              = 8
	Ev8              = 9
	Ev9              = 10
	Ev0              = 11
	EvMinus          = 12
	EvEqual          = 13
	EvQ              = 16
	EvW              = 17
	EvE              = 18
	EvR              = 19
	EvT              = 20
	EvY              = 21
	EvU              = 22
	EvI              = 23
	EvO              = 24
	EvP              = 25
	EvBracketL       = 26
	EvBracketR       = 27
	EvA              = 30
	EvS              = 31
	EvD              = 32
	EvF              = 33
	EvG              = 34
	EvH              = 35
	EvJ              = 36
	EvK              = 37
	EvL              = 38
	EvSemicolon      = 39
	EvApostrophe     = 40
	EvGrave          = 41
	EvZ              = 44
	EvX              = 45
	EvC              = 46
	EvV              = 47
	EvB              = 48
	EvN              = 49
	EvM              = 50
	EvComma          = 51
	EvDot            = 52
	EvSlash          = 53
	EvRightShift     = 54
	EvKpAsterisk     = 55
	EvSpace          = 57
	EvCapsLock       = 58
	EvF1             = 59
	EvF2             = 60
	EvF3             = 61
	EvF4             = 62
	EvF5             = 63
	EvF6             = 64
	EvF7             = 65
	EvF8             = 66
	EvF9             = 67
	EvF10            = 68
	EvNumLock        = 69
	EvScrollLock     = 70
	EvKp7            = 71
	EvKp8            = 72
	EvKp9            = 73
	EvKpMinus        = 74
	EvKp4            = 75
	EvKp5            = 76
	EvKp6            = 77
	EvKpPlus         = 78
	EvKp1            = 79
	EvKp2            = 80
	EvKp3            = 81
	EvKp0            = 82
	EvKpDot          = 83
	EvF11            = 87
	EvF12            = 88
	EvKpEnter        = 96
	EvRightCtrl      = 97
	EvKpSlash        = 98
	EvSysRq          = 99
	EvRightAlt       = 100
	EvHome           = 102
	EvLeft           = 105
	EvUp             = 103
	EvPageUp         = 104
	EvRight          = 106
	EvEnd            = 107
	EvDown           = 108
	EvPageDown       = 109
	EvInsert         = 110
	EvDelete         = 111
	EvMute           = 113
	EvVolumeDown     = 114
	EvVolumeUp       = 115
	EvKpEqual        = 117
	EvPause          = 119
	EvKpComma        = 121
	EvLeftMeta       = 125
	EvRightMeta      = 126
	EvCompose        = 127
	EvStop           = 128
	EvAgain          = 129
	EvProps          = 130
	EvUndo           = 131
	EvFront          = 132
	EvCopy           = 133
	EvOpen           = 134
	EvPaste          = 135
	EvFind           = 136
	EvCut            = 137
	EvHelp           = 138
	EvMenu           = 139
	EvCalc           = 140
	EvSetup          = 141
	EvSleep          = 142
	EvWakeUp         = 143
	EvFile           = 144
	EvSendFile       = 145
	EvDeleteFile     = 146
	EvXfer           = 147
	EvProg1          = 148
	EvProg2          = 149
	EvWWW            = 150
	EvMSDOS          = 151
	EvCoffee         = 152
	EvRotateDisplay  = 153
	EvCycleWindows   = 154
	EvMail           = 155
	EvBookmarks      = 156
	EvComputer       = 157
	EvBack           = 158
	EvForward        = 159
	EvCloseCD        = 160
	EvEjectCD        = 161
	EvEjectCloseCD   = 162
	EvNextSong       = 163
	EvPlayPause      = 164
	EvPrevSong       = 165
	EvStopCD         = 166
	EvRecord         = 167
	EvRewind         = 168
	EvPhone          = 169
	EvISO            = 170
	EvConfig         = 171
	EvHomepage       = 172
	EvRefresh        = 173
	EvExit           = 174
	EvMove           = 175
	EvEdit           = 176
	EvScrollUp       = 177
	EvScrollDown     = 178
	EvKpLeftParen    = 179
	EvKpRightParen   = 180
	EvF13            = 183
	EvF14            = 184
	EvF15            = 185
	EvF16            = 186
	EvF17            = 187
	EvF18            = 188
	EvF19            = 189
	EvF20            = 190
	EvF21            = 191
	EvF22            = 192
	EvF23            = 193
	EvF24            = 194
	EvPlayCD         = 200
	EvPauseCD        = 201
	EvProg3          = 202
	EvProg4          = 203
	EvSuspend        = 205
	EvClose          = 206
	EvPlay           = 207
	EvFastForward    = 208
	EvBassBoost      = 209
	EvPrint          = 210
	EvHP             = 211
	EvCamera         = 212
	EvSound          = 213
	EvQuestion       = 214
	EvEmail          = 215
	EvChat           = 216
	EvSearch         = 217
	EvConnect        = 218
	EvFinance        = 219
	EvSport          = 220
	EvShop           = 221
	EvAltErase       = 222
	EvCancel         = 223
	EvBrightnessDown = 224
	EvBrightnessUp   = 225
	EvMedia          = 226
	EvUnknown        = 240
	EvLeftShift      = 42
	EvLeftCtrl       = 29
	EvBackslash      = 43
	EvLeftAlt        = 56
	EvEnter          = 28
	EvTab            = 15
	EvBackspace      = 14
)

// vkToEvdevUS covers the full US layout: letters, digits, F1-F12, numpad,
// symbol row, nav cluster, L/R modifiers, and media/browser/launch keys.
// Generic VK_SHIFT/CONTROL/MENU fall back to the left evdev code.
var vkToEvdevUS = map[int]int{
	0x20: EvSpace,
	0x0D: EvEnter,
	0x09: EvTab,
	0x08: EvBackspace,
	0x1B: EvEsc,
	0x2E: EvDelete,
	0x25: EvLeft, 0x26: EvUp, 0x27: EvRight, 0x28: EvDown,
	0x10: EvLeftShift, 0x11: EvLeftCtrl, 0x12: EvLeftAlt,
	0xA0: EvLeftShift, 0xA1: EvRightShift,
	0xA2: EvLeftCtrl, 0xA3: EvRightCtrl,
	0xA4: EvLeftAlt, 0xA5: EvRightAlt,
	0x5B: EvLeftMeta, 0x5C: EvRightMeta, 0x5D: EvMenu,
	0x14: EvCapsLock,
	// Nav cluster.
	0x2D: EvInsert, 0x24: EvHome, 0x21: EvPageUp,
	0x23: EvEnd, 0x22: EvPageDown,
	0x2C: EvSysRq, // PrintScreen
	0x13: EvPause, 0x90: EvNumLock, 0x91: EvScrollLock,
	0x2A: EvPrint,
	// OEM symbol row.
	0xBA: EvSemicolon, 0xBB: EvEqual, 0xBC: EvComma,
	0xBD: EvMinus, 0xBE: EvDot, 0xBF: EvSlash, 0xC0: EvGrave,
	0xDB: EvBracketL, 0xDC: EvBackslash, 0xDD: EvBracketR, 0xDE: EvApostrophe,
	// Numpad.
	0x60: EvKp0, 0x61: EvKp1, 0x62: EvKp2, 0x63: EvKp3, 0x64: EvKp4,
	0x65: EvKp5, 0x66: EvKp6, 0x67: EvKp7, 0x68: EvKp8, 0x69: EvKp9,
	0x6A: EvKpAsterisk, 0x6B: EvKpPlus, 0x6D: EvKpMinus,
	0x6E: EvKpDot, 0x6F: EvKpSlash,
	// Media / browser / launch (best-effort consumer mapping).
	0xAD: EvMute, 0xAE: EvVolumeDown, 0xAF: EvVolumeUp,
	0xB0: EvNextSong, 0xB1: EvPrevSong, 0xB2: EvStopCD, 0xB3: EvPlayPause,
	0xA6: EvBack, 0xA7: EvForward, 0xA8: EvRefresh,
	0xAA: EvSearch, 0xAB: EvBookmarks, 0xAC: EvHomepage,
	0xB6: EvProg1, 0xB7: EvProg2, 0xB8: EvMail, 0xB9: EvMedia,
}

// genericVKs are the side-agnostic modifiers; on reverse lookup the
// side-specific VK (A0-A5) wins deterministically.
var genericVKs = map[int]bool{0x10: true, 0x11: true, 0x12: true}

func init() {
	// A-Z: evdev codes are not sequential — fill known US map.
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
	fkeys := map[int]int{
		0x70: 59, 0x71: 60, 0x72: 61, 0x73: 62, 0x74: 63, 0x75: 64,
		0x76: 65, 0x77: 66, 0x78: 67, 0x79: 68, 0x7A: 87, 0x7B: 88,
	}
	for k, v := range fkeys {
		vkToEvdevUS[k] = v
	}
}

var evdevToVKUS map[int]int

func init() {
	evdevToVKUS = make(map[int]int, len(vkToEvdevUS))
	// First pass: side-specific and ordinary keys win.
	for vk, ev := range vkToEvdevUS {
		if genericVKs[vk] {
			continue
		}
		if _, ok := evdevToVKUS[ev]; !ok {
			evdevToVKUS[ev] = vk
		}
	}
	// Second pass: generic modifiers only fill gaps.
	for vk, ev := range vkToEvdevUS {
		if !genericVKs[vk] {
			continue
		}
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
