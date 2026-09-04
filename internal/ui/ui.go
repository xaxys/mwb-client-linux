// Package ui: M0 CLI + tray stub (systray/AppIndicator lands in M5;
// Fyne/GTK settings pages land in M3+). UI surfaces the probed backend
// honestly and never pretends portal is available.
package ui

import (
	"fmt"

	"github.com/xaxys/mwb-client-linux/internal/input"
)

// DescribeBackend renders the backend status line for CLI/tray.
func DescribeBackend(k input.BackendKind) string {
	if input.RestrictedMode(k) {
		return fmt.Sprintf("backend: %s (restricted — evdev needs input group/udev; Wayland cursor-hide TBD)", string(k))
	}
	return fmt.Sprintf("backend: %s", string(k))
}
