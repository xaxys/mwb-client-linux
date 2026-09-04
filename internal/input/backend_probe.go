package input

import (
	"os"
	"runtime"
	"strings"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

// BackendKind selects the capture/inject implementation.
type BackendKind string

const (
	BackendX11     BackendKind = "x11"
	BackendPortal  BackendKind = "wayland-portal"
	BackendEvdev   BackendKind = "evdev-uinput"
	BackendUnknown BackendKind = "unknown"
)

// Probe picks the backend at runtime: XDG_SESSION_TYPE + portal capability.
// Never hard-code; UI must display the chosen backend honestly.
// Non-linux hosts (Windows dev/CI) always report unknown unless overridden
// via MWB_FORCE_BACKEND (x11|wayland-portal|evdev-uinput) for testing.
func Probe(log *util.Logger) BackendKind {
	if force := strings.ToLower(os.Getenv("MWB_FORCE_BACKEND")); force != "" {
		switch BackendKind(force) {
		case BackendX11, BackendPortal, BackendEvdev:
			return BackendKind(force)
		}
	}
	if runtime.GOOS != "linux" {
		return BackendUnknown
	}
	sess := strings.ToLower(os.Getenv("XDG_SESSION_TYPE"))
	desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	_ = desktop
	if sess == "x11" || os.Getenv("DISPLAY") != "" && sess != "wayland" {
		// X11 session (22.04主力/24.04可选): full fidelity.
		return BackendX11
	}
	if sess == "wayland" {
		if portalInputCaptureAvailable() {
			return BackendPortal // GNOME46+ (24.04)
		}
		return BackendEvdev // GNOME42 (22.04) / portal missing: restricted
	}
	// Unknown (ssh/docker/CI): report honestly.
	if log != nil {
		log.Warnf("unknown session type %q, no input backend", sess)
	}
	return BackendUnknown
}

// portalInputCaptureAvailable checks for the InputCapture portal.
// Real check shells to gdbus/query portal version; M0 heuristic:
// Wayland + GNOME46+ version file or explicit override env.
func portalInputCaptureAvailable() bool {
	if v := os.Getenv("MWB_FORCE_PORTAL"); v == "1" {
		return true
	}
	if v := os.Getenv("MWB_FORCE_PORTAL"); v == "0" {
		return false
	}
	// Best-effort: GNOME version via /usr/share/gnome/gnome-version.xml.
	data, err := os.ReadFile("/usr/share/gnome/gnome-version.xml")
	if err != nil {
		return false
	}
	s := string(data)
	// GNOME 46+ ships InputCapture (xdg-desktop-portal-gnome 46+).
	for _, ver := range []string{">46", "46", "47", "48"} {
		_ = ver
	}
	// crude: look for <platform>46+ markers
	if strings.Contains(s, "<minor>46</minor>") || strings.Contains(s, "<platform>46") {
		return true
	}
	// GNOME 47/48 contain "47"/"48" platform tags
	if strings.Contains(s, "47") || strings.Contains(s, "48") {
		// avoid false positives from unrelated numbers: require platform tag
		if strings.Contains(s, "platform") {
			return true
		}
	}
	return false
}

// RestrictedMode reports whether the backend is degraded (evdev on Wayland:
// needs input group/udev uaccess; cursor-hide under mutter TBD on hardware).
func RestrictedMode(k BackendKind) bool { return k == BackendEvdev }
