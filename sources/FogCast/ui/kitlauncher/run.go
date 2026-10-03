package kitlauncher

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/theme"
)

type Pad interface {
	Poll() ([]remoteinput.Event, error)
	Close() error
}
type observation struct {
	epoch           uint64
	session         Session
	health          hostclient.HealthResult
	kitLease        kitlease.Status
	games           []hostclient.Game
	strip           []hostclient.Game
	stripLabel      string
	recents         []hostclient.Game
	haveStrip       bool
	attract         hostclient.AttractPlaylist
	haveAttract     bool
	hydrateAttract  bool
	coreStatuses    []hostclient.CoreAvailability
	haveCoreStatus  bool
	coreStatusErr   bool
	detailID        string
	presentation    hostclient.Presentation
	haveDetail      bool
	cache           hostclient.LibraryCache
	haveCache       bool
	err             error
	mutation        bool
	message         string
	hostAbsent      bool
	localAction     string
	localErr        error
	localStatus     localcores.RunStatus
	haveLocalStatus bool
}

const hostUnavailableMessage = "Host unavailable"
const localLoadTimeout = 60 * time.Second // matches the runtime's bounded core-load operation
const launchTimeoutGrace = 5 * time.Second

// launcherNow keeps launch deadlines deterministic in state-machine tests.
var launcherNow = time.Now
var launcherStatusInterval = time.Second
var launcherPollInterval = time.Second

// launcherObserve exposes post-transition snapshots to host-only Run tests.
// It is nil in the shipped launcher and must be restored by tests.
var launcherObserve func(Model)

func localLaunchFailure(title, reason string, elapsed time.Duration) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "game"
	}
	if elapsed >= localLoadTimeout {
		return title + " took too long to start"
	}
	if reason == "" {
		reason = "the core did not become ready"
	}
	return "Couldn't start " + title + ": " + reason
}

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
	if c.menuDisplay && (c.menuPause == nil || c.menuResume == nil) {
		return errors.New("menu display handoff unavailable")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := Model{
		Message: connectingMessage, Shelf: normalizeShelf(c.config.Shelf), Pack: theme.NormalizePack(c.config.Theme), WheelOpen: true,
		Session: Session{HPSFramebuffer: c.config.HPSFramebuffer},
	}
	m.LocalPlayEnabled = c.LocalCores != nil
	localRunning := false
	localPending := false
	localStopRequested := false
	localSawLaunching := false
	localRequestReturned := false
	hostTimedOut := false
	localTimedOut := false
	menuPaused := false
	resumeMenu := func() {
		if c.menuDisplay && menuPaused {
			if launcherObserve != nil {
				launcherObserve(m)
			}
			c.menuResume()
			menuPaused = false
		}
	}
	localStatusBusy := false
	var localStatusNext time.Time
	loggedSplash := false
	paintKitHDMI := func(m Model) {
		if launcherObserve != nil {
			launcherObserve(m)
		}
		m.LoadNow = time.Now()
		if m.Session.State == "launching" && !m.LoadStarted.IsZero() && !m.HideLoadElapsed {
			m.LoadElapsed = formatLoadElapsed(m.LoadNow.Sub(m.LoadStarted))
		}
		if c.menuDisplay {
			if !menuPaused && (!m.Busy || m.Session.State == "launching") && m.Session.State != "active" && present != nil {
				present(m)
			}
			return
		}
		if ShouldPaintHDMI(m) {
			if present != nil {
				present(m)
			}
			return
		}
		// Log once when idle is confirmed and the recipe has no HPS framebuffer.
		// Do not blank-and-fail: the service keeps running and splash stays up.
		if loggedSplash || c.menuDisplay || m.Busy || m.Session.HPSFramebuffer || m.Session.State != "idle" {
			return
		}
		loggedSplash = true
		log.Printf("kit hdmi: confirmed idle without HPS framebuffer (no 0x002f); leaving splash visible")
	}
	var pad Pad
	var feed *localFeed
	var coreBound localCorePresence
	inputDown := false
	inputLogged := false
	var nextLocalDial time.Time
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
	// A probe error keeps the last answer for one second so a brief stalled
	// status read does not release held buttons. The first call is synchronous,
	// just before the loop; later calls stay off the loop and do not wait on the host.
	refreshCore := func() {
		next, err := c.readLocalCore(ctx)
		coreBound.observe(next, err, time.Now())
	}
	showLocalInput := func() {
		if inputDown && coreBound.bound(time.Now()) && !m.Busy {
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
	localGames, localPath, closeLocal := bootLocalCatalog(ctx, c)
	matcher := &localROMMatcher{}
	if closeLocal != nil {
		defer closeLocal()
	}
	localApplied := false
	send := func(o observation) {
		select {
		case results <- o:
		case <-ctx.Done():
		}
	}
	poll := func() {
		if polling || (m.Busy && m.Session.State != "launching") {
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
			if o.err == nil && !o.hostAbsent && strings.TrimSpace(c.config.TargetID) != "" {
				o.kitLease, o.err = c.PairedKitLease(ctx)
				if errors.Is(o.err, errPairedKitLeaseUnsupported) {
					// A missing projection leaves this kit's lease unknown.
					o.kitLease = kitlease.Status{}
					o.err = nil
					o.health.TargetReachable = false
					o.health.TargetReady = false
				} else if o.err == nil {
					o.health.TargetReachable = o.kitLease.TargetReachable
					o.health.TargetReady = o.kitLease.TargetReady
				}
			} else if o.err == nil && !o.hostAbsent {
				// Host-mode clients have no paired target identity; preserve the
				// selected-target health lease behavior for that mode.
				o.kitLease = kitlease.Status{State: o.health.Connection.State, Owner: o.health.Connection.Owner}
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
		// A timed-out local request still owns an unresolved runtime launch.
		// Keep its epoch and identity until status reaches running or idle.
		if (action == "launch" || action == "local-launch") && (localPending || localTimedOut) {
			m.Message = "Still checking whether the previous game started"
			paintKitHDMI(m)
			return
		}
		if m.Busy && action != "stop" {
			return
		}
		if action == "stop" && localPending && !localRunning {
			// The local runtime cannot interrupt FPGA programming before it
			// publishes a running lease. Remember the chord and reconcile status;
			// stop only after running is observed.
			localStopRequested = true
			m.Message = "Will stop when the core is ready"
			paintKitHDMI(m)
			return
		}
		epoch++
		e := epoch
		m.Busy = true
		local := action == "local-launch" || (action == "stop" && (localRunning || localPending))
		id := m.Session.GameID
		if action == "launch" || action == "local-launch" {
			id = m.consumeLaunchID()
			if id == "" {
				m.Busy = false
				return
			}
			if action == "local-launch" {
				m.Session.GameID = id
			}
			m.Session.State = "launching"
			m.LoadStarted = launcherNow()
			m.LaunchFailed = false
			hostTimedOut = false
			localTimedOut = false
			if action == "local-launch" {
				localSawLaunching = false
				localRequestReturned = false
			}
			m.LoadPhase = "Launching"
			m.Message = "Loading game"
			m.LoadPhase = "Loading " + strings.TrimPrefix(sessionActionLabel(m, action, id), "Launch ")
		}
		label := sessionActionLabel(m, action, id)
		closeFeed(true)
		if m.AttractActive {
			m.hideAttract()
		}
		if c.menuDisplay {
			if action == "launch" || action == "local-launch" {
				paintKitHDMI(m)
			}
			menuPaused = true
			pauseCtx, pauseCancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.menuPause(pauseCtx)
			pauseCancel()
			if err != nil {
				m.Busy = false
				m.Session.State = "idle"
				m.LoadStarted = time.Time{}
				m.LoadPhase = ""
				m.Message = "Could not pause the menu. Please try again"
				resumeMenu()
				paintKitHDMI(m)
				return
			}
		}
		// Loading and stopping copy is a temporary overlay, and only when this
		// idle still enables the HPS framebuffer. Splash has no linuxfb picture.
		if action != "launch" && action != "local-launch" {
			m.Message = "Loading game"
		}
		if action == "stop" {
			m.Message = "Stopping game"
		}
		// The launch state owns input; release Busy so Select+Start can be
		// armed and the asynchronous status probes can update the overlay.
		paintKitHDMI(m)
		// Do not log credentials, raw transport errors, or response bodies.
		if local {
			log.Printf("kit local dispatch epoch=%d action=%s game_id=%q", e, action, boundedSessionText(id, 160))
		} else {
			log.Printf("kit session dispatch epoch=%d action=%s game_id=%q", e, action, boundedSessionText(id, 160))
		}
		if local {
			localPending = true
			go func() {
				o := observation{epoch: e, mutation: true, localAction: action}
				if action == "local-launch" {
					var fetch func(context.Context, string) (string, error)
					if c.Library != nil {
						fetch = c.Library.GameROMHash
					}
					o.localErr = launchMatchedLocalGame(ctx, c.LocalCores, fetch, localPath, localGames, matcher, id)
					if o.localErr != nil {
						log.Printf("kit local cartridge launch failed game_id=%q", boundedSessionText(id, 160))
					}
				} else {
					o.localErr = dispatchLocalAction(ctx, c.LocalCores, localPath, localGames, action, id)
				}
				if o.localErr != nil {
					o.message = localCoreMessage(o.localErr)
					if action == "local-launch" {
						// The agent may have completed a load after our request timed out.
						if status, err := c.LocalCores.Status(ctx); err == nil {
							o.localStatus, o.haveLocalStatus = status, true
						}
					}
				}
				log.Printf("kit local result epoch=%d action=%s game_id=%q error=%t", e, action, boundedSessionText(id, 160), o.localErr != nil)
				send(o)
			}()
			return
		}
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
	if m.LocalPlayEnabled {
		go func(e uint64) {
			status, err := c.LocalCores.Status(ctx)
			send(observation{epoch: e, localAction: "recover", localErr: err, localStatus: status})
		}(epoch)
	}
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
	localAdoptBusy := false
	var localAdoptNext time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case o := <-results:
			if o.localAction == "recover" {
				localAdoptBusy = false
				// Adopt only while the core is still observed bound (#474's one-second
				// probe cache); a recovery answer that arrives after the probe
				// expired must not resurrect a run the shell has already left.
				if o.epoch == epoch && !localPending && !localRunning && o.localErr == nil && localRunInProgress(o.localStatus) && coreBound.bound(time.Now()) {
					if c.menuDisplay {
						if !menuPaused {
							pauseCtx, pauseCancel := context.WithTimeout(ctx, 5*time.Second)
							pauseErr := c.menuPause(pauseCtx)
							pauseCancel()
							if pauseErr != nil {
								// Leave the run unadopted; the next adoption poll retries.
								continue
							}
						}
						menuPaused = true
					}
					localRunning = true
					m.Session.State = "active"
				}
				continue
			}
			if o.localAction == "status" {
				localStatusBusy = false
				if o.epoch == epoch && o.localErr == nil && o.localStatus.Phase == "launching" && (localPending || m.Session.State == "launching") {
					localSawLaunching = true
					m.LoadPhase = "Launching"
				} else if o.epoch == epoch && o.localErr == nil && o.localStatus.Phase == "running" && (localPending || localTimedOut || m.Session.State == "launching") {
					wasTimedOut := localTimedOut
					if wasTimedOut && c.menuDisplay {
						pauseCtx, pauseCancel := context.WithTimeout(ctx, 5*time.Second)
						pauseErr := c.menuPause(pauseCtx)
						pauseCancel()
						if pauseErr != nil {
							// Keep the pending identity and retry status/pause later.
							continue
						}
						menuPaused = true
					}
					localTimedOut = false
					localPending = false
					localRunning = true
					m.Busy = false
					m.Session.State = "active"
					m.LoadStarted = time.Time{}
					m.LoadPhase = ""
					m.Message = ""
					if localStopRequested {
						localStopRequested = false
						mutate("stop")
					}
					if launcherObserve != nil {
						launcherObserve(m)
					}
				} else if o.epoch == epoch && o.localErr == nil && o.localStatus.Phase == "idle" && (localPending || localTimedOut || m.Session.State == "launching") && (localSawLaunching || localRequestReturned) {
					elapsed := launcherNow().Sub(m.LoadStarted)
					localPending = false
					localStopRequested = false
					m.Busy = false
					m.Session.State = "idle"
					m.LoadStarted = time.Time{}
					m.LoadPhase = ""
					if !localTimedOut {
						m.Message = localLaunchFailure(m.SessionTitle(), "launch ended before the core was ready", elapsed)
					}
					localTimedOut = false
					if c.menuDisplay {
						resumeMenu()
					}
				} else if o.epoch == epoch && localRunning && o.localErr == nil && o.localStatus.Phase == "idle" {
					localRunning = false
					m.Session.State = "idle"
					m.Session.GameID = ""
					m.LoadStarted = time.Time{}
					m.LoadPhase = ""
					m.Message = ""
					if c.menuDisplay {
						resumeMenu()
					}
				}
				continue
			}
			if !o.mutation {
				polling = false
			}
			if o.epoch != epoch {
				continue
			}
			if o.mutation {
				if o.localAction != "" {
					localPending = false
					if o.localAction == "local-launch" {
						localRequestReturned = true
					}
					if o.localAction == "local-launch" && o.localErr == nil {
						// GET /v1/local/status, not request completion, owns
						// the transition to running.
						localPending = true
					} else if o.localAction == "local-launch" && o.localErr != nil && (!o.haveLocalStatus || localRunInProgress(o.localStatus)) {
						// The load may still be in flight after an ambiguous reply.
						localPending = true
						m.Busy = false
						o.message = ""
					} else if o.localAction == "stop" && (o.localErr == nil || errors.Is(o.localErr, localcores.ErrInUse)) || o.localAction == "local-launch" && o.localErr != nil {
						elapsed := launcherNow().Sub(m.LoadStarted)
						localTimedOut = false
						localRunning = false
						localPending = false
						m.Session.State = "idle"
						m.LoadStarted = time.Time{}
						m.LoadPhase = ""
						if o.localAction == "local-launch" {
							o.message = localLaunchFailure(m.SessionTitle(), o.message, elapsed)
						} else {
							m.Session.GameID = ""
						}
					}
				}
				m.Busy = false
				if o.localAction == "" && o.session.State == "active" {
					o.message = ""
				} else if o.localAction == "" && o.message != "" && m.Session.State == "launching" {
					m.Session.State = "idle"
					m.LoadStarted = time.Time{}
					m.LoadPhase = ""
				}
				m.Message = o.message
				if c.menuDisplay && !localRunning && !localPending && m.Session.State != "launching" {
					resumeMenu()
				}
				nextPoll = time.Time{}
				if o.localAction != "" {
					continue
				}
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
				if !localApplied && len(localGames) > 0 {
					m.SetCatalog(localGames)
					localApplied = true
					catalogLoaded = true
				}
				if o.session.State != "" && !localRunning && !localPending {
					m.Session = applyObservedSession(m.Session, o.session)
				}
				if !m.Busy && !localRunning && o.session.State != "active" && o.session.State != "failed" && (m.Message == "" || m.Message == connectingMessage || m.Message == OfflineMessage) {
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
				State:   o.kitLease.State,
				Owner:   o.kitLease.Owner,
				Purpose: o.kitLease.Purpose,
			})
			if !localRunning && !localPending {
				wasLoading := m.Session.State == "launching"
				if o.session.State == "active" && c.menuDisplay && !menuPaused {
					pauseCtx, pauseCancel := context.WithTimeout(ctx, 5*time.Second)
					pauseErr := c.menuPause(pauseCtx)
					pauseCancel()
					if pauseErr != nil {
						// Do not expose an active game while menu scanout may continue.
						m.Session.State = "idle"
						m.Busy = false
						continue
					}
					menuPaused = true
				}
				if hostTimedOut && o.session.State == "launching" {
					// Continue reconciling quietly after visible timeout.
				} else {
					m.Session = applyObservedSession(m.Session, o.session)
				}
				if m.Session.State == "active" {
					hostTimedOut = false
					m.Busy = false
					m.LoadStarted = time.Time{}
					m.LoadPhase = ""
					m.LaunchFailed = false
					m.Message = ""
					if launcherObserve != nil {
						launcherObserve(m)
					}
				} else if m.Session.State == "launching" {
					if m.LoadStarted.IsZero() {
						m.LoadStarted = launcherNow()
					}
					m.LoadPhase = m.Session.Progress
				}
				if m.Session.State == "failed" {
					if wasLoading {
						m.LaunchFailed = true
					}
					hostTimedOut = false
					m.Busy = false
					m.LoadStarted = time.Time{}
					m.LoadPhase = ""
					m.Message = "Couldn't start " + m.SessionTitle()
					resumeMenu()
				}
			}
			if !o.mutation {
				m.TargetReady = o.health.TargetReady
				if kitlease.ForeignHID(o.kitLease) {
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
			if m.Session.State == "launching" && !m.LoadStarted.IsZero() && launcherNow().Sub(m.LoadStarted) >= localLoadTimeout+launchTimeoutGrace {
				if localPending {
					localTimedOut = true
				} else {
					hostTimedOut = true
				}
				m.Session.State = "idle"
				m.Busy = false
				m.Message = m.SessionTitle() + " took too long to start"
				m.LoadPhase = ""
				m.LoadStarted = time.Time{}
				resumeMenu()
			}
			if (localRunning || localPending || localTimedOut) && !localStatusBusy && now.After(localStatusNext) {
				localStatusNext = now.Add(launcherStatusInterval)
				localStatusBusy = true
				go func(e uint64) {
					status, err := c.LocalCores.Status(ctx)
					send(observation{epoch: e, localAction: "status", localErr: err, localStatus: status})
				}(epoch)
			}
			if now.After(nextPoll) {
				poll()
				nextPoll = now.Add(launcherPollInterval)
			}
			if pad == nil && now.After(nextPad) {
				var err error
				pad, err = openPad()
				m.ControllerConnected = err == nil
				nextPad = now.Add(time.Second)
			}
			bound := coreBound.bound(now)
			// A kit-local run started outside this shell (for example over the
			// local control socket, or by a shell that restarted after its own
			// startup check) binds the core without this shell knowing. Adopt
			// it so Select+Start can stop it and the menu resumes afterwards.
			if bound && m.LocalPlayEnabled && !localRunning && !localPending && !localAdoptBusy && !m.Busy && now.After(localAdoptNext) {
				localAdoptNext = now.Add(time.Second)
				localAdoptBusy = true
				go func(e uint64) {
					status, err := c.LocalCores.Status(ctx)
					send(observation{epoch: e, localAction: "recover", localErr: err, localStatus: status})
				}(epoch)
			}
			if !bound {
				if feed != nil {
					closeFeed(true)
				}
				if inputDown {
					inputDown = false
					inputLogged = false
					nextLocalDial = time.Time{}
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
						var action string
						if bound {
							// A bound core owns the pad, including when this
							// host session is still idle. Play frames go to
							// the local feed. Select+Start still arms Stop.
							// Browse, the platform wheel, and launch stay put.
							m.armStopChord(e, now)
						} else {
							prevShelf := m.Shelf
							prevPack := m.Pack
							action = m.Input(e, now)
							if m.Shelf != prevShelf {
								persistShelf(c, m.activeShelf())
							}
							if m.Pack != prevPack {
								persistPack(c, m.Pack)
							}
						}
						// Play input follows local core presence. It does not
						// wait for the host, input.ready, or the kit lease.
						if bound {
							if feed == nil {
								feed = newLocalFeed(c.localInputSocket(), c.localDial)
							}
							// Skip the dial while the socket is already down.
							// The Stop chord above still runs.
							// The next try is one send after the cooldown.
							if !(inputDown && now.Before(nextLocalDial)) {
								if err := feed.send(e, now); err != nil {
									if errors.Is(err, errLocalInputUnavailable) {
										if !inputDown {
											log.Printf("kit local input unavailable: %v", err)
										}
										inputDown = true
										nextLocalDial = now.Add(localInputRetry)
									} else if !inputLogged {
										inputLogged = true
										log.Printf("kit local input dropped a frame: %v", err)
									}
								} else if inputDown {
									inputDown = false
									inputLogged = false
									nextLocalDial = time.Time{}
									if m.Message == localInputUnavailableMessage {
										m.Message = ""
									}
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
			if !m.Busy || m.Session.State == "launching" {
				paintKitHDMI(m)
			}
		}
	}
}

func localRunInProgress(status localcores.RunStatus) bool {
	return status.Phase == "launching" || status.Phase == "running"
}

// bootLocalCatalog scans the configured library when the kit has a catalog
// file. The remote launcher API is not required. A missing file or a failed
// boot leaves the disk snapshot in place.
func bootLocalCatalog(ctx context.Context, c *Client) ([]hostclient.Game, func(context.Context, string) (string, error), func() error) {
	if c == nil || strings.TrimSpace(c.catalogConfig) == "" || ctx.Err() != nil {
		return nil, nil, nil
	}
	paths, err := fogcast.PathsForConfig(c.catalogConfig)
	if err != nil {
		return nil, nil, nil
	}
	served, err := hostapi.ServeLocal(ctx, paths)
	if err != nil {
		return nil, nil, nil
	}
	games, err := listLocalCatalog(ctx, served.Base)
	if err != nil {
		_ = served.Close()
		return nil, nil, nil
	}
	return games, served.ContentPath, served.Close
}

func launchLocalGame(ctx context.Context, client LocalCoreClient, resolve func(context.Context, string) (string, error), games []hostclient.Game, id string) error {
	if client == nil || resolve == nil {
		return localcores.ErrUnavailable
	}
	var game hostclient.Game
	for _, row := range games {
		if row.ID == id {
			game = row
			break
		}
	}
	if !game.LocalCatalogPlayable() {
		return localcores.ErrUnavailable
	}
	cores, err := client.List(ctx)
	if err != nil {
		return err
	}
	packageID := ""
	for _, core := range cores {
		if core.CoreID == "fes.sms" {
			packageID = core.PackageID
			break
		}
	}
	if packageID == "" {
		return errSMSCoreMissing
	}
	path, err := resolve(ctx, id)
	if err != nil || !strings.HasPrefix(path, "/") {
		return errCartridgeMissing
	}
	return client.LaunchROM(ctx, packageID, path)
}

func dispatchLocalAction(ctx context.Context, client LocalCoreClient, resolve func(context.Context, string) (string, error), games []hostclient.Game, action, id string) error {
	if client == nil {
		return localcores.ErrUnavailable
	}
	if action == "local-launch" {
		return launchLocalGame(ctx, client, resolve, games, id)
	}
	if action == "stop" {
		return client.Stop(ctx)
	}
	return localcores.ErrUnavailable
}

var errSMSCoreMissing = errors.New("sms core missing")
var errCartridgeMissing = errors.New("cartridge missing")
var errNotOnKit = errors.New("not on this kit")
var errCartridgeCheck = errors.New("cartridge check failed")

func containsLocalID(games []hostclient.Game, id string) bool {
	for _, game := range games {
		if game.ID == id {
			return true
		}
	}
	return false
}

func localCoreMessage(err error) string {
	switch {
	case errors.Is(err, errSMSCoreMissing):
		return "The Master System core is not installed."
	case errors.Is(err, errCartridgeMissing), errors.Is(err, localcores.ErrNotFound):
		return "Cartridge is missing"
	case errors.Is(err, errNotOnKit):
		return "Not on this kit"
	case errors.Is(err, errCartridgeCheck):
		return "Could not check this kit's cartridges"
	case errors.Is(err, localcores.ErrInUse):
		return "Kit is in use"
	case errors.Is(err, localcores.ErrBlocked):
		return "Kit cannot launch this game"
	default:
		return "Kit local control is unavailable"
	}
}

func listLocalCatalog(ctx context.Context, base string) ([]hostclient.Game, error) {
	client := hostclient.NewClient(base, &http.Client{Timeout: 5 * time.Second})
	var all []hostclient.Game
	var cursor string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		games, next, err := client.ListGames(ctx, hostclient.GameListQuery{Limit: 200, Availability: "all", Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, games...)
		if next == "" || len(games) == 0 || len(all) >= 2000 {
			break
		}
		cursor = next
	}
	return all, nil
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
