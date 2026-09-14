package kitlauncher

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/remoteinput"
	"io"
	"net/http"
	"net/url"
	"time"
)

// InputStream owns one bounded source queue. Overflow closes the stream so the
// host neutralizes controls instead of retaining a pressed button indefinitely.
type InputStream struct {
	Ready  chan struct{}
	Done   chan struct{}
	events chan remoteinput.Event
	cancel context.CancelFunc
}

func (s *InputStream) Close() { s.cancel() }
func (s *InputStream) Send(e remoteinput.Event) bool {
	select {
	case <-s.Done:
		return false
	default:
	}
	select {
	case s.events <- e:
		return true
	default:
		s.Close()
		return false
	}
}
func (c *Client) OpenInput(parent context.Context, sessionID string) *InputStream {
	ctx, cancel := context.WithCancel(parent)
	s := &InputStream{Ready: make(chan struct{}), Done: make(chan struct{}), events: make(chan remoteinput.Event, 64), cancel: cancel}
	go func() {
		defer close(s.Done)
		defer cancel()
		reader, writer := io.Pipe()
		defer reader.Close()
		defer writer.Close()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.API+"/api/v1/launcher/input?session_id="+url.QueryEscape(sessionID), reader)
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/x-ndjson")
		client := *c.HTTP
		client.Timeout = 0
		writeDone := make(chan struct{})
		go func() {
			defer close(writeDone)
			defer writer.Close()
			enc := json.NewEncoder(writer)
			tick := time.NewTicker(250 * time.Millisecond)
			defer tick.Stop()
			// Send the first heartbeat immediately so either peer can flush headers.
			if enc.Encode(struct{}{}) != nil {
				return
			}
			for {
				select {
				case <-ctx.Done():
					return
				case e := <-s.events:
					if enc.Encode(struct {
						Event remoteinput.Event `json:"event"`
					}{e}) != nil {
						return
					}
				case <-tick.C:
					if enc.Encode(struct{}{}) != nil {
						return
					}
				}
			}
		}()
		res, err := client.Do(req)
		if err == nil {
			if res.StatusCode == http.StatusOK {
				close(s.Ready)
				_, _ = io.Copy(io.Discard, res.Body)
			}
			_ = res.Body.Close()
		}
		cancel()
		_ = reader.Close()
		_ = writer.Close()
		<-writeDone
	}()
	return s
}
