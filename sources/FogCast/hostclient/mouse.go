package hostclient

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/remoteinput"
)

// SendMouseRelativeForSession posts one bounded relative report through the
// current session's attached input stream. A failed post is never replayed.
func (c *Client) SendMouseRelativeForSession(ctx context.Context, session SessionResult, dx, dy int16, buttons uint8) error {
	if buttons > 3 {
		return errors.New("mouse buttons must be 0..3")
	}
	if session.State != "active" || !sessionHasMouseBinding(session) || session.Input == nil || session.Input.State != "attached" || !session.Input.Ready {
		return errors.New("active mouse input is not attached")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/v1/session/input/event", struct {
		Event remoteinput.Event `json:"event"`
	}{Event: remoteinput.MouseEvent(dx, dy, buttons)}, nil)
}

func sessionHasMouseBinding(session SessionResult) bool {
	core := session.CorePackage
	if core == nil || core.ABI != (SessionCoreABI{ID: "fes.computer", Major: 1}) || core.Generation == 0 || len(core.PackageID) != 64 || strings.ToLower(core.PackageID) != core.PackageID {
		return false
	}
	if _, err := hex.DecodeString(core.PackageID); err != nil {
		return false
	}
	for _, contract := range core.ActiveInterfaces {
		if contract == (SessionCoreInterface{ID: "fes.mouse.relative", Major: 1}) {
			return true
		}
	}
	return false
}
