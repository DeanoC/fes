package kitlauncher

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
	"github.com/DeanoC/FogCast/remoteinput"
)

type Pad interface {
	Poll() ([]remoteinput.Event, error)
	Close() error
}
type observation struct {
	epoch          uint64
	session        Session
	health         tenfoot.HealthResult
	games          []tenfoot.Game
	strip          []tenfoot.Game
	stripLabel     string
	recents        []tenfoot.Game
	haveStrip      bool
	attract        tenfoot.AttractPlaylist
	haveAttract    bool
	hydrateAttract bool
	detailID       string
	presentation   tenfoot.Presentation
	haveDetail     bool
	cache          tenfoot.LibraryCache
	haveCache      bool
	err            error
	mutation       bool
	message        string
	hostAbsent     bool
}

// Run keeps device/UI work on one loop. Slow host requests run outside that loop;
// observations started before a mutation cannot undo its resulting state.
func Run(ctx context.Context, c *Client, present func(Model), openPad func() (Pad, error)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := Model{Message: connectingMessage, Shelf: normalizeShelf(c.config.Shelf), Pack: theme.NormalizePack(c.config.Theme), WheelOpen: true}
	var pad Pad
	var stream *InputStream
	streamKey := ""
	closeInput := func() {
		if stream != nil {
			stream.Close()
			stream = nil
			streamKey = ""
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
	attractLoaded := false
	lastAttract := time.Time{}
	if applyLocalSnapshot(&m, c) {
		catalogLoaded = true
		m.Message = OfflineMessage
	}
	m.Cache = mergeCacheStatus(c.Cache, tenfoot.LibraryCache{}, false)
	if present != nil {
		present(m)
	}
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
		loadAttract := !attractLoaded || time.Since(lastAttract) > attractIdleRefresh
		detailID := ""
		if m.AttractActive && !m.DetailOpen {
			if item, ok := m.currentAttractItem(); ok {
				detailID = strings.TrimSpace(item.GameID)
			}
		} else if !m.WheelOpen {
			if game, ok := m.seriesSubject(); ok {
				detailID = game.ID
			}
		}
		go func() {
			o := observation{epoch: e}
			o.session, o.err = c.Session(ctx)
			if o.err != nil {
				o.hostAbsent = true
				o.err = nil
				if c.hostlessHeld() {
					if st, stErr := c.hostless.status(ctx); stErr == nil {
						o.session = sessionFromStatus(st)
					}
				}
			}
			if o.err == nil && !o.hostAbsent {
				o.health, o.err = c.Library.Health(ctx)
			}
			if o.err == nil && !o.hostAbsent && load {
				o.games, o.err = loadCatalog(ctx, c)
				if o.err == nil {
					o.strip, o.stripLabel, o.recents = loadStrip(ctx, c)
					o.haveStrip = true
					persistSnapshot(c, CatalogSnapshot{
						Games:      o.games,
						Strip:      o.strip,
						StripLabel: o.stripLabel,
						Recents:    o.recents,
					})
					if c.Library != nil {
						if cache, err := c.Library.LibraryCache(ctx); err == nil {
							o.cache = cache
							o.haveCache = true
						}
					}
				}
			}
			if o.err == nil && !o.hostAbsent && loadAttract {
				p, err := c.Library.Attract(ctx, defaultAttractLimit)
				if err == nil {
					o.attract = p
					o.haveAttract = true
				} else {
					o.hydrateAttract = true
				}
			}
			if o.err == nil && !o.hostAbsent && detailID != "" {
				p, err := c.Library.GamePresentation(ctx, detailID)
				if err == nil {
					o.detailID = detailID
					o.presentation = p
					o.haveDetail = true
				}
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
		if m.AttractActive {
			m.hideAttract()
		}
		m.Message = "Loading game"
		id := ""
		if action == "launch" {
			id = m.consumeLaunchID()
			if id == "" {
				m.Busy = false
				return
			}
			// Publish feedback while Menu still owns the display, before the
			// asynchronous request can hand HDMI to the game.
			present(m)
		} else {
			m.Message = "Stopping game"
			present(m)
		}
		game, _ := m.lookupGame(id)
		hostless := !m.Connected
		go func() {
			o := observation{epoch: e, mutation: true}
			var r tenfoot.SessionResult
			var err error
			if action == "launch" {
				if hostless {
					o.hostAbsent = true
					resp, launchErr := c.hostless.launch(ctx, game)
					err = launchErr
					if err == nil {
						o.session = sessionFromStatus(resp.Status)
					}
				} else {
					r, err = c.Library.Launch(ctx, id)
				}
			} else if action == "hostless-yield" {
				sess, yieldErr := c.hostless.stopAndRelease(ctx)
				err = yieldErr
				if c.hostlessHeld() {
					o.hostAbsent = true
					if sess.State != "" {
						o.session = sess
					}
				}
			} else if c.hostlessHeld() {
				o.hostAbsent = true
				sess, stopErr := c.hostless.stopAndRelease(ctx)
				err = stopErr
				if sess.State != "" {
					o.session = sess
				}
			} else {
				r, err = c.Library.Stop(ctx)
			}
			if err != nil {
				var refuse hostlessRefuse
				if errors.As(err, &refuse) {
					o.message = refuse.reason
				} else if r.HTTPStatus >= 400 {
					o.message = "Operation failed; retry Stop with Select + Start"
				} else {
					o.message = "Operation failed; retry Stop with Select + Start"
				}
			} else if r.HTTPStatus >= 400 {
				o.message = "Operation failed; retry Stop with Select + Start"
			} else if !o.hostAbsent && (action != "launch" || !hostless) {
				o.session, o.err = c.Session(ctx)
			}
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
					m.Message = OfflineMessage
				}
				closeInput()
				continue
			}
			if o.hostAbsent {
				m.Connected = false
				if o.session.State != "" {
					m.Session = o.session
				}
				if !m.Busy && o.session.State != "active" && o.session.State != "failed" {
					if m.Message != RefuseNeedsHost && m.Message != RefuseKitInUse {
						m.Message = OfflineMessage
					}
				}
				// Keep the Select+Start chord across offline polls so a
				// hostless game can still be stopped.
				if stream != nil {
					closeInput()
				}
				continue
			}
			if c.hostlessHeld() && !o.mutation {
				mutate("hostless-yield")
				continue
			}
			m.Connected = true
			m.ForeignLease = o.health.Connection.State == "busy"
			if m.ForeignLease && stream != nil {
				closeInput()
			}
			if streamKey != "" && streamKey != inputStreamKey(o.session) {
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
				} else if isTransientStatus(m.Message) {
					m.Message = ""
				}
				if o.games != nil {
					m.ApplyCatalog(o.games)
					catalogLoaded = true
					lastCatalog = time.Now()
				}
				if o.haveStrip {
					m.ApplyStrip(o.strip, o.stripLabel)
					m.Recents = append([]tenfoot.Game(nil), o.recents...)
				}
				if o.haveCache || c.Cache != nil {
					m.Cache = mergeCacheStatus(c.Cache, o.cache, o.haveCache)
				}
				if o.haveAttract {
					m.SetAttractPlaylist(o.attract)
					attractLoaded = true
					lastAttract = time.Now()
				} else if o.hydrateAttract {
					m.HydrateAttractIdle()
					attractLoaded = true
					lastAttract = time.Now()
				}
				if o.haveDetail {
					if m.AttractActive && !m.DetailOpen {
						m.ApplyAttractPresentation(o.detailID, o.presentation)
					} else {
						m.ApplyPresentation(o.detailID, o.presentation)
					}
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
					streamKey = ""
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
					key := playHIDStreamKey(m)
					if stream == nil && m.Connected && !m.Busy && !m.ForeignLease && key != "" && now.After(nextPad) {
						streamKey = key
						stream = c.OpenInput(ctx, m.Session.Input.SessionID)
						nextPad = now.Add(time.Second)
					}
					for _, e := range events {
						prevShelf := m.Shelf
						prevPack := m.Pack
						action := m.Input(e, now)
						if m.Shelf != prevShelf {
							persistShelf(c, m.activeShelf())
						}
						if m.Pack != prevPack {
							persistPack(c, m.Pack)
						}
						if stream != nil && !m.ForeignLease && streamKey == playHIDStreamKey(m) {
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

func playHIDStreamKey(m Model) string {
	if m.ForeignLease {
		return ""
	}
	return inputStreamKey(m.Session)
}

func inputStreamKey(session Session) string {
	if session.State != "active" || (!session.Input.Ready && session.Input.State != "reconnecting") || session.Input.SessionID == "" {
		return ""
	}
	switch session.Execution {
	case "fpga_native":
		return session.Execution + ":" + session.Input.SessionID
	case "fpga_development":
		if session.CorePackage == nil || session.CorePackage.Generation == 0 {
			return ""
		}
		if !session.CorePackage.Gamepad && !session.CorePackage.HasKeyboard() {
			return ""
		}
		return session.Execution + ":" + session.Input.SessionID + ":" + strconv.FormatUint(session.CorePackage.Generation, 10)
	default:
		return ""
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

func loadStrip(ctx context.Context, c *Client) ([]tenfoot.Game, string, []tenfoot.Game) {
	if c == nil || c.Library == nil {
		return nil, "", nil
	}
	recents, err := c.Library.FetchLibrary(ctx, tenfoot.GameListQuery{Collection: "recents", Limit: stripMax}, stripMax)
	if err != nil {
		recents = nil
	}
	favorites, err := c.Library.FetchLibrary(ctx, tenfoot.GameListQuery{Collection: "favorites", Limit: stripMax}, stripMax)
	if err != nil {
		favorites = nil
	}
	games, label := ComposeStrip(recents, favorites)
	return games, label, recents
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

func persistPack(c *Client, pack string) {
	if c == nil || c.config.path == "" {
		return
	}
	pack = theme.NormalizePack(pack)
	if pack == "" || theme.NormalizePack(c.config.Theme) == pack {
		return
	}
	cfg := c.config
	cfg.Theme = pack
	if err := SaveConfig(cfg); err == nil {
		c.config.Theme = pack
	}
}
