package fogcastcli

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

func diskDataThroughHostAPI(ctx context.Context, origin, gameID, baseID string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	client := hostclient.NewClient(origin, hostJSONClient())
	prior, err := client.Session(ctx)
	if err != nil {
		return fail(err)
	}
	var after hostclient.SessionResult
	if gameID == "" {
		after, err = client.SaveDiskForSession(ctx, prior)
	} else {
		after, err = client.InsertLibraryDiskForSession(ctx, prior, gameID, baseID)
	}
	if err != nil {
		return fail(err)
	}
	return commandResult{jsonValue: after, human: func(w io.Writer) error {
		return writeHumanStatusResult(w, statusResult{State: protocol.State(after.State)})
	}}
}

// floppyThroughHostAPI drives pc386's drives:
// insert-floppy <A|B> <game-id> <base-media-id> [write-protected] | eject-floppy <A|B>.
func floppyThroughHostAPI(ctx context.Context, origin string, args []string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	drive := ""
	if len(args) > 1 {
		drive = strings.ToUpper(args[1])
	}
	unit, ok := map[string]uint8{"A": protocol.PcFloppyUnitA, "B": protocol.PcFloppyUnitB}[drive]
	insert := args[0] == "insert-floppy"
	if !ok || (insert && (len(args) < 4 || len(args) > 5 || (len(args) == 5 && args[4] != "write-protected"))) || (!insert && len(args) != 2) {
		return fail(errors.New("usage: fogcast insert-floppy <A|B> <game-id> <base-media-id> [write-protected] | eject-floppy <A|B>"))
	}
	client := hostclient.NewClient(origin, hostJSONClient())
	prior, err := client.Session(ctx)
	if err != nil {
		return fail(err)
	}
	var after hostclient.SessionResult
	if insert {
		after, err = client.InsertPcFloppyForSession(ctx, prior, unit, args[2], args[3], len(args) == 5)
	} else {
		after, err = client.EjectPcFloppyForSession(ctx, prior, unit)
	}
	if err != nil {
		return fail(err)
	}
	return commandResult{jsonValue: after, human: func(w io.Writer) error {
		return writeHumanStatusResult(w, statusResult{State: protocol.State(after.State)})
	}}
}
