package hostapi

import (
	"context"

	"github.com/DeanoC/FogCast-POC/internal/mediasession"
)

// NewMediaSessionAdapter exposes a managed media session through the host API
// lifecycle seam without coupling mediasession to the HTTP API package.
func NewMediaSessionAdapter(session *mediasession.Session) MediaSession {
	return mediaSessionAdapter{session: session}
}

type mediaSessionAdapter struct {
	session *mediasession.Session
}

func (a mediaSessionAdapter) Start(ctx context.Context, gameID string) (MediaHandle, error) {
	handle, err := a.session.Start(ctx, gameID)
	if handle == nil {
		return nil, err
	}
	return mediaHandleAdapter{handle: handle}, err
}

type mediaHandleAdapter struct {
	handle mediasession.Handle
}

func (h mediaHandleAdapter) Stop(ctx context.Context) error {
	return h.handle.Stop(ctx)
}

func (h mediaHandleAdapter) Done() <-chan struct{} {
	if terminal, ok := h.handle.(interface{ Done() <-chan struct{} }); ok {
		return terminal.Done()
	}
	return nil
}

var _ MediaSession = mediaSessionAdapter{}
var _ MediaHandle = mediaHandleAdapter{}
