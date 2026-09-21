package kitlauncher

// ShouldPaintHDMI reports whether fogcast-kit may blit the HPS linuxfb.
//
// An active session does not paint: the game owns HDMI. Loading and stopping
// feedback still paints when the idle enables the framebuffer, because that
// copy is the temporary overlay rather than the game picture.
//
// Confirmed idle without an HPS framebuffer — production SplashIdle omits SPI
// 0x002f — does not paint, even when the model holds a catalog, detail pane,
// or attract list. FPGA splash pixels stay on HDMI. The kit process keeps
// running; a missing linuxfb device must not stop it.
//
// When the idle still enables the HPS framebuffer, linuxfb may show the
// existing connecting, retry, and last-good shelf shell. That shell is a
// temporary overlay. It is not Menu's file browser, not rooms, and not a
// permanent catalog-on-linuxfb product. Omission of hps_framebuffer matches
// the splash contract. Launcher config or an explicit session field opts in.
func ShouldPaintHDMI(m Model) bool {
	if !m.Session.HPSFramebuffer {
		return false
	}
	if m.Busy {
		return true
	}
	return m.Session.State != "active"
}
