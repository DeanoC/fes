package kitlauncher

import (
	"context"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/theme"
	"log"
	"strconv"
	"strings"
	"time"
)

type Pad interface {
	Poll() ([]remoteinput.Event, error)
	Close() error
}
type observation struct {
	epoch          uint64
	session        Session
	health         hostclient.HealthResult
	games          []hostclient.Game
	strip          []hostclient.Game
	stripLabel     string
	recents        []hostclient.Game
	haveStrip      bool
	attract        hostclient.AttractPlaylist
	haveAttract    bool
	hydrateAttract bool
	coreStatuses   []hostclient.CoreAvailability
	haveCoreStatus bool
	coreStatusErr  bool
	detailID       string
	presentation   hostclient.Presentation
	haveDetail     bool
	cache          hostclient.LibraryCache
	haveCache      bool
	err            error
	mutation       bool
	message        string
	hostAbsent     bool
}

const hostUnavailableMessage = "Host unavailable"

// Resolve feedback from the submitted identity, never from later UI focus.
func sessionActionLabel(m Model, action, id string) string {
	verb := "Launch"
	if action == "stop" {
		verb = "Stop"
	}
	title := id
	for _, pool := range [][]hostclient.Game{m.Catalog, m.Games, m.Strip} {
		for _, game := range pool {
			if game.ID == id && strings.TrimSpace(game.Title) != "" {
				title = game.Title
				return verb + " " + boundedSessionText(title, 40)
			}
		}
	}
	return strings.TrimSpace(verb + " " + boundedSessionText(title, 40))
}

func sessionOperationMessage(result hostclient.SessionResult, err error) string {
	code := boundedSessionCode(result.ErrorCode)
	message := boundedSessionText(result.ErrorMessage, 120)
	// The footer may clip: show the actionable explanation before context.
	// The machine-readable code is retained in the bounded result log.
	if message != "" {
		return message
	}
	if code != "" {
		return code
	}
	if err != nil && result.HTTPStatus == 0 {
		return hostUnavailableMessage
	}
	if err != nil || result.HTTPStatus >= 400 {
		return "Operation failed"
	}
	return ""
}

func boundedSessionCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var out strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			out.WriteRune(r)
		}
		if out.Len() >= 64 {
			break
		}
	}
	return out.String()
}

func boundedSessionText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var out strings.Builder
	for _, r := range value {
		switch r {
		case '\n', '\r', '\t':
			out.WriteByte(' ')
		default:
			if r >= 0x20 && r != 0x7f {
				out.WriteRune(r)
			}
		}
		if out.Len() >= limit {
			break
		}
	}
	return strings.TrimSpace(out.String())
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
	m.Cache = mergeCacheStatus(c.Cache, hostclient.LibraryCache{}, false)
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
			}
			if o.err == nil && !o.hostAbsent {
				o.health, o.err = c.Library.Health(ctx)
			}
			if o.err == nil && !o.hostAbsent && load {
				o.games, o.err = loadCatalog(ctx, c)
				if o.err == nil {
					if library, coreErr := c.Library.CoreLibrary(ctx); coreErr == nil {
						o.coreStatuses = library.Availability()
						o.haveCoreStatus = true
					} else {
						o.coreStatusErr = true
					}
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
		id := m.Session.GameID
		if action == "launch" {
			id = m.consumeLaunchID()
			if id == "" {
				m.Busy = false
				return
			}
		}
		label := sessionActionLabel(m, action, id)
		closeInput()
		if m.AttractActive {
			m.hideAttract()
		}
		m.Message = "Loading game"
		if action == "launch" {
			// Publish feedback while Menu still owns the display, before the
			// asynchronous request can hand HDMI to the game.
			present(m)
		} else {
			m.Message = "Stopping game"
			present(m)
		}
		// Do not log credentials, raw transport errors, or response bodies.
		log.Printf("kit session dispatch epoch=%d action=%s game_id=%q", e, action, boundedSessionText(id, 160))
		go func() {
			o := observation{epoch: e, mutation: true}
			var r hostclient.SessionResult
			var err error
			if action == "launch" {
				r, err = c.Library.Launch(ctx, id)
			} else {
				r, err = c.Library.Stop(ctx)
			}
			log.Printf("kit session result epoch=%d action=%s game_id=%q http_status=%d code=%q request_error=%t",
				e, action, boundedSessionText(id, 160), r.HTTPStatus, boundedSessionCode(r.ErrorCode), err != nil)
			if err != nil {
				o.message = sessionOperationMessage(r, err)
				o.hostAbsent = r.HTTPStatus == 0
			} else if r.ErrorCode != "" || r.HTTPStatus >= 400 {
				o.message = sessionOperationMessage(r, nil)
			} else {
				o.session, o.err = c.Session(ctx)
			}
			if o.message != "" {
				o.message += " (" + label + ")"
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
				m.ClearCoreStatuses(true)
				if !m.Busy {
					m.Message = OfflineMessage
				}
				closeInput()
				continue
			}
			if o.hostAbsent {
				m.Connected = false
				m.ClearCoreStatuses(true)
				if o.session.State != "" {
					m.Session = o.session
				}
				if !m.Busy && o.session.State != "active" && o.session.State != "failed" {
					if m.Message != hostUnavailableMessage && !strings.HasPrefix(m.Message, hostUnavailableMessage+" (") {
						m.Message = OfflineMessage
					}
				}
				if stream != nil {
					closeInput()
				}
				continue
			}
			m.Connected = true
			m.ForeignLease = kitlease.ForeignHID(kitlease.Status{
				State: o.health.Connection.State,
				Owner: o.health.Connection.Owner,
			})
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
				if o.haveCoreStatus {
					m.ApplyCoreStatuses(o.coreStatuses)
				} else if o.coreStatusErr {
					m.ClearCoreStatuses(true)
				}
				if o.haveStrip {
					m.ApplyStrip(o.strip, o.stripLabel)
					m.Recents = append([]hostclient.Game(nil), o.recents...)
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
							if encoded, ok := encodePlayHIDEvent(m.Session, e); ok {
								select {
								case <-stream.Ready:
									stream.Send(encoded)
								default:
								}
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

func encodePlayHIDEvent(session Session, e remoteinput.Event) (remoteinput.Event, bool) {
	if e.Player > 1 {
		return remoteinput.Event{}, false
	}
	// Legacy cores retain their single merged pad, including a surviving second
	// physical controller. Only the negotiated ports contract preserves identity.
	if !session.CorePackage.HasControllerPorts() {
		e.Player = 0
	}
	return playhid.StreamEvent(e, session.CorePackage.HasKeyboard())
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

func loadCatalog(ctx context.Context, c *Client) ([]hostclient.Game, error) {
	var games []hostclient.Game
	for _, system := range catalogSystems(ctx, c) {
		page, err := c.Library.FetchLibrary(ctx, hostclient.GameListQuery{Platform: system}, 10000)
		if err != nil {
			return nil, err
		}
		games = append(games, page...)
	}
	if games == nil {
		games = []hostclient.Game{}
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

func loadStrip(ctx context.Context, c *Client) ([]hostclient.Game, string, []hostclient.Game) {
	if c == nil || c.Library == nil {
		return nil, "", nil
	}
	recents, err := c.Library.FetchLibrary(ctx, hostclient.GameListQuery{Collection: "recents", Limit: stripMax}, stripMax)
	if err != nil {
		recents = nil
	}
	favorites, err := c.Library.FetchLibrary(ctx, hostclient.GameListQuery{Collection: "favorites", Limit: stripMax}, stripMax)
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
