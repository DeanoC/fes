package targetclient

import (
	"context"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"net/http"
	"time"
)

func (c *Client) InsertLibraryMedia(ctx context.Context, size int64, body io.Reader, b protocol.LibraryMediaBinding) (protocol.Status, error) {
	if !b.Valid() || size != protocol.AtariStFloppyBytes || body == nil {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	s, err := c.mediaUnitRequestBudget(ctx, "/v1/library/media/insert", size, readOnlyReader{body}, b.MediaUnitBinding, &b, 450*time.Second)
	if err != nil {
		return s, err
	}
	u, ok := protocol.MediaUnit(s.CorePackage, b.Unit)
	if !ok || u.State != protocol.MediaUnitReady || (protocol.MediaWriteCapable(s.CorePackage) && (u.Persistence == nil || u.Persistence.GameID != b.GameID || u.Persistence.BaseMediaID != b.BaseMediaID)) {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "library disk response does not match its durable binding", Phase: "recovery"}
	}
	return s, nil
}
func (c *Client) SaveMedia(ctx context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	if !b.Valid() {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	s, err := c.mediaUnitRequestBudget(ctx, "/v1/library/media/save", 0, http.NoBody, b, nil, 150*time.Second)
	if err != nil {
		return s, err
	}
	u, ok := protocol.MediaUnit(s.CorePackage, b.Unit)
	if !ok || u.Persistence == nil {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "disk checkpoint response lost its durable binding", Phase: "recovery"}
	}
	return s, nil
}
