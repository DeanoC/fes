package fogcastcli

import (
	"context"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"io"
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
