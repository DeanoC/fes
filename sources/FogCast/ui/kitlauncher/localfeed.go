package kitlauncher

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// localInputUnavailableMessage is the footer when play input cannot reach the
// kit-local socket. It is not a host error and it is not cleared by reconnect.
const localInputUnavailableMessage = "Local input unavailable"

// errLocalInputUnavailable means the agent is not listening. Callers must not
// post the same pad to the host.
var errLocalInputUnavailable = errors.New("local input socket is not listening")

// localInputSession satisfies the input-frame encoding, which rejects session
// 0. The kit-local socket does not consult a lease or this value.
const localInputSession = localfeed.Session

const localInputDial = localfeed.Dial

// localInputRetry is how long a missing agent socket stays dark. One pad poll
// emits several events, and each failed dial can spend the full dial budget.
// Further sends wait until this elapses, then try once.
const localInputRetry = localfeed.Retry

// localFeed is one connection to mister-agent's kit-local input socket.
// The frame encoding lives in ui/localfeed, shared with tenfoot.
type localFeed struct {
	inner *localfeed.Feed
}

func newLocalFeed(path string, dial func(string, string, time.Duration) (net.Conn, error)) *localFeed {
	return &localFeed{inner: localfeed.New(path, dial)}
}

func (c *Client) localInputSocket() string {
	if c == nil {
		return ""
	}
	return c.localInputPath
}

func (f *localFeed) Close() {
	if f == nil {
		return
	}
	f.inner.Close()
}

func (f *localFeed) send(e remoteinput.Event, now time.Time) error {
	if f == nil || f.inner == nil {
		return errLocalInputUnavailable
	}
	err := f.inner.Send(e, now)
	if err == nil {
		return nil
	}
	if errors.Is(err, localfeed.ErrUnavailable) {
		return fmt.Errorf("%w: %v", errLocalInputUnavailable, err)
	}
	return err
}

// localInputFrame is the raw event the hub already produced. mister-agent
// applies playhid.StreamEvent from the runtime observation, so the kit keeps
// the player index and does not reshape keyboard frames here.
func localInputFrame(seq uint32, e remoteinput.Event, now time.Time) protocol.InputFrame {
	return localfeed.Frame(seq, e, now)
}
