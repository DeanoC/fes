//go:build sdl3

package tenfoot

/*
#cgo pkg-config: sdl3
#include "sdl_cgo.h"
*/
import "C"

import (
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

func logicalFromSDLButton(code C.int) remoteinput.Code {
	switch code {
	case C.SDL_GAMEPAD_BUTTON_DPAD_UP:
		return remoteinput.ButtonDPadUp
	case C.SDL_GAMEPAD_BUTTON_DPAD_DOWN:
		return remoteinput.ButtonDPadDown
	case C.SDL_GAMEPAD_BUTTON_DPAD_LEFT:
		return remoteinput.ButtonDPadLeft
	case C.SDL_GAMEPAD_BUTTON_DPAD_RIGHT:
		return remoteinput.ButtonDPadRight
	case C.SDL_GAMEPAD_BUTTON_SOUTH:
		return remoteinput.ButtonA
	case C.SDL_GAMEPAD_BUTTON_EAST:
		return remoteinput.ButtonB
	case C.SDL_GAMEPAD_BUTTON_WEST:
		return remoteinput.ButtonX
	case C.SDL_GAMEPAD_BUTTON_NORTH:
		return remoteinput.ButtonY
	case C.SDL_GAMEPAD_BUTTON_LEFT_SHOULDER:
		return remoteinput.ButtonL
	case C.SDL_GAMEPAD_BUTTON_RIGHT_SHOULDER:
		return remoteinput.ButtonR
	case C.SDL_GAMEPAD_BUTTON_START:
		return remoteinput.ButtonStart
	case C.SDL_GAMEPAD_BUTTON_BACK:
		return remoteinput.ButtonSelect
	default:
		return 0
	}
}

func commandFromSDLButton(code C.int) Command {
	if code == C.SDL_GAMEPAD_BUTTON_GUIDE {
		return CmdSettings
	}
	if logical := logicalFromSDLButton(code); logical != 0 {
		return CommandFromLogical(remoteinput.Event{Kind: remoteinput.KindButton, Code: logical})
	}
	return CmdNone
}

func handleSearchKey(app *App, ev *C.FogcastEvent, now time.Time) bool {
	key := C.SDL_Keycode(ev.code)
	shift := C.SDL_GetModState()&C.SDL_KMOD_SHIFT != 0
	tabCmd := CmdTab
	if shift {
		tabCmd = CmdTabPrev
	}
	if ev.down != 0 {
		switch key {
		case C.SDLK_BACKSPACE:
			app.SearchBackspace(now)
		case C.SDLK_ESCAPE:
			app.Press(CmdBack, now)
		case C.SDLK_RETURN:
			app.ConfirmSearch(now)
		case C.SDLK_TAB:
			app.Press(tabCmd, now)
		case C.SDLK_UP, C.SDLK_DOWN, C.SDLK_LEFT, C.SDLK_RIGHT:
			app.Press(commandFromSDLKey(ev.code), now)
		}
		return false
	}
	switch key {
	case C.SDLK_ESCAPE:
		app.Release(CmdBack)
	case C.SDLK_RETURN:
		app.Release(CmdSelect)
	case C.SDLK_TAB:
		app.Release(tabCmd)
	case C.SDLK_UP, C.SDLK_DOWN, C.SDLK_LEFT, C.SDLK_RIGHT:
		app.Release(commandFromSDLKey(ev.code))
	}
	return false
}

func commandFromSDLKey(code C.int) Command {
	return CommandFromKey(sdlKeyName(code))
}

// sdlHIDUsage returns the USB HID Keyboard/Keypad usage of an SDL scancode;
// SDL scancodes are those usages. Larger scancodes have no usage.
func sdlHIDUsage(scancode C.int) uint8 {
	if scancode < 0 || scancode > 0xff {
		return 0
	}
	return uint8(scancode)
}

func sdlPlayKeyName(code C.int) string {
	if name := sdlKeyName(code); name != "" {
		return name
	}
	switch C.SDL_Keycode(code) {
	case C.SDLK_LSHIFT, C.SDLK_RSHIFT:
		return "shift"
	case C.SDLK_PERIOD:
		return "period"
	case C.SDLK_0:
		return "0"
	case C.SDLK_1:
		return "1"
	case C.SDLK_2:
		return "2"
	case C.SDLK_3:
		return "3"
	case C.SDLK_4:
		return "4"
	case C.SDLK_5:
		return "5"
	case C.SDLK_6:
		return "6"
	case C.SDLK_7:
		return "7"
	case C.SDLK_8:
		return "8"
	case C.SDLK_9:
		return "9"
	case C.SDLK_B:
		return "b"
	case C.SDLK_E:
		return "e"
	case C.SDLK_H:
		return "h"
	case C.SDLK_I:
		return "i"
	case C.SDLK_J:
		return "j"
	case C.SDLK_K:
		return "k"
	case C.SDLK_M:
		return "m"
	case C.SDLK_N:
		return "n"
	case C.SDLK_P:
		return "p"
	case C.SDLK_R:
		return "r"
	case C.SDLK_T:
		return "t"
	case C.SDLK_U:
		return "u"
	case C.SDLK_Y:
		return "y"
	case C.SDLK_Z:
		return "z"
	default:
		return ""
	}
}

func sdlKeyName(code C.int) string {
	switch C.SDL_Keycode(code) {
	case C.SDLK_UP:
		return "up"
	case C.SDLK_W:
		return "w"
	case C.SDLK_DOWN:
		return "down"
	case C.SDLK_S:
		return "s"
	case C.SDLK_H:
		return "h"
	case C.SDLK_HOME:
		return "home"
	case C.SDLK_LEFT:
		return "left"
	case C.SDLK_A:
		return "a"
	case C.SDLK_RIGHT:
		return "right"
	case C.SDLK_D:
		return "d"
	case C.SDLK_RETURN:
		return "return"
	case C.SDLK_SPACE:
		return "space"
	case C.SDLK_ESCAPE:
		return "escape"
	case C.SDLK_BACKSPACE:
		return "backspace"
	case C.SDLK_TAB:
		return "tab"
	case C.SDLK_Q:
		return "q"
	case C.SDLK_LEFTBRACKET:
		return "leftbracket"
	case C.SDLK_RIGHTBRACKET:
		return "rightbracket"
	case C.SDLK_X:
		return "x"
	case C.SDLK_SLASH:
		return "slash"
	case C.SDLK_F:
		return "f"
	case C.SDLK_C:
		return "c"
	case C.SDLK_V:
		return "v"
	case C.SDLK_MINUS:
		return "minus"
	case C.SDLK_EQUALS:
		return "equals"
	case C.SDLK_PLUS:
		return "plus"
	case C.SDLK_L:
		return "l"
	case C.SDLK_O:
		return "o"
	case C.SDLK_G:
		return "g"
	default:
		return ""
	}
}

func sdlError() string {
	msg := C.GoString(C.SDL_GetError())
	if msg == "" {
		return "unknown SDL error"
	}
	return msg
}
