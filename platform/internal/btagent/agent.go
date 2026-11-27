package btagent

import (
	"bufio"
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	systemSocket = "/var/run/dbus/system_bus_socket"
	agentPath    = "/org/fes/bluetooth/agent"
	agentCap     = "NoInputNoOutput"
	dbusDest     = "org.freedesktop.DBus"
	dbusPath     = "/org/freedesktop/DBus"
	dbusIface    = "org.freedesktop.DBus"
	bluezDest    = "org.bluez"
	adapterIface = "org.bluez.Adapter1"
	deviceIface  = "org.bluez.Device1"
	agentIface   = "org.bluez.Agent1"
	propsIface   = "org.freedesktop.DBus.Properties"
	objectsIface = "org.freedesktop.DBus.ObjectManager"
	flagNoReply  = 1
	// staleBondRemovalMax bounds recovery attempts for one device during this
	// agent lifetime, including attempts whose RemoveDevice call fails.
	staleBondRemovalMax = 3
	pairCallTimeout     = 35 * time.Second
	pairCleanupTimeout  = 10 * time.Second
	maxReconnects       = 4
	maxPairs            = 4
)

type dbusError struct {
	Name    string
	Message string
}

func (e *dbusError) Error() string {
	if e.Name == "" {
		return "dbus error"
	}
	if e.Message != "" {
		return e.Name + ": " + e.Message
	}
	return e.Name
}

func isName(err error, name string) bool {
	var de *dbusError
	return errors.As(err, &de) && de.Name == name
}

type devInfo struct {
	Path       string
	Address    string
	Name       string
	UUIDs      []string
	Class      uint32
	Appearance uint16
	Modalias   string
	Paired     bool
	Connected  bool
	Trusted    bool
}

type adapterInfo struct {
	Path        string
	Name        string
	Alias       string
	Address     string
	Powered     bool
	Discovering bool
}

type bus struct {
	r      *bufio.Reader
	w      io.Writer
	wmu    sync.Mutex
	mu     sync.Mutex
	next   uint32
	wait   map[uint32]chan *Message
	queue  []*Message
	kick   chan struct{}
	done   chan struct{}
	err    error
	once   sync.Once
	onCall func(*Message)
}

func newBus(r *bufio.Reader, w io.Writer) *bus {
	return &bus{
		r:    r,
		w:    w,
		wait: map[uint32]chan *Message{},
		kick: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

func (b *bus) fail(err error) {
	b.once.Do(func() {
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		close(b.done)
	})
}

func (b *bus) readLoop() {
	for {
		msg, err := readMessage(b.r)
		if err != nil {
			b.fail(err)
			return
		}
		switch msg.Type {
		case msgMethodReturn, msgError:
			b.mu.Lock()
			ch := b.wait[msg.ReplySerial]
			delete(b.wait, msg.ReplySerial)
			b.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		case msgSignal:
			b.mu.Lock()
			b.queue = append(b.queue, msg)
			b.mu.Unlock()
			select {
			case b.kick <- struct{}{}:
			default:
			}
		case msgMethodCall:
			if b.onCall != nil {
				b.onCall(msg)
			}
		}
	}
}

func (b *bus) call(ctx context.Context, msg *Message, body []byte) (*Message, error) {
	ch := make(chan *Message, 1)
	b.mu.Lock()
	b.next++
	if b.next == 0 {
		b.next = 1
	}
	msg.Serial = b.next
	b.wait[msg.Serial] = ch
	serial := msg.Serial
	b.mu.Unlock()
	b.wmu.Lock()
	err := writeMessage(b.w, msg, body)
	b.wmu.Unlock()
	if err != nil {
		b.fail(err)
		return nil, err
	}
	select {
	case reply := <-ch:
		return reply, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.wait, serial)
		b.mu.Unlock()
		return nil, ctx.Err()
	case <-b.done:
		// readLoop may deliver the final reply and then observe EOF. In that
		// case both ch and done are ready, and select can choose done even
		// though the reply was already received.
		select {
		case reply := <-ch:
			return reply, nil
		default:
		}
		b.mu.Lock()
		err = b.err
		b.mu.Unlock()
		if err == nil {
			err = io.ErrClosedPipe
		}
		return nil, err
	}
}

func (b *bus) send(msg *Message, body []byte) error {
	b.mu.Lock()
	b.next++
	if b.next == 0 {
		b.next = 1
	}
	msg.Serial = b.next
	b.mu.Unlock()
	b.wmu.Lock()
	defer b.wmu.Unlock()
	return writeMessage(b.w, msg, body)
}

func (b *bus) drain() []*Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.queue
	b.queue = nil
	return out
}

func handshake(raw io.ReadWriteCloser) (*bus, error) {
	if d, ok := raw.(interface{ SetDeadline(time.Time) error }); ok {
		_ = d.SetDeadline(time.Now().Add(10 * time.Second))
		defer func() { _ = d.SetDeadline(time.Time{}) }()
	}
	r := bufio.NewReader(raw)
	token := hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))
	if _, err := io.WriteString(raw, "\x00AUTH EXTERNAL "+token+"\r\n"); err != nil {
		return nil, err
	}
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "OK") {
		return nil, fmt.Errorf("dbus auth: %s", line)
	}
	if _, err := io.WriteString(raw, "BEGIN\r\n"); err != nil {
		return nil, err
	}
	return newBus(r, raw), nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// ServeWithControl dials the system bus and pairs/reconnects controllers
// until ctx is cancelled or the bus connection fails. If ctrl is non-nil, it
// also handles pairing-window and device-removal commands from the local UI.
func ServeWithControl(ctx context.Context, ctrl *ControlServer) error {
	conn, err := net.DialTimeout("unix", systemSocket, 2*time.Second)
	if err != nil {
		return err
	}
	return RunWithControl(ctx, conn, ctrl)
}

// RunWithControl pairs and reconnects Bluetooth controllers until ctx is
// cancelled or the bus connection fails. The caller redials.
func RunWithControl(ctx context.Context, raw io.ReadWriteCloser, ctrl *ControlServer) error {
	defer raw.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			raw.Close()
		case <-stop:
		}
	}()
	bus, err := handshake(raw)
	if err != nil {
		return err
	}
	a := &agent{
		bus:                      bus,
		attempts:                 newAttempts(),
		devices:                  map[string]devInfo{},
		adapters:                 map[string]adapterInfo{},
		configuredAdapters:       map[string]struct{}{},
		pendingPairingCancel:     map[string]struct{}{},
		logged:                   map[string]time.Time{},
		staleBondRemoved:         map[string]struct{}{},
		staleBondRemovalAttempts: map[string]int{},
		reconnectDone:            make(chan reconnectResult, maxReconnects),
		pairDone:                 make(chan pairResult, maxPairs),
		activePairAdapters:       map[string]struct{}{},
		deviceGeneration:         map[string]uint64{},
		forgotten:                map[string]struct{}{},
		controlCh:                make(chan controlRequest, 8),
	}
	bus.onCall = a.handleCall
	go bus.readLoop()
	if ctrl != nil {
		ctrl.attach(a)
		defer ctrl.detach(a)
	}
	if err := a.run(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

type agent struct {
	bus                      *bus
	attempts                 *attempts
	devices                  map[string]devInfo
	adapters                 map[string]adapterInfo
	logged                   map[string]time.Time
	bluezOwner               string
	registeredOwner          string
	configuredAdapters       map[string]struct{}
	pendingPairingCancel     map[string]struct{}
	staleBondMu              sync.Mutex
	staleBondRemoved         map[string]struct{}
	staleBondRemovalAttempts map[string]int
	reconnectDone            chan reconnectResult
	reconnects               int
	reconnectCtx             context.Context
	reconnectCancel          context.CancelFunc
	pairDone                 chan pairResult
	pairs                    int
	activePairAdapters       map[string]struct{}
	pairingCtx               context.Context
	pairingCancel            context.CancelFunc
	deviceGeneration         map[string]uint64

	pairingUntil time.Time
	pairingTimer *time.Timer
	forgotten    map[string]struct{}
	controlCh    chan controlRequest
	now          func() time.Time

	statusMu           sync.RWMutex
	cachedStatus       Status
	cachedPairingUntil time.Time
}

type reconnectResult struct {
	dev        devInfo
	owner      string
	trustErr   error
	err        error
	generation uint64
}

type pairResult struct {
	dev                    devInfo
	owner                  string
	generation             uint64
	pairErr                error
	pendingCancel          bool
	restoreFailedAfterPair bool
	restoreErr             error
	trustErr               error
	connectErr             error
}

func (a *agent) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *agent) pairingActive(now time.Time) bool {
	return !a.pairingUntil.IsZero() && now.Before(a.pairingUntil)
}

func (a *agent) resetPairingTimer(d time.Duration) {
	if a.pairingTimer == nil {
		a.pairingTimer = time.NewTimer(d)
		return
	}
	if !a.pairingTimer.Stop() {
		select {
		case <-a.pairingTimer.C:
		default:
		}
	}
	a.pairingTimer.Reset(d)
}

func (a *agent) stopPairingTimer() {
	if a.pairingTimer == nil {
		return
	}
	if !a.pairingTimer.Stop() {
		select {
		case <-a.pairingTimer.C:
		default:
		}
	}
}

func (a *agent) pairingTimerChan() <-chan time.Time {
	if a.pairingTimer == nil {
		return nil
	}
	return a.pairingTimer.C
}

func (a *agent) run(ctx context.Context) error {
	a.resetReconnectContext(ctx)
	a.resetPairingContext(ctx)
	defer func() {
		a.reconnectCancel()
		a.pairingCancel()
		a.stopPairingTimer()
	}()
	if _, err := a.call(ctx, dbusDest, dbusPath, dbusIface, "Hello", "", nil, 10*time.Second); err != nil {
		return err
	}
	if err := a.addSignalMatches(ctx); err != nil {
		return err
	}
	owner, err := a.nameOwner(ctx, bluezDest)
	if err != nil {
		if isName(err, "org.freedesktop.DBus.Error.NameHasNoOwner") {
			log.Printf("bluetooth: waiting for %s to acquire its D-Bus name", bluezDest)
		} else {
			return fmt.Errorf("D-Bus GetNameOwner for %s: %w", bluezDest, err)
		}
	} else {
		a.bluezOwner = owner
		if err := a.setupBlueZ(ctx); err != nil {
			log.Printf("bluetooth: setup failed for org.bluez owner %s: %v", owner, err)
		}
	}
	a.publishSnapshot()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.bus.done:
			a.bus.mu.Lock()
			err := a.bus.err
			a.bus.mu.Unlock()
			if err == nil {
				err = io.ErrClosedPipe
			}
			return err
		case <-a.bus.kick:
			if err := a.handleSignals(ctx); err != nil {
				return err
			}
		case result := <-a.reconnectDone:
			if err := a.handleReconnectResult(ctx, result); err != nil {
				return err
			}
		case result := <-a.pairDone:
			if err := a.handlePairResult(ctx, result); err != nil {
				return err
			}
		case req := <-a.controlCh:
			a.handleControl(ctx, req)
		case <-a.pairingTimerChan():
			if !a.pairingUntil.IsZero() && !a.clock().Before(a.pairingUntil) {
				a.pairingUntil = time.Time{}
				a.resetPairingContext(ctx)
				a.stopDiscoveryAll(ctx)
				a.publishSnapshot()
			}
		case <-ticker.C:
			if a.bluezOwner == "" {
				continue
			}
			if a.registeredOwner != a.bluezOwner {
				if err := a.setupBlueZ(ctx); err != nil {
					log.Printf("bluetooth: setup failed for org.bluez owner %s: %v", a.bluezOwner, err)
				}
				continue
			}
			if err := a.refresh(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				a.logf("refresh", "bluetooth: %v", err)
			}
		}
	}
}

func (a *agent) nameOwner(ctx context.Context, name string) (string, error) {
	reply, err := a.call(ctx, dbusDest, dbusPath, dbusIface, "GetNameOwner", "s", bodyString(name), 10*time.Second)
	if err != nil {
		return "", err
	}
	d := &decoder{b: reply.Body}
	owner, rest, err := d.readComplete("s")
	if err != nil || rest != "" {
		return "", errors.New("D-Bus GetNameOwner returned an invalid owner")
	}
	value, ok := owner.(string)
	if !ok || value == "" {
		return "", errors.New("D-Bus GetNameOwner returned an empty owner")
	}
	return value, nil
}

func (a *agent) setupBlueZ(ctx context.Context) error {
	if a.bluezOwner == "" {
		return nil
	}
	if a.registeredOwner != a.bluezOwner {
		err := a.callVoid(ctx, bluezDest, "/org/bluez", "org.bluez.AgentManager1", "RegisterAgent", "os", bodyPathString(agentPath, agentCap), 10*time.Second)
		if err != nil && !isName(err, "org.bluez.Error.AlreadyExists") {
			return fmt.Errorf("RegisterAgent: %w", err)
		}
		if err := a.callVoid(ctx, bluezDest, "/org/bluez", "org.bluez.AgentManager1", "RequestDefaultAgent", "o", bodyPath(agentPath), 10*time.Second); err != nil {
			return fmt.Errorf("RequestDefaultAgent: %w", err)
		}
		a.registeredOwner = a.bluezOwner
		log.Printf("bluetooth: agent registered as default for org.bluez owner %s", a.bluezOwner)
	}
	if err := a.refresh(ctx); err != nil {
		return fmt.Errorf("refresh BlueZ adapters: %w", err)
	}
	return nil
}

func (a *agent) handleSignals(ctx context.Context) error {
	for {
		batch := a.bus.drain()
		if len(batch) == 0 {
			return nil
		}
		for _, msg := range batch {
			if err := a.onSignal(ctx, msg); err != nil {
				return err
			}
		}
	}
}

func (a *agent) refresh(ctx context.Context) error {
	reply, err := a.call(ctx, bluezDest, "/", objectsIface, "GetManagedObjects", "", nil, 10*time.Second)
	if err != nil {
		return err
	}
	devices, adapters, err := decodeObjects(reply.Body)
	if err != nil {
		return err
	}
	for devicePath, next := range devices {
		if old, ok := a.devices[devicePath]; !ok || old.Paired != next.Paired {
			a.invalidateDevice(devicePath)
		}
	}
	for devicePath := range a.devices {
		if _, ok := devices[devicePath]; !ok {
			a.invalidateDevice(devicePath)
		}
	}
	a.devices = devices
	a.retryPendingPairingCleanup()
	a.adapters = adapters
	for path := range a.configuredAdapters {
		if _, ok := a.adapters[path]; !ok {
			delete(a.configuredAdapters, path)
		}
	}
	for path := range adapters {
		a.configure(ctx, path)
	}
	a.publishSnapshot()
	for _, dev := range devices {
		a.act(ctx, dev)
	}
	return nil
}

func (a *agent) addSignalMatches(ctx context.Context) error {
	rules := []string{
		"type='signal',sender='org.bluez',path='/',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesAdded'",
		"type='signal',sender='org.bluez',path='/',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesRemoved'",
		"type='signal',sender='org.bluez',path_namespace='/org/bluez',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'",
		"type='signal',sender='org.freedesktop.DBus',path='/org/freedesktop/DBus',interface='org.freedesktop.DBus',member='NameOwnerChanged',arg0='org.bluez'",
	}
	for _, rule := range rules {
		if err := a.callVoid(ctx, dbusDest, dbusPath, dbusIface, "AddMatch", "s", bodyString(rule), 10*time.Second); err != nil {
			return fmt.Errorf("dbus AddMatch: %w", err)
		}
	}
	return nil
}

func (a *agent) onSignal(ctx context.Context, msg *Message) error {
	switch msg.Member {
	case "NameOwnerChanged":
		name, _, newOwner, err := decodeNameOwnerChanged(msg.Body)
		if err != nil {
			a.logf("owner", "bluetooth: NameOwnerChanged: %v", err)
			return nil
		}
		if name != bluezDest || newOwner == a.bluezOwner {
			return nil
		}
		previousOwner := a.bluezOwner
		a.resetReconnectContext(ctx)
		a.resetPairingContext(ctx)
		a.bluezOwner = newOwner
		a.registeredOwner = ""
		a.adapters = map[string]adapterInfo{}
		a.devices = map[string]devInfo{}
		a.configuredAdapters = map[string]struct{}{}
		a.pendingPairingCancel = map[string]struct{}{}
		a.stopPairingTimer()
		a.pairingUntil = time.Time{}
		a.resetStaleBondRemovalClaims()
		a.publishSnapshot()
		if newOwner == "" {
			log.Printf("bluetooth: org.bluez owner disappeared from %s", previousOwner)
			return nil
		}
		log.Printf("bluetooth: org.bluez owner changed from %s to %s; restoring agent and adapters", previousOwner, newOwner)
		if err := a.setupBlueZ(ctx); err != nil {
			log.Printf("bluetooth: setup failed for org.bluez owner %s: %v", newOwner, err)
		}
	case "InterfacesAdded":
		path, ifaces, err := decodeAdded(msg.Body)
		if err != nil {
			a.logf("added", "bluetooth: InterfacesAdded: %v", err)
			return nil
		}
		if props, ok := ifaces[adapterIface]; ok {
			a.mergeAdapter(path, props)
			a.configure(ctx, path)
			a.publishSnapshot()
		}
		if props, ok := ifaces[deviceIface]; ok {
			if _, exists := a.devices[path]; !exists {
				a.invalidateDevice(path)
			}
			a.merge(path, props)
			a.publishSnapshot()
			a.act(ctx, a.devices[path])
		}
	case "InterfacesRemoved":
		path, names, err := decodeRemoved(msg.Body)
		if err != nil {
			return nil
		}
		for _, name := range names {
			switch name {
			case deviceIface:
				a.invalidateDevice(path)
				delete(a.devices, path)
			case adapterIface:
				delete(a.adapters, path)
				delete(a.configuredAdapters, path)
			}
		}
		a.publishSnapshot()
	case "PropertiesChanged":
		iface, props, err := decodeChanged(msg.Body)
		if err != nil {
			a.logf("changed", "bluetooth: PropertiesChanged: %v", err)
			return nil
		}
		if iface == deviceIface && msg.Path != "" {
			if paired, ok := props["Paired"].(bool); ok && a.devices[msg.Path].Paired != paired {
				a.invalidateDevice(msg.Path)
			}
			a.merge(msg.Path, props)
			a.publishSnapshot()
			a.act(ctx, a.devices[msg.Path])
		}
		if iface == adapterIface && msg.Path != "" {
			a.mergeAdapter(msg.Path, props)
			if adapterNeedsSetup(props, a.pairingActive(a.clock())) {
				delete(a.configuredAdapters, msg.Path)
			}
			a.configure(ctx, msg.Path)
			a.publishSnapshot()
		}
	}
	return nil
}

func (a *agent) configure(ctx context.Context, path string) {
	if _, ok := a.configuredAdapters[path]; ok {
		return
	}
	if a.configuredAdapters == nil {
		a.configuredAdapters = map[string]struct{}{}
	}
	sets := []struct {
		name string
		body []byte
	}{
		{"Powered", bodyProp(adapterIface, "Powered", "b", true, 0)},
	}
	pairingPending := a.adapterHasPendingPairing(path)
	if !pairingPending {
		// Keep BlueZ bondability off while idle. Pairable is BlueZ's controller
		// bondable setting, so pairing enables it only for Pair.
		sets = append(sets, struct {
			name string
			body []byte
		}{"Pairable", bodyProp(adapterIface, "Pairable", "b", false, 0)})
	}
	sets = append(sets, struct {
		name string
		body []byte
	}{"Discoverable", bodyProp(adapterIface, "Discoverable", "b", false, 0)})
	configured := true
	for _, set := range sets {
		if err := a.callVoid(ctx, bluezDest, path, propsIface, "Set", "ssv", set.body, 10*time.Second); err != nil {
			configured = false
			log.Printf("bluetooth: adapter setup failed %s set %s: %v", path, set.name, err)
		}
	}
	wantDiscovery := a.pairingActive(a.clock())
	info := a.adapters[path]
	if wantDiscovery {
		err := a.callVoid(ctx, bluezDest, path, adapterIface, "StartDiscovery", "", nil, 10*time.Second)
		if err != nil && !isName(err, "org.bluez.Error.InProgress") {
			configured = false
			log.Printf("bluetooth: adapter setup failed %s start discovery: %v", path, err)
		} else {
			info.Discovering = true
			a.adapters[path] = info
		}
	} else if info.Discovering {
		err := a.callVoid(ctx, bluezDest, path, adapterIface, "StopDiscovery", "", nil, 10*time.Second)
		if err != nil && !isStopDiscoveryBenign(err) {
			configured = false
			log.Printf("bluetooth: adapter setup failed %s stop discovery: %v", path, err)
		} else {
			info.Discovering = false
			a.adapters[path] = info
		}
	}
	if configured {
		a.configuredAdapters[path] = struct{}{}
		stateDesc := "discovery idle"
		if wantDiscovery {
			stateDesc = "discovery active"
		}
		if pairingPending {
			log.Printf("bluetooth: adapter setup succeeded %s (Pairable restoration pending, %s)", path, stateDesc)
		} else {
			log.Printf("bluetooth: adapter setup succeeded %s (Powered=true Pairable=false Discoverable=false, %s)", path, stateDesc)
		}
	}
}

func isStopDiscoveryBenign(err error) bool {
	var de *dbusError
	if !errors.As(err, &de) {
		return false
	}
	if de.Name == "org.bluez.Error.NotReady" || de.Name == "org.bluez.Error.DoesNotExist" {
		return true
	}
	return de.Name == "org.bluez.Error.Failed" && strings.Contains(strings.ToLower(de.Message), "no discovery")
}

func (a *agent) adapterHasPendingPairing(adapterPath string) bool {
	if _, active := a.activePairAdapters[adapterPath]; active {
		return true
	}
	for devicePath := range a.pendingPairingCancel {
		if path.Dir(devicePath) == adapterPath {
			return true
		}
	}
	return false
}

func (a *agent) retryPendingPairingCleanup() {
	for devicePath := range a.pendingPairingCancel {
		if err := a.cancelPairing(devicePath); err != nil {
			a.logf("cancel-pairing:"+devicePath, "bluetooth: cancel pending pairing %s: %v", devicePath, err)
			continue
		}
		adapterPath := path.Dir(devicePath)
		if err := a.restoreAdapterPairable(adapterPath); err != nil {
			a.logf("restore-pairable:"+adapterPath, "bluetooth: restore adapter pairability %s after pairing cancellation: %v", adapterPath, err)
			continue
		}
		delete(a.pendingPairingCancel, devicePath)
		delete(a.configuredAdapters, adapterPath)
	}
}

func adapterNeedsSetup(props map[string]any, pairingActive bool) bool {
	if powered, ok := props["Powered"].(bool); ok && !powered {
		return true
	}
	if pairable, ok := props["Pairable"].(bool); ok && pairable {
		return true
	}
	if discoverable, ok := props["Discoverable"].(bool); ok && discoverable {
		return true
	}
	if discovering, ok := props["Discovering"].(bool); ok && discovering != pairingActive {
		return true
	}
	return false
}

func (a *agent) act(ctx context.Context, dev devInfo) {
	what := decide(dev)
	if what == actionIgnore {
		return
	}
	if what == actionPair && (!a.pairingActive(a.clock()) || a.isForgotten(dev) || a.adapterHasPendingPairing(path.Dir(dev.Path)) || a.pairs >= maxPairs || a.bluezOwner == "") {
		return
	}
	if what == actionConnect && (a.reconnects >= maxReconnects || a.bluezOwner == "") {
		return
	}
	if !a.attempts.Start(dev, time.Now(), what) {
		return
	}
	if what == actionConnect {
		// Keep the event loop free to observe other devices while BlueZ waits
		// for a sleeping controller. Pin calls to the current unique owner so
		// an old attempt cannot mutate a replacement BlueZ instance.
		a.reconnects++
		workerCtx := a.reconnectCtx
		if workerCtx == nil {
			workerCtx = ctx
		}
		go a.reconnect(workerCtx, dev, a.bluezOwner, a.deviceGeneration[dev.Path])
		return
	}
	// Keep the event loop free to serve control requests (including
	// POST /pairing/stop) and timer expiry while BlueZ waits on Pair.
	a.pairs++
	if a.activePairAdapters == nil {
		a.activePairAdapters = map[string]struct{}{}
	}
	a.activePairAdapters[path.Dir(dev.Path)] = struct{}{}
	workerCtx := a.pairingCtx
	if workerCtx == nil {
		workerCtx = ctx
	}
	log.Printf("bluetooth: pairing %s %s", dev.Path, dev.Name)
	go a.pairAndConnect(workerCtx, dev, a.bluezOwner, a.deviceGeneration[dev.Path])
}

// An outstanding operation belongs to one BlueZ owner. Cancellation frees a
// slot promptly on daemon restart; the unique destination also prevents an
// old call from being delivered to the new daemon before the signal arrives.
func (a *agent) resetReconnectContext(ctx context.Context) {
	if a.reconnectCancel != nil {
		a.reconnectCancel()
	}
	a.reconnectCtx, a.reconnectCancel = context.WithCancel(ctx)
}

func (a *agent) resetPairingContext(ctx context.Context) {
	if a.pairingCancel != nil {
		a.pairingCancel()
	}
	a.pairingCtx, a.pairingCancel = context.WithCancel(ctx)
}

func (a *agent) invalidateDevice(devicePath string) {
	if a.deviceGeneration == nil {
		a.deviceGeneration = map[string]uint64{}
	}
	a.deviceGeneration[devicePath]++
}

func (a *agent) reconnect(ctx context.Context, dev devInfo, owner string, generation uint64) {
	trustErr := a.callVoid(ctx, owner, dev.Path, propsIface, "Set", "ssv", bodyProp(deviceIface, "Trusted", "b", true, 0), 10*time.Second)
	var err error
	if ctx.Err() == nil {
		err = a.callVoid(ctx, owner, dev.Path, deviceIface, "Connect", "", nil, 35*time.Second)
	} else {
		err = ctx.Err()
	}
	// At most maxReconnects workers exist, and the channel has that many
	// slots, so shutdown does not leave a worker blocked on result delivery.
	a.reconnectDone <- reconnectResult{dev: dev, owner: owner, generation: generation, trustErr: trustErr, err: err}
}

func (a *agent) handleReconnectResult(ctx context.Context, result reconnectResult) error {
	// A reply and earlier BlueZ signals can both be queued when select wakes.
	// Process those signals first so a removed/replaced device or BlueZ owner
	// invalidates an old missing-key result before it can remove a bond.
	if err := a.handleSignals(ctx); err != nil {
		return err
	}
	a.finishReconnect(ctx, result)
	return nil
}

func (a *agent) finishReconnect(ctx context.Context, result reconnectResult) {
	a.reconnects--
	a.attempts.Finish(result.dev.Path, false, time.Now())
	// The response belongs to the BlueZ owner captured at dispatch. In
	// particular, never remove a bond from a newly restarted daemon.
	if result.owner != a.bluezOwner || result.generation != a.deviceGeneration[result.dev.Path] {
		a.attempts.ForgetConnect(result.dev.Path)
		return
	}
	if device, exists := a.devices[result.dev.Path]; !exists || !device.Paired || device.Connected {
		return
	}
	if result.trustErr != nil {
		a.logf("trust:"+result.dev.Path, "bluetooth: trust %s: %v", result.dev.Path, result.trustErr)
	}
	if result.err == nil || isName(result.err, "org.bluez.Error.AlreadyConnected") {
		return
	}
	if result.dev.Paired && isGamepad(result.dev) && isStaleBondError(result.err) && a.claimStaleBondRemoval(result.dev.Path) {
		adapterPath := path.Dir(result.dev.Path)
		log.Printf("bluetooth: removing stale bond for %s after missing-key error: %v", result.dev.Path, result.err)
		if err := a.callVoid(ctx, result.owner, adapterPath, adapterIface, "RemoveDevice", "o", bodyPath(result.dev.Path), 10*time.Second); err != nil {
			a.releaseStaleBondRemoval(result.dev.Path)
			a.logf("remove-stale-bond:"+result.dev.Path, "bluetooth: remove stale bond %s: %v", result.dev.Path, err)
		} else {
			a.attempts.AllowPairAfterBondRemoval(result.dev.Path)
		}
		return
	}
	a.logf("connect:"+result.dev.Path, "bluetooth: connect %s: %v", result.dev.Path, result.err)
}

func (a *agent) pairAndConnect(ctx context.Context, dev devInfo, owner string, generation uint64) {
	res := a.runPairDevice(ctx, owner, dev, pairCallTimeout)
	res.generation = generation
	if res.pairErr != nil {
		a.pairDone <- res
		return
	}
	if ctx.Err() != nil {
		res.connectErr = ctx.Err()
		a.pairDone <- res
		return
	}
	res.trustErr = a.callVoid(ctx, owner, dev.Path, propsIface, "Set", "ssv", bodyProp(deviceIface, "Trusted", "b", true, 0), 10*time.Second)
	if ctx.Err() != nil {
		res.connectErr = ctx.Err()
		a.pairDone <- res
		return
	}
	log.Printf("bluetooth: connecting %s %s", dev.Path, dev.Name)
	res.connectErr = a.callVoid(ctx, owner, dev.Path, deviceIface, "Connect", "", nil, 35*time.Second)
	a.pairDone <- res
}

func (a *agent) handlePairResult(ctx context.Context, result pairResult) error {
	if err := a.handleSignals(ctx); err != nil {
		return err
	}
	a.finishPair(result)
	return nil
}

func (a *agent) finishPair(result pairResult) {
	a.pairs--
	delete(a.activePairAdapters, path.Dir(result.dev.Path))
	if result.owner != a.bluezOwner || result.generation != a.deviceGeneration[result.dev.Path] {
		a.attempts.Finish(result.dev.Path, false, time.Now())
		return
	}
	pairSucceeded := result.pairErr == nil
	a.attempts.Finish(result.dev.Path, pairSucceeded, time.Now())
	a.applyPairOutcome(result)
	if !pairSucceeded {
		a.logf("pair:"+result.dev.Path, "bluetooth: pair %s: %v", result.dev.Path, result.pairErr)
		return
	}
	a.resetStaleBondRemoval(result.dev.Path)
	dev := result.dev
	if existing, ok := a.devices[result.dev.Path]; ok {
		dev = existing
	}
	dev.Paired = true
	if result.trustErr != nil {
		a.logf("trust:"+dev.Path, "bluetooth: trust %s: %v", dev.Path, result.trustErr)
	}
	if result.connectErr != nil && !isName(result.connectErr, "org.bluez.Error.AlreadyConnected") {
		a.devices[dev.Path] = dev
		a.publishSnapshot()
		a.logf("connect:"+dev.Path, "bluetooth: connect %s: %v", dev.Path, result.connectErr)
		return
	}
	dev.Connected = true
	a.devices[dev.Path] = dev
	a.publishSnapshot()
}

func (a *agent) applyPairOutcome(result pairResult) {
	adapterPath := path.Dir(result.dev.Path)
	if result.pendingCancel {
		if a.pendingPairingCancel == nil {
			a.pendingPairingCancel = map[string]struct{}{}
		}
		a.pendingPairingCancel[result.dev.Path] = struct{}{}
		delete(a.configuredAdapters, adapterPath)
		return
	}
	if result.restoreFailedAfterPair {
		delete(a.configuredAdapters, adapterPath)
		a.logf("restore-pairable:"+adapterPath, "bluetooth: restore adapter pairability %s after successful pairing: %v", adapterPath, result.restoreErr)
	}
}

// runPairDevice enables the adapter's bondable state only while pairing an
// unpaired, policy-approved controller. BlueZ otherwise can complete a
// temporary pairing without persisting its link key. Restore the idle state
// even if Pair fails or the caller is cancelled.
func (a *agent) runPairDevice(ctx context.Context, dest string, dev devInfo, timeout time.Duration) pairResult {
	if dest == "" {
		dest = bluezDest
	}
	adapterPath := path.Dir(dev.Path)
	res := pairResult{dev: dev, owner: dest}
	if err := a.setAdapterPairableDest(ctx, dest, adapterPath, true); err != nil {
		enableErr := fmt.Errorf("enable adapter bondability: %w", err)
		if restoreErr := a.restoreAdapterPairableDest(dest, adapterPath); restoreErr != nil {
			res.pairErr = errors.Join(enableErr, fmt.Errorf("restore adapter pairability: %w", restoreErr))
			return res
		}
		res.pairErr = enableErr
		return res
	}
	pairErr := a.callVoid(ctx, dest, dev.Path, deviceIface, "Pair", "", nil, timeout)
	if pairErr != nil {
		var bluezErr *dbusError
		if !errors.As(pairErr, &bluezErr) {
			if cancelErr := a.cancelPairingDest(dest, dev.Path); cancelErr != nil {
				res.pairErr = errors.Join(pairErr, fmt.Errorf("cancel pairing before restoring adapter bondability: %w", cancelErr))
				res.pendingCancel = true
				return res
			}
		}
	}
	restoreErr := a.restoreAdapterPairableDest(dest, adapterPath)
	if pairErr != nil {
		if restoreErr != nil {
			res.pairErr = errors.Join(pairErr, fmt.Errorf("restore adapter pairability: %w", restoreErr))
			return res
		}
		res.pairErr = pairErr
		return res
	}
	if restoreErr != nil {
		// Pair has already created the bond. Retry transient restoration errors,
		// but keep the successful pairing path moving even if BlueZ still refuses
		// to restore its idle state. The next adapter refresh will configure it
		// again because this entry is no longer marked configured.
		if retryErr := a.restoreAdapterPairableDest(dest, adapterPath); retryErr != nil {
			restoreErr = errors.Join(restoreErr, retryErr)
		} else {
			return res
		}
		res.restoreFailedAfterPair = true
		res.restoreErr = restoreErr
	}
	return res
}

func (a *agent) restoreAdapterPairable(adapterPath string) error {
	return a.restoreAdapterPairableDest(bluezDest, adapterPath)
}

func (a *agent) restoreAdapterPairableDest(dest, adapterPath string) error {
	if dest == "" {
		dest = bluezDest
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), pairCleanupTimeout)
	defer cancel()
	return a.setAdapterPairableDest(cleanupCtx, dest, adapterPath, false)
}

func (a *agent) cancelPairing(devicePath string) error {
	return a.cancelPairingDest(bluezDest, devicePath)
}

func (a *agent) cancelPairingDest(dest, devicePath string) error {
	if dest == "" {
		dest = bluezDest
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), pairCleanupTimeout)
	defer cancel()
	err := a.callVoid(cleanupCtx, dest, devicePath, deviceIface, "CancelPairing", "", nil, pairCleanupTimeout)
	if isName(err, "org.bluez.Error.DoesNotExist") {
		return nil
	}
	return err
}

func (a *agent) setAdapterPairableDest(ctx context.Context, dest, adapterPath string, pairable bool) error {
	if dest == "" {
		dest = bluezDest
	}
	return a.callVoid(ctx, dest, adapterPath, propsIface, "Set", "ssv",
		bodyProp(adapterIface, "Pairable", "b", pairable, 0), 10*time.Second)
}

func (a *agent) claimStaleBondRemoval(devicePath string) bool {
	a.staleBondMu.Lock()
	defer a.staleBondMu.Unlock()
	if a.staleBondRemoved == nil {
		a.staleBondRemoved = map[string]struct{}{}
	}
	if a.staleBondRemovalAttempts == nil {
		a.staleBondRemovalAttempts = map[string]int{}
	}
	if _, exists := a.staleBondRemoved[devicePath]; exists {
		return false
	}
	if a.staleBondRemovalAttempts[devicePath] >= staleBondRemovalMax {
		return false
	}
	a.staleBondRemovalAttempts[devicePath]++
	a.staleBondRemoved[devicePath] = struct{}{}
	return true
}

func (a *agent) releaseStaleBondRemoval(devicePath string) {
	a.staleBondMu.Lock()
	defer a.staleBondMu.Unlock()
	delete(a.staleBondRemoved, devicePath)
}

// A successful pairing creates a fresh bond. Clear the current-bond claim,
// while retaining the cumulative cap to prevent an unbounded remove/pair loop.
func (a *agent) resetStaleBondRemoval(devicePath string) {
	a.releaseStaleBondRemoval(devicePath)
}

// BlueZ owner changes invalidate in-flight claims but preserve the attempt cap.
func (a *agent) resetStaleBondRemovalClaims() {
	a.staleBondMu.Lock()
	defer a.staleBondMu.Unlock()
	a.staleBondRemoved = map[string]struct{}{}
}

func isStaleBondError(err error) bool {
	if err == nil {
		return false
	}
	var bluezErr *dbusError
	if !errors.As(err, &bluezErr) {
		return false
	}
	message := strings.ToLower(bluezErr.Message)
	return strings.Contains(message, "key missing") || strings.Contains(message, "missing key") ||
		strings.Contains(message, "key-missing") || strings.Contains(message, "key_missing")
}

func (a *agent) merge(path string, props map[string]any) {
	dev := a.devices[path]
	dev.Path = path
	applyProps(&dev, props)
	a.devices[path] = dev
}

func (a *agent) mergeAdapter(adapterPath string, props map[string]any) {
	info := a.adapters[adapterPath]
	info.Path = adapterPath
	applyAdapterProps(&info, props)
	a.adapters[adapterPath] = info
}

func applyAdapterProps(info *adapterInfo, props map[string]any) {
	if s, ok := props["Name"].(string); ok {
		info.Name = s
	}
	if s, ok := props["Alias"].(string); ok && s != "" {
		info.Alias = s
	}
	if s, ok := props["Address"].(string); ok && s != "" {
		info.Address = strings.ToUpper(s)
	}
	if b, ok := props["Powered"].(bool); ok {
		info.Powered = b
	}
	if b, ok := props["Discovering"].(bool); ok {
		info.Discovering = b
	}
}

func (a *agent) call(ctx context.Context, dest, path, iface, member, sig string, body []byte, timeout time.Duration) (*Message, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	msg := &Message{Type: msgMethodCall, Dest: dest, Path: path, Iface: iface, Member: member, Signature: sig}
	reply, err := a.bus.call(ctx, msg, body)
	if err != nil {
		return nil, err
	}
	if reply.Type == msgError {
		message := ""
		d := &decoder{b: reply.Body}
		if value, rest, decodeErr := d.readComplete("s"); decodeErr == nil && rest == "" {
			message, _ = value.(string)
		}
		return nil, &dbusError{Name: reply.ErrorName, Message: message}
	}
	if reply.Type != msgMethodReturn {
		return nil, fmt.Errorf("dbus %s: unexpected reply type %d", member, reply.Type)
	}
	return reply, nil
}

func (a *agent) callVoid(ctx context.Context, dest, path, iface, member, sig string, body []byte, timeout time.Duration) error {
	_, err := a.call(ctx, dest, path, iface, member, sig, body, timeout)
	return err
}

func (a *agent) logf(key, format string, args ...any) {
	now := time.Now()
	if t, ok := a.logged[key]; ok && now.Sub(t) < 30*time.Second {
		return
	}
	a.logged[key] = now
	log.Printf(format, args...)
}

func (a *agent) handleCall(call *Message) {
	if call.Flags&flagNoReply != 0 {
		return
	}
	sig, body, errName := a.dispatch(call)
	reply := &Message{Type: msgMethodReturn, ReplySerial: call.Serial, Dest: call.Sender, Signature: sig}
	if errName != "" {
		reply.Type = msgError
		reply.ErrorName = errName
		reply.Signature = "s"
		e := &encoder{}
		e.str("rejected")
		body = e.buf
	}
	if err := a.bus.send(reply, body); err != nil {
		a.bus.fail(err)
	}
}

func (a *agent) dispatch(call *Message) (string, []byte, string) {
	switch call.Member {
	case "Release", "Cancel":
		return "", nil, ""
	case "DisplayPinCode", "DisplayPasskey":
		path, err := bodyObject(call.Body)
		if err != nil || !a.attempts.Has(path) {
			return "", nil, "org.bluez.Error.Rejected"
		}
		// NoInputNoOutput agents cannot display passkeys or PINs.
		return "", nil, "org.bluez.Error.Rejected"
	case "RequestPinCode":
		path, err := bodyObject(call.Body)
		if err != nil || !a.attempts.Has(path) {
			return "", nil, "org.bluez.Error.Rejected"
		}
		// Legacy gamepads commonly use this fixed PIN. The callback is only
		// reachable during an in-flight gamepad pairing attempt.
		return "s", bodyString("0000"), ""
	case "RequestPasskey":
		path, err := bodyObject(call.Body)
		if err != nil || !a.attempts.Has(path) {
			return "", nil, "org.bluez.Error.Rejected"
		}
		// NoInputNoOutput agents cannot enter an arbitrary passkey.
		return "", nil, "org.bluez.Error.Rejected"
	case "RequestConfirmation":
		// A NoInputNoOutput agent cannot compare or confirm a passkey. Just
		// Works pairing does not require this callback.
		log.Printf("bluetooth: rejecting passkey confirmation; NoInputNoOutput cannot verify passkeys")
		return "", nil, "org.bluez.Error.Rejected"
	case "RequestAuthorization":
		path, err := bodyObject(call.Body)
		if err != nil || !a.attempts.Has(path) {
			return "", nil, "org.bluez.Error.Rejected"
		}
		// attempts.Start admits only policy-approved gamepads, and Has requires
		// an in-flight Pair attempt. This is the Just Works authorization path.
		return "", nil, ""
	case "AuthorizeService":
		path, uuid, err := bodyPathUUID(call.Body)
		if err != nil || !a.attempts.HasServiceAuthorization(path) || !oneControllerUUID(uuid) {
			return "", nil, "org.bluez.Error.Rejected"
		}
		return "", nil, ""
	default:
		return "", nil, "org.freedesktop.DBus.Error.UnknownMethod"
	}
}

func bodyPath(path string) []byte {
	e := &encoder{}
	e.str(path)
	return e.buf
}

func bodyPathString(path, value string) []byte {
	e := &encoder{}
	e.str(path)
	e.str(value)
	return e.buf
}

func bodyString(value string) []byte {
	e := &encoder{}
	e.str(value)
	return e.buf
}

func bodyProp(iface, name, kind string, flag bool, n uint32) []byte {
	e := &encoder{}
	e.str(iface)
	e.str(name)
	e.sig(kind)
	switch kind {
	case "b":
		e.boolean(flag)
	case "u":
		e.u32(n)
	}
	return e.buf
}

func bodyObject(body []byte) (string, error) {
	d := &decoder{b: body}
	v, rest, err := d.readComplete("o")
	if err != nil || rest != "" {
		return "", errors.New("missing object path")
	}
	s, _ := v.(string)
	if s == "" {
		return "", errors.New("missing object path")
	}
	return s, nil
}

func bodyPathUUID(body []byte) (string, string, error) {
	d := &decoder{b: body}
	vals, err := d.readSig("os")
	if err != nil || len(vals) != 2 {
		return "", "", errors.New("missing service uuid")
	}
	path, _ := vals[0].(string)
	uuid, _ := vals[1].(string)
	if path == "" || uuid == "" {
		return "", "", errors.New("missing service uuid")
	}
	return path, uuid, nil
}

func decodeObjects(body []byte) (map[string]devInfo, map[string]adapterInfo, error) {
	d := &decoder{b: body}
	v, rest, err := d.readComplete("a{oa{sa{sv}}}")
	if err != nil || rest != "" {
		if err == nil {
			err = errors.New("trailing managed objects")
		}
		return nil, nil, err
	}
	entries, ok := v.([]any)
	if !ok {
		return nil, nil, errors.New("managed objects")
	}
	devices := map[string]devInfo{}
	adapters := map[string]adapterInfo{}
	for _, ent := range entries {
		pair, ok := ent.([]any)
		if !ok || len(pair) != 2 {
			return nil, nil, errors.New("managed object entry")
		}
		objPath, ok := pair[0].(string)
		if !ok {
			return nil, nil, errors.New("managed object path")
		}
		ifaces, err := ifaceMap(pair[1])
		if err != nil {
			return nil, nil, err
		}
		if props, ok := ifaces[adapterIface]; ok {
			info := adapterInfo{Path: objPath}
			applyAdapterProps(&info, props)
			adapters[objPath] = info
		}
		if props, ok := ifaces[deviceIface]; ok {
			dev := devInfo{Path: objPath}
			applyProps(&dev, props)
			devices[objPath] = dev
		}
	}
	return devices, adapters, nil
}

func decodeAdded(body []byte) (string, map[string]map[string]any, error) {
	d := &decoder{b: body}
	vals, err := d.readSig("oa{sa{sv}}")
	if err != nil || len(vals) != 2 {
		if err == nil {
			err = errors.New("InterfacesAdded")
		}
		return "", nil, err
	}
	path, ok := vals[0].(string)
	if !ok {
		return "", nil, errors.New("InterfacesAdded path")
	}
	ifaces, err := ifaceMap(vals[1])
	if err != nil {
		return "", nil, err
	}
	return path, ifaces, nil
}

func decodeRemoved(body []byte) (string, []string, error) {
	d := &decoder{b: body}
	vals, err := d.readSig("oas")
	if err != nil || len(vals) != 2 {
		return "", nil, errors.New("InterfacesRemoved")
	}
	path, ok := vals[0].(string)
	if !ok {
		return "", nil, errors.New("InterfacesRemoved path")
	}
	list, ok := vals[1].([]any)
	if !ok {
		return "", nil, errors.New("InterfacesRemoved interfaces")
	}
	var names []string
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return "", nil, errors.New("InterfacesRemoved name")
		}
		names = append(names, s)
	}
	return path, names, nil
}

func decodeChanged(body []byte) (string, map[string]any, error) {
	d := &decoder{b: body}
	vals, err := d.readSig("sa{sv}as")
	if err != nil || len(vals) < 2 {
		return "", nil, errors.New("PropertiesChanged")
	}
	iface, ok := vals[0].(string)
	if !ok {
		return "", nil, errors.New("PropertiesChanged interface")
	}
	props, err := propMap(vals[1])
	if err != nil {
		return "", nil, err
	}
	return iface, props, nil
}

func decodeNameOwnerChanged(body []byte) (string, string, string, error) {
	d := &decoder{b: body}
	values, err := d.readSig("sss")
	if err != nil || len(values) != 3 {
		return "", "", "", errors.New("NameOwnerChanged")
	}
	name, nameOK := values[0].(string)
	oldOwner, oldOK := values[1].(string)
	newOwner, newOK := values[2].(string)
	if !nameOK || !oldOK || !newOK || name == "" {
		return "", "", "", errors.New("NameOwnerChanged fields")
	}
	return name, oldOwner, newOwner, nil
}

func ifaceMap(v any) (map[string]map[string]any, error) {
	entries, ok := v.([]any)
	if !ok {
		return nil, errors.New("dbus interfaces")
	}
	out := map[string]map[string]any{}
	for _, ent := range entries {
		pair, ok := ent.([]any)
		if !ok || len(pair) != 2 {
			return nil, errors.New("dbus interface entry")
		}
		name, ok := pair[0].(string)
		if !ok {
			return nil, errors.New("dbus interface name")
		}
		props, err := propMap(pair[1])
		if err != nil {
			return nil, err
		}
		out[name] = props
	}
	return out, nil
}

func propMap(v any) (map[string]any, error) {
	entries, ok := v.([]any)
	if !ok {
		return nil, errors.New("dbus properties")
	}
	out := map[string]any{}
	for _, ent := range entries {
		pair, ok := ent.([]any)
		if !ok || len(pair) != 2 {
			return nil, errors.New("dbus property entry")
		}
		name, ok := pair[0].(string)
		if !ok {
			return nil, errors.New("dbus property name")
		}
		out[name] = pair[1]
	}
	return out, nil
}

func applyProps(dev *devInfo, props map[string]any) {
	if s, ok := props["Address"].(string); ok && s != "" {
		dev.Address = strings.ToUpper(s)
	}
	if s, ok := props["Name"].(string); ok {
		dev.Name = s
	}
	if s, ok := props["Alias"].(string); ok && s != "" {
		dev.Name = s
	}
	if u, ok := props["Class"].(uint32); ok {
		dev.Class = u
	}
	if u, ok := props["Appearance"].(uint16); ok {
		dev.Appearance = u
	}
	if s, ok := props["Modalias"].(string); ok {
		dev.Modalias = s
	}
	if b, ok := props["Paired"].(bool); ok {
		dev.Paired = b
	}
	if b, ok := props["Connected"].(bool); ok {
		dev.Connected = b
	}
	if b, ok := props["Trusted"].(bool); ok {
		dev.Trusted = b
	}
	if list, ok := props["UUIDs"].([]any); ok {
		dev.UUIDs = nil
		for _, item := range list {
			if s, ok := item.(string); ok {
				dev.UUIDs = append(dev.UUIDs, s)
			}
		}
	}
}

func deviceAddress(dev devInfo) string {
	if addr := strings.ToUpper(strings.TrimSpace(dev.Address)); addr != "" {
		return addr
	}
	base := path.Base(dev.Path)
	raw, ok := strings.CutPrefix(base, "dev_")
	if !ok {
		return ""
	}
	parts := strings.Split(raw, "_")
	if len(parts) != 6 {
		return ""
	}
	return strings.ToUpper(strings.Join(parts, ":"))
}

func (a *agent) isForgotten(dev devInfo) bool {
	if len(a.forgotten) == 0 {
		return false
	}
	addr := deviceAddress(dev)
	if addr == "" {
		return false
	}
	_, suppressed := a.forgotten[addr]
	return suppressed
}

func (a *agent) submitControl(ctx context.Context, req controlRequest) (Status, error) {
	req.reply = make(chan controlResult, 1)
	select {
	case a.controlCh <- req:
	case <-a.bus.done:
		return Status{}, &controlError{code: http.StatusServiceUnavailable, err: errors.New("bluetooth agent stopped")}
	case <-ctx.Done():
		return Status{}, ctx.Err()
	}
	select {
	case res := <-req.reply:
		return res.status, res.err
	case <-a.bus.done:
		return Status{}, &controlError{code: http.StatusServiceUnavailable, err: errors.New("bluetooth agent stopped")}
	case <-ctx.Done():
		return Status{}, ctx.Err()
	}
}

func (a *agent) handleControl(ctx context.Context, req controlRequest) {
	switch req.kind {
	case controlStartPairing:
		st, err := a.startPairing(ctx, req.duration)
		req.reply <- controlResult{status: st, err: err}
		if err == nil {
			for _, dev := range a.devices {
				a.act(ctx, dev)
			}
		}
	case controlStopPairing:
		st := a.stopPairing(ctx)
		req.reply <- controlResult{status: st}
	case controlForgetDevice:
		st, err := a.forgetDevice(ctx, req.address)
		req.reply <- controlResult{status: st, err: err}
	default:
		req.reply <- controlResult{err: &controlError{code: http.StatusBadRequest, err: errors.New("unknown control request")}}
	}
}

func (a *agent) startPairing(ctx context.Context, duration time.Duration) (Status, error) {
	if a.bluezOwner == "" || len(a.adapters) == 0 {
		return a.snapshot(), &controlError{code: http.StatusConflict, err: errors.New("no Bluetooth adapter available")}
	}
	clear(a.forgotten)
	now := a.clock()
	a.pairingUntil = now.Add(duration)
	a.resetPairingTimer(duration)
	started := false
	var firstErr error
	for adapterPath := range a.adapters {
		delete(a.configuredAdapters, adapterPath)
		a.configure(ctx, adapterPath)
		if info, ok := a.adapters[adapterPath]; ok && info.Discovering {
			started = true
		} else if firstErr == nil {
			firstErr = errors.New("bluetooth adapter could not start discovery")
		}
	}
	if !started && firstErr != nil {
		a.pairingUntil = time.Time{}
		a.stopPairingTimer()
		a.publishSnapshot()
		return a.snapshot(), &controlError{code: http.StatusInternalServerError, err: firstErr}
	}
	a.publishSnapshot()
	return a.snapshot(), nil
}

func (a *agent) stopPairing(ctx context.Context) Status {
	a.stopPairingTimer()
	a.pairingUntil = time.Time{}
	a.resetPairingContext(ctx)
	a.stopDiscoveryAll(ctx)
	a.publishSnapshot()
	return a.snapshot()
}

func (a *agent) stopDiscoveryAll(ctx context.Context) {
	for adapterPath, info := range a.adapters {
		delete(a.configuredAdapters, adapterPath)
		if !info.Discovering {
			continue
		}
		if a.bluezOwner != "" {
			if err := a.callVoid(ctx, bluezDest, adapterPath, adapterIface, "StopDiscovery", "", nil, 10*time.Second); err != nil && !isStopDiscoveryBenign(err) {
				a.logf("stop-discovery:"+adapterPath, "bluetooth: stop discovery %s: %v", adapterPath, err)
			}
		}
		info.Discovering = false
		a.adapters[adapterPath] = info
	}
}

func (a *agent) forgetDevice(ctx context.Context, address string) (Status, error) {
	addr := strings.ToUpper(strings.TrimSpace(address))
	if addr == "" {
		return a.snapshot(), &controlError{code: http.StatusBadRequest, err: errors.New("bluetooth address is required")}
	}
	var target *devInfo
	for _, dev := range a.devices {
		if strings.EqualFold(deviceAddress(dev), addr) {
			copyDev := dev
			target = &copyDev
			break
		}
	}
	if target == nil {
		return a.snapshot(), &controlError{code: http.StatusNotFound, err: fmt.Errorf("bluetooth device %s not found", addr)}
	}
	if a.bluezOwner == "" {
		return a.snapshot(), &controlError{code: http.StatusServiceUnavailable, err: errors.New("bluetooth service unavailable")}
	}
	err := a.callVoid(ctx, a.bluezOwner, path.Dir(target.Path), adapterIface, "RemoveDevice", "o", bodyPath(target.Path), 10*time.Second)
	if err != nil && !isName(err, "org.bluez.Error.DoesNotExist") {
		a.logf("forget:"+target.Path, "bluetooth: remove forgotten device %s: %v", target.Path, err)
		return a.snapshot(), &controlError{code: http.StatusInternalServerError, err: fmt.Errorf("remove bluetooth device %s: %w", addr, err)}
	}
	a.forgotten[addr] = struct{}{}
	a.invalidateDevice(target.Path)
	delete(a.devices, target.Path)
	a.attempts.AllowPairAfterBondRemoval(target.Path)
	a.resetStaleBondRemoval(target.Path)
	a.publishSnapshot()
	return a.snapshot(), nil
}

func (a *agent) publishSnapshot() {
	var adapterNames []string
	for p, info := range a.adapters {
		name := strings.TrimSpace(info.Alias)
		if name == "" {
			name = strings.TrimSpace(info.Name)
		}
		if name == "" {
			name = path.Base(p)
		}
		adapterNames = append(adapterNames, name)
	}
	slices.Sort(adapterNames)

	st := Status{
		AdapterConnected: len(a.adapters) > 0,
	}
	if len(adapterNames) > 0 {
		st.AdapterName = adapterNames[0]
	}
	for _, dev := range a.devices {
		if !isGamepad(dev) || (!dev.Paired && !dev.Connected) {
			continue
		}
		addr := deviceAddress(dev)
		if addr == "" {
			continue
		}
		name := strings.TrimSpace(dev.Name)
		if name == "" {
			name = addr
		}
		if dev.Connected {
			st.ConnectedCount++
		}
		st.Devices = append(st.Devices, DeviceStatus{
			Address:   addr,
			Name:      name,
			Kind:      controllerKindLabel(dev),
			Connected: dev.Connected,
		})
	}
	slices.SortFunc(st.Devices, func(a, b DeviceStatus) int {
		if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.Address, b.Address)
	})

	a.statusMu.Lock()
	a.cachedStatus = st
	a.cachedPairingUntil = a.pairingUntil
	a.statusMu.Unlock()
}

func (a *agent) snapshot() Status {
	now := a.clock()
	a.statusMu.RLock()
	st := a.cachedStatus
	until := a.cachedPairingUntil
	if len(a.cachedStatus.Devices) > 0 {
		st.Devices = append([]DeviceStatus(nil), a.cachedStatus.Devices...)
	}
	a.statusMu.RUnlock()

	if !until.IsZero() && now.Before(until) {
		st.PairingActive = true
		st.PairingRemainingMS = until.Sub(now).Milliseconds()
	}
	return st
}
