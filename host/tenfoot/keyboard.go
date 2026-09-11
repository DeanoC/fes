package tenfoot

// Keyboard browse/nav mapping for USB HID on the SDL tenfoot path.
// This is independent of play-session HID forwarding (coreKeyFromSDL in
// sdl.go, used while ForwardsPlayHID / an attached session is active).

// CommandFromKey maps USB keyboard keys onto the sofa focus graph.
// Names are SDL-style identifiers (see commandFromSDLKey).
func CommandFromKey(name string) Command {
	switch name {
	case "up", "w":
		return CmdUp
	case "down":
		return CmdDown
	case "left", "a":
		return CmdLeft
	case "right", "d":
		return CmdRight
	case "return", "space":
		return CmdSelect
	case "escape", "backspace":
		return CmdBack
	case "tab":
		return CmdTab
	case "shift-tab":
		return CmdTabPrev
	case "s":
		return CmdStop
	case "q":
		return CmdQuit
	case "leftbracket", "[":
		return CmdFilterPrev
	case "rightbracket", "]":
		return CmdFilterNext
	case "x":
		return CmdSortCycle
	case "/", "slash", "f":
		return CmdSearch
	case "c":
		return CmdViewNext
	case "v", "*":
		return CmdFavorite
	case "-", "minus":
		return CmdSafeAreaOut
	case "=", "plus", "equals":
		return CmdSafeAreaIn
	case "l":
		return CmdLayoutCycle
	case "o":
		return CmdSettings
	case "g":
		return CmdFilters
	default:
		return CmdNone
	}
}
