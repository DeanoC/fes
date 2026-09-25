package kitlauncher

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/theme"
	"log"
	"strings"
	"sync/atomic"
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
	m := Model{
		Message: connectingMessage, Shelf: normalizeShelf(c.config.Shelf), Pack: theme.NormalizePack(c.config.Theme), WheelOpen: true,
		Session: Session{HPSFramebuffer: c.config.HPSFramebuffer},
	}
	loggedSplash := false
	paintKitHDMI := func(m Model) {
		if ShouldPaintHDMI(m) {
			if present != nil {
				present(m)
			}
			return
		}
		// Log once when idle is confirmed and the recipe has no HPS framebuffer.
		// Do not blank-and-fail: the service keeps running and splash stays up.
		if loggedSplash || m.Busy || m.Session.HPSFramebuffer || m.Session.State != "idle" {
			return
		}
		loggedSplash = true
		log.Printf("kit hdmi: confirmed idle without HPS framebuffer (no 0x002f); leaving splash visible")
	}
	var pad Pad
	var feed *localFeed
	var coreBound atomic.Bool
	inputDown := false
	inputLogged := false
	closeFeed := func(reset bool) {
		if feed != nil {
			feed.Close()
			feed = nil
		}
		if reset {
			m.ResetControls()
		}
	}
	defer func() {
		closeFeed(false)
		if pad != nil {
			_ = pad.Close()
		}
	}()
	// A probe error keeps the last answer so a stalled status read does not
	// release held buttons. The first call is synchronous, just before the
	// loop; later calls stay off this loop and do not wait on the host.
	refreshCore := func() {
		next, err := c.readLocalCore(ctx)
		if err != nil {
			return
		}
		coreBound.Store(next)
	}
	showLocalInput := func() {
		if inputDown && coreBound.Load() && !m.Busy {
			m.Message = localInputUnavailableMessage
		}
	}
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
	paintKitHDMI(m)
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
		closeFeed(true)
		if m.AttractActive {
			m.hideAttract()
		}
		// Loading and stopping copy is a temporary overlay, and only when this
		// idle still enables the HPS framebuffer. Splash has no linuxfb picture.
		m.Message = "Loading game"
		if action != "launch" {
			m.Message = "Stopping game"
		}
		paintKitHDMI(m)
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
	// Sample after the first paint so a slow runtime status cannot hide the
	// shell. The loop still does not start until this sample returns, so the
	// first pad poll already knows whether a core is bound.
	refreshCore()
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				refreshCore()
			}
		}
	}()
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
				showLocalInput()
				continue
			}
			if o.hostAbsent {
				m.Connected = false
				m.ClearCoreStatuses(true)
				if o.session.State != "" {
					m.Session = applyObservedSession(m.Session, o.session)
				}
				if !m.Busy && o.session.State != "active" && o.session.State != "failed" {
					if m.Message != hostUnavailableMessage && !strings.HasPrefix(m.Message, hostUnavailableMessage+" (") && m.Message != localInputUnavailableMessage {
						m.Message = OfflineMessage
					}
				}
				showLocalInput()
				continue
			}
			m.Connected = true
			// ForeignLease still marks a grant this host does not own. Play
			// input to the local socket does not consult it.
			m.ForeignLease = kitlease.ForeignHID(kitlease.Status{
				State: o.health.Connection.State,
				Owner: o.health.Connection.Owner,
			})
			m.Session = applyObservedSession(m.Session, o.session)
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
				showLocalInput()
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
			bound := coreBound.Load()
			if !bound {
				if feed != nil {
					closeFeed(true)
				}
				if inputDown {
					inputDown = false
					inputLogged = false
					if m.Message == localInputUnavailableMessage {
						m.Message = ""
					}
				}
			}
			if pad != nil {
				events, err := pad.Poll()
				if err != nil {
					_ = pad.Close()
					pad = nil
					m.ControllerConnected = false
					closeFeed(true)
					nextPad = now.Add(time.Second)
				} else {
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
						// Play input follows local core presence. It does not
						// wait for the host, input.ready, or the kit lease.
						if bound {
							if feed == nil {
								feed = newLocalFeed(c.localInputSocket())
							}
							if err := feed.send(e, now); err != nil {
								if errors.Is(err, errLocalInputUnavailable) {
									if !inputDown {
										log.Printf("kit local input unavailable: %v", err)
									}
									inputDown = true
								} else if !inputLogged {
									inputLogged = true
									log.Printf("kit local input dropped a frame: %v", err)
								}
							} else if inputDown {
								inputDown = false
								inputLogged = false
								if m.Message == localInputUnavailableMessage {
									m.Message = ""
								}
							}
						}
						if action != "" {
							mutate(action)
						}
					}
					showLocalInput()
				}
			}
			if action := m.Tick(now); action != "" {
				mutate(action)
			}
			// A live game owns HDMI. Confirmed idle without an HPS framebuffer
			// leaves FPGA splash pixels alone instead of painting kit chrome.
			if !m.Busy {
				paintKitHDMI(m)
			}
		}
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
