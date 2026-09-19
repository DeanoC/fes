package tenfoot

import (
	"fmt"
	"github.com/DeanoC/FogCast/ui/shared"
	"strings"
)

func selectWord(kind InputKind) string {
	switch kind {
	case InputKeyboard:
		return "Enter"
	case InputMouse:
		return "click"
	default:
		return "A"
	}
}

func backWord(kind InputKind) string {
	switch kind {
	case InputKeyboard:
		return "Esc"
	case InputMouse:
		return "click empty"
	default:
		return "B"
	}
}

func westWord(kind InputKind) string {
	switch kind {
	case InputKeyboard, InputMouse:
		return "x"
	default:
		return "X"
	}
}

func northWord(kind InputKind) string {
	switch kind {
	case InputKeyboard, InputMouse:
		return "/"
	default:
		return "Y"
	}
}

func startWord(kind InputKind) string {
	switch kind {
	case InputKeyboard, InputMouse:
		return "Q"
	default:
		return "START"
	}
}

func layoutWord(kind InputKind) string {
	switch kind {
	case InputKeyboard, InputMouse:
		return "l"
	default:
		return "SELECT"
	}
}

func browseHint(kind InputKind) string {
	switch kind {
	case InputKeyboard:
		return "[ ] platform  x sort  g filter  Tab search  Enter launch  v fav  l layout  h rooms  o settings"
	case InputMouse:
		return "move focus  click launch"
	default:
		return "LB/RB platform  X sort  hold X filter  Y search  hold A view  hold Y fav  SELECT layout  hold B rooms  GUIDE settings"
	}
}

func sessionChromeHint(kind InputKind, retry bool) string {
	stop := "B stop"
	switch kind {
	case InputKeyboard, InputMouse:
		if retry {
			stop = "Esc/S retry Stop"
		} else {
			stop = "Esc/S stop"
		}
	default:
		if retry {
			stop = "B retry Stop"
		}
	}
	return stop + "  " + startWord(kind) + " quit  " + layoutWord(kind) + " layout"
}

func detailHintFor(kind InputKind, count int) string {
	switch kind {
	case InputKeyboard:
		if count > 1 {
			return "Enter launch  Esc back  Left/Right screenshots  o settings"
		}
		return "Enter launch  Esc back  o settings"
	case InputMouse:
		return "click launch  click empty back"
	default:
		return detailHint(count)
	}
}

func oskHintFor(kind InputKind, page int) string {
	switch kind {
	case InputKeyboard:
		charset := "Tab symbols"
		if page == shared.OSKPageSymbols {
			charset = "Tab letters"
		}
		return "type  Enter done  Esc close  " + charset
	case InputMouse:
		return "click type  click empty close"
	default:
		return shared.OSKHint(page)
	}
}

func filterHintFor(kind InputKind, apply bool) string {
	if apply {
		return selectWord(kind) + " apply  " + backWord(kind) + " back"
	}
	return selectWord(kind) + " select  " + backWord(kind) + " back"
}

func viewPickerHint(kind InputKind) string {
	return selectWord(kind) + " open  " + westWord(kind) + " add/remove  " + northWord(kind) + " manage  " + backWord(kind) + " back"
}

func viewPickerFooterHint(kind InputKind) string {
	return selectWord(kind) + " open  " + westWord(kind) + " add/remove  " + northWord(kind) + " manage"
}

func collectionHintFor(kind InputKind, confirm bool) string {
	if confirm {
		return selectWord(kind) + " delete  " + backWord(kind) + " cancel"
	}
	return selectWord(kind) + " select  " + backWord(kind) + " back"
}

func settingsDefaultHint(kind InputKind) string {
	return selectWord(kind) + " confirm  " + backWord(kind) + " close  Left/Right change"
}

func settingsActionHint(kind InputKind, action, extra string) string {
	hint := selectWord(kind) + " " + action + "  " + backWord(kind) + " close"
	if extra != "" {
		hint = selectWord(kind) + " " + action + "  " + extra + "  " + backWord(kind) + " close"
	}
	return hint
}

func settingsRemoveHint(kind InputKind, action, extra string) string {
	hint := selectWord(kind) + " " + action + "  " + westWord(kind) + " remove  " + backWord(kind) + " close"
	if extra != "" {
		hint = selectWord(kind) + " " + action + "  " + extra + "  " + westWord(kind) + " remove  " + backWord(kind) + " close"
	}
	return hint
}

func sessionAttachFallback(kind InputKind) string {
	return westWord(kind) + " attach/detach"
}

// AffinityBadge is the header device label for the current owner.
func (s Snapshot) AffinityBadge() string {
	switch s.Affinity {
	case InputMouse:
		return "MOUSE"
	case InputGamepad:
		if s.Gamepads > 0 {
			return fmt.Sprintf("PAD %d", s.Gamepads)
		}
		return "PAD"
	case InputKeyboard:
		return "KB"
	default:
		if s.Gamepads > 0 {
			return fmt.Sprintf("PAD %d", s.Gamepads)
		}
		return "KB"
	}
}

// HeaderHint is the on-screen footer/header hint for the affinity device.
func (s Snapshot) HeaderHint() string {
	kind := s.Affinity
	if s.GPUParked || s.Session.State == "active" || s.Session.RetryStop {
		hint := sessionChromeHint(kind, s.Session.RetryStop)
		if h := strings.TrimSpace(s.Session.InputHint); h != "" {
			return hint + "  " + h
		}
		if !s.Session.Diagnostic && s.Session.InputState != "" {
			return hint + "  " + sessionAttachFallback(kind)
		}
		return hint
	}
	if s.OSK.Open {
		if h := strings.TrimSpace(s.OSK.Hint); h != "" {
			return h
		}
		return oskHintFor(kind, s.OSK.Page)
	}
	if s.Filters.Open {
		if h := strings.TrimSpace(s.Filters.Hint); h != "" {
			return h
		}
		return filterHintFor(kind, false)
	}
	if s.CollectionMenu.Open {
		if h := strings.TrimSpace(s.CollectionMenu.Hint); h != "" {
			return h
		}
		return collectionHintFor(kind, s.CollectionMenu.Confirm)
	}
	if s.ViewPicker {
		return viewPickerHint(kind)
	}
	if s.RoomPicker.Open {
		return roomPickerHint(kind)
	}
	if s.Room.Open {
		if s.Room.Err != "" {
			return backWord(kind) + " home"
		}
		if s.Room.Choice.Open {
			if h := strings.TrimSpace(s.Room.Choice.Hint); h != "" {
				return h
			}
			return roomChoiceHint(kind)
		}
		if s.Detail.Open {
			return selectWord(kind) + " play  " + backWord(kind) + " close  " + settingsWord(kind) + " settings"
		}
		action := strings.TrimSpace(s.Room.Destination.Action)
		if action == "" {
			action = "confirm"
		}
		return selectWord(kind) + " " + strings.ToLower(action) + "  " + detailsWord(kind) + " details  " + backWord(kind) + " back  " + homeWord(kind) + " rooms  " + settingsWord(kind) + " settings"
	}
	if s.Detail.Open {
		if h := strings.TrimSpace(s.Detail.Hint); h != "" {
			return h
		}
		return detailHintFor(kind, s.Detail.Count)
	}
	return browseHint(kind)
}
