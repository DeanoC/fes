package fogcastcli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/DeanoC/FogCast/hostclient"
)

func mouseArguments(args []string) (int16, int16, uint8, error) {
	if len(args) != 4 {
		return 0, 0, 0, fmt.Errorf("mouse requires dx, dy and buttons")
	}
	x, errX := strconv.ParseInt(args[1], 10, 16)
	y, errY := strconv.ParseInt(args[2], 10, 16)
	buttons, errButtons := strconv.ParseUint(args[3], 10, 8)
	if errX != nil || errY != nil || errButtons != nil || buttons > 3 {
		return 0, 0, 0, fmt.Errorf("mouse dx/dy must be -32768..32767 and buttons 0..3")
	}
	return int16(x), int16(y), uint8(buttons), nil
}

func mouseCommandValid(args []string) bool {
	_, _, _, err := mouseArguments(args)
	return err == nil
}

func mouseThroughHostAPI(ctx context.Context, origin string, args []string) commandResult {
	dx, dy, buttons, err := mouseArguments(args)
	if err != nil {
		return commandResult{err: err, exit: 2}
	}
	client := hostclient.NewClient(origin, hostJSONClient())
	session, err := client.Session(ctx)
	if err == nil {
		err = client.SendMouseRelativeForSession(ctx, session, dx, dy, buttons)
	}
	if err != nil {
		return commandResult{err: err, exit: 1}
	}
	result := struct {
		DX      int16 `json:"dx"`
		DY      int16 `json:"dy"`
		Buttons uint8 `json:"buttons"`
	}{dx, dy, buttons}
	return commandResult{jsonValue: result, human: func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "mouse dx=%d dy=%d buttons=%d\n", dx, dy, buttons)
		return err
	}}
}
