package kitlauncher

import (
	"context"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/remoteinput"
)

type Pad interface {
	Poll() ([]remoteinput.Event, error)
	Close() error
}
type observation struct {
	epoch    uint64
	session  Session
	health   tenfoot.HealthResult
	games    []tenfoot.Game
	err      error
	mutation bool
	message  string
}

// Run keeps device/UI work on one loop. Slow host requests run outside that loop;
// observations started before a mutation cannot undo its resulting state.
func Run(ctx context.Context, c *Client, present func(Model), openPad func() (Pad, error)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := Model{Message: "Connecting to FogCast", Shelf: normalizeShelf(c.config.Shelf)}
	var pad Pad
	var stream *InputStream
	streamID := ""
	closeInput := func() {
		if stream != nil {
			stream.Close()
			stream = nil
			streamID = ""
		}
		m.ResetControls()
	}
	defer func() {
		closeInput()
		if pad != nil {
			_ = pad.Close()
		}
	}()
	results := make(chan observation, 4)
	var epoch uint64
	polling := false
	catalogLoaded := false
	lastCatalog := time.Time{}
	send := func(o observation) {
		select {
		case results <- o:
		case <-ctx.Done():
		}
	}
	poll := func() {
		if polling || m.Busy {
			return
		}
		polling = true
		e := epoch
		load := !catalogLoaded || time.Since(lastCatalog) > 30*time.Second
		go func() {
			o := observation{epoch: e}
			o.session, o.err = c.Session(ctx)
			if o.err == nil {
				o.health, o.err = c.Library.Health(ctx)
			}
			if o.err == nil && load {
				o.games, o.err = loadCatalog(ctx, c)
			}
			send(o)
		}()
	}
	mutate := func(action string) {
		if m.Busy {
			return
		}
		epoch++
		e := epoch
		m.Busy = true
		closeInput()
		m.Message = "Loading game"
		id := ""
		if action == "launch" {
			if len(m.Games) == 0 {
				m.Busy = false
				return
			}
			id = m.Games[m.Focus].ID
			// Publish feedback while Menu still owns the display, before the
			// asynchronous request can hand HDMI to the game.
			present(m)
		} else {
			m.Message = "Stopping game"
		}
		go func() {
			o := observation{epoch: e, mutation: true}
			var r tenfoot.SessionResult
			var err error
			if action == "launch" {
				r, err = c.Library.Launch(ctx, id)
			} else {
				r, err = c.Library.Stop(ctx)
			}
			if err != nil || r.HTTPStatus >= 400 {
				o.message = "Operation failed; retry Stop with Select + Start"
			} else {
				o.message = ""
			}
			o.session, o.err = c.Session(ctx)
			send(o)
		}()
	}
	tick := time.NewTicker(16 * time.Millisecond)
	defer tick.Stop()
	nextPoll, nextPad := time.Time{}, time.Time{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case o := <-results:
			if !o.mutation {
				polling = false
			}
			if o.epoch != epoch {
				continue
			}
			if o.mutation {
				m.Busy = false
				m.Message = o.message
				nextPoll = time.Time{}
			}
			if o.err != nil {
				m.Connected = false
				if !m.Busy {
					m.Message = "Host unavailable - reconnecting"
				}
				closeInput()
				continue
			}
			m.Connected = true
			if m.Session.Input.SessionID != o.session.Input.SessionID || m.Session.State != o.session.State {
				closeInput()
			}
			m.Session = o.session
			if !o.mutation {
				m.TargetReady = o.health.TargetReady
				if o.health.Connection.State == "busy" {
					m.Message = "Kit in use"
				} else if !o.health.TargetReachable {
					m.Message = "Kit unavailable"
				} else if !m.TargetReady && m.Session.State != "active" {
					m.Message = "Kit not ready"
				} else if m.Message == "Connecting to FogCast" || m.Message == "Host unavailable - reconnecting" || m.Message == "Kit in use" || m.Message == "Kit unavailable" || m.Message == "Kit not ready" {
					m.Message = ""
				}
				if o.games != nil {
					m.SetCatalog(o.games)
					catalogLoaded = true
					lastCatalog = time.Now()
				}
			}
		case now := <-tick.C:
			if now.After(nextPoll) {
				poll()
				nextPoll = now.Add(time.Second)
			}
			if pad == nil && now.After(nextPad) {
				var err error
				pad, err = openPad()
				m.ControllerConnected = err == nil
				nextPad = now.Add(time.Second)
			}
			if stream != nil {
				select {
				case <-stream.Done:
					stream = nil
					streamID = ""
					m.ResetControls()
					nextPad = now.Add(time.Second)
				default:
				}
			}
			if pad != nil {
				events, err := pad.Poll()
				if err != nil {
					_ = pad.Close()
					pad = nil
					m.ControllerConnected = false
					closeInput()
					nextPad = now.Add(time.Second)
				} else {
					if stream == nil && m.Connected && !m.Busy && m.Session.State == "active" && m.Session.Execution == "fpga_native" && m.Session.Input.SessionID != "" && now.After(nextPad) {
						streamID = m.Session.Input.SessionID
						stream = c.OpenInput(ctx, streamID)
						nextPad = now.Add(time.Second)
					}
					for _, e := range events {
						prevShelf := m.Shelf
						action := m.Input(e, now)
						if m.Shelf != prevShelf {
							persistShelf(c, m.activeShelf())
						}
						if stream != nil && streamID == m.Session.Input.SessionID {
							select {
							case <-stream.Ready:
								stream.Send(e)
							default:
							}
						}
						if action != "" {
							mutate(action)
						}
					}
				}
			}
			if action := m.Tick(now); action != "" {
				mutate(action)
			}
			// A live/unknown game owns HDMI; retain the last menu pixels until confirmed idle.
			if !m.Busy && m.Session.State != "active" {
				present(m)
			}
		}
	}
}

func loadCatalog(ctx context.Context, c *Client) ([]tenfoot.Game, error) {
	var games []tenfoot.Game
	for _, system := range catalogSystems(ctx, c) {
		page, err := c.Library.FetchLibrary(ctx, tenfoot.GameListQuery{Platform: system}, 10000)
		if err != nil {
			return nil, err
		}
		games = append(games, page...)
	}
	if games == nil {
		games = []tenfoot.Game{}
	}
	return games, nil
}

func catalogSystems(ctx context.Context, c *Client) []string {
	platforms, err := c.Library.Platforms(ctx)
	if err != nil {
		return append([]string(nil), fallbackCatalogSystems...)
	}
	ids := make([]string, 0, len(platforms))
	seen := map[string]struct{}{}
	for _, p := range platforms {
		id := strings.ToLower(strings.TrimSpace(p.ID))
		if id == "" || p.GameCount <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return append([]string(nil), fallbackCatalogSystems...)
	}
	return ids
}

func persistShelf(c *Client, shelf string) {
	if c == nil {
		return
	}
	shelf = normalizeShelf(shelf)
	if shelf == normalizeShelf(c.config.Shelf) || c.config.path == "" {
		return
	}
	cfg := c.config
	cfg.Shelf = shelf
	if err := SaveConfig(cfg); err == nil {
		c.config.Shelf = shelf
	}
}
