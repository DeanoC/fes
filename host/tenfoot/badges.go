package tenfoot

import "strings"

// PortableSystem reports handheld catalog systems already in the system table.
// Presentation.portable can also set the chip when the host sends that field.
func PortableSystem(system string) bool {
	switch strings.ToLower(strings.TrimSpace(system)) {
	case "gb", "gbc", "gba", "gg", "lynx", "ws", "wsc", "psp", "nds", "ngp":
		return true
	default:
		return false
	}
}
