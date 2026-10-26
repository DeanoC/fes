package btagent

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
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
	Name       string
	UUIDs      []string
	Class      uint32
	Appearance uint16
	Modalias   string
	Paired     bool
	Connected  bool
	Trusted    bool
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

// Serve dials the system bus and pairs controllers until ctx is cancelled
// or the connection fails.
func Serve(ctx context.Context) error {
	conn, err := net.DialTimeout("unix", systemSocket, 2*time.Second)
	if err != nil {
		return err
	}
	return Run(ctx, conn)
}

// Run pairs and reconnects Bluetooth controllers until ctx is cancelled or
// the bus connection fails. The caller redials.
func Run(ctx context.Context, raw io.ReadWriteCloser) error {
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
		adapters:                 map[string]struct{}{},
		configuredAdapters:       map[string]struct{}{},
		pendingPairingCancel:     map[string]struct{}{},
		logged:                   map[string]time.Time{},
		staleBondRemoved:         map[string]struct{}{},
		staleBondRemovalAttempts: map[string]int{},
	}
	bus.onCall = a.handleCall
	go bus.readLoop()
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
	adapters                 map[string]struct{}
	logged                   map[string]time.Time
	bluezOwner               string
	registeredOwner          string
	configuredAdapters       map[string]struct{}
	pendingPairingCancel     map[string]struct{}
	staleBondMu              sync.Mutex
	staleBondRemoved         map[string]struct{}
	staleBondRemovalAttempts map[string]int
}

func (a *agent) run(ctx context.Context) error {
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
	a.devices = devices
	a.retryPendingPairingCleanup()
	a.adapters = make(map[string]struct{}, len(adapters))
	for _, path := range adapters {
		a.adapters[path] = struct{}{}
	}
	for path := range a.configuredAdapters {
		if _, ok := a.adapters[path]; !ok {
			delete(a.configuredAdapters, path)
		}
	}
	for _, path := range adapters {
		a.configure(ctx, path)
	}
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
		a.bluezOwner = newOwner
		a.registeredOwner = ""
		a.adapters = map[string]struct{}{}
		a.devices = map[string]devInfo{}
		a.configuredAdapters = map[string]struct{}{}
		a.pendingPairingCancel = map[string]struct{}{}
		a.resetStaleBondRemovalClaims()
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
		if _, ok := ifaces[adapterIface]; ok {
			a.adapters[path] = struct{}{}
			a.configure(ctx, path)
		}
		if props, ok := ifaces[deviceIface]; ok {
			a.merge(path, props)
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
				delete(a.devices, path)
			case adapterIface:
				delete(a.adapters, path)
				delete(a.configuredAdapters, path)
			}
		}
	case "PropertiesChanged":
		iface, props, err := decodeChanged(msg.Body)
		if err != nil {
			a.logf("changed", "bluetooth: PropertiesChanged: %v", err)
			return nil
		}
		if iface == deviceIface && msg.Path != "" {
			a.merge(msg.Path, props)
			a.act(ctx, a.devices[msg.Path])
		}
		if iface == adapterIface && msg.Path != "" {
			if adapterNeedsSetup(props) {
				delete(a.configuredAdapters, msg.Path)
			}
			a.configure(ctx, msg.Path)
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
	err := a.callVoid(ctx, bluezDest, path, adapterIface, "StartDiscovery", "", nil, 10*time.Second)
	if err != nil && !isName(err, "org.bluez.Error.InProgress") {
		configured = false
		log.Printf("bluetooth: adapter setup failed %s start discovery: %v", path, err)
	}
	if configured {
		a.configuredAdapters[path] = struct{}{}
		if pairingPending {
			log.Printf("bluetooth: adapter setup succeeded %s (Pairable restoration pending, discovery active)", path)
		} else {
			log.Printf("bluetooth: adapter setup succeeded %s (Powered=true Pairable=false Discoverable=false, discovery active)", path)
		}
	}
}

func (a *agent) adapterHasPendingPairing(adapterPath string) bool {
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

func adapterNeedsSetup(props map[string]any) bool {
	if powered, ok := props["Powered"].(bool); ok && !powered {
		return true
	}
	if pairable, ok := props["Pairable"].(bool); ok && pairable {
		return true
	}
	if discoverable, ok := props["Discoverable"].(bool); ok && discoverable {
		return true
	}
	if discovering, ok := props["Discovering"].(bool); ok && !discovering {
		return true
	}
	return false
}

func (a *agent) act(ctx context.Context, dev devInfo) {
	what := decide(dev)
	if what == actionIgnore {
		return
	}
	if !a.attempts.Start(dev, time.Now(), what) {
		return
	}
	pairSucceeded := what != actionPair
	defer func() { a.attempts.Finish(dev.Path, pairSucceeded, time.Now()) }()
	if what == actionPair {
		log.Printf("bluetooth: pairing %s %s", dev.Path, dev.Name)
		if err := a.pairDevice(ctx, dev); err != nil {
			a.logf("pair:"+dev.Path, "bluetooth: pair %s: %v", dev.Path, err)
			return
		}
		pairSucceeded = true
		a.resetStaleBondRemoval(dev.Path)
	}
	if err := a.callVoid(ctx, bluezDest, dev.Path, propsIface, "Set", "ssv", bodyProp(deviceIface, "Trusted", "b", true, 0), 10*time.Second); err != nil {
		a.logf("trust:"+dev.Path, "bluetooth: trust %s: %v", dev.Path, err)
	}
	log.Printf("bluetooth: connecting %s %s", dev.Path, dev.Name)
	if err := a.callVoid(ctx, bluezDest, dev.Path, deviceIface, "Connect", "", nil, 35*time.Second); err != nil && !isName(err, "org.bluez.Error.AlreadyConnected") {
		if dev.Paired && isGamepad(dev) && isStaleBondError(err) && a.claimStaleBondRemoval(dev.Path) {
			adapterPath := path.Dir(dev.Path)
			log.Printf("bluetooth: removing stale bond for %s after missing-key error: %v", dev.Path, err)
			if removeErr := a.callVoid(ctx, bluezDest, adapterPath, adapterIface, "RemoveDevice", "o", bodyPath(dev.Path), 10*time.Second); removeErr != nil {
				a.releaseStaleBondRemoval(dev.Path)
				a.logf("remove-stale-bond:"+dev.Path, "bluetooth: remove stale bond %s: %v", dev.Path, removeErr)
			} else {
				a.attempts.AllowPairAfterBondRemoval(dev.Path)
			}
			return
		}
		a.logf("connect:"+dev.Path, "bluetooth: connect %s: %v", dev.Path, err)
		return
	}
}

// pairDevice enables the adapter's bondable state only while pairing an
// unpaired, policy-approved controller. BlueZ otherwise can complete a
// temporary pairing without persisting its link key. Restore the idle state
// even if Pair fails or the caller is cancelled.
func (a *agent) pairDevice(ctx context.Context, dev devInfo) error {
	return a.pairDeviceWithTimeout(ctx, dev, pairCallTimeout)
}

func (a *agent) pairDeviceWithTimeout(ctx context.Context, dev devInfo, timeout time.Duration) error {
	adapterPath := path.Dir(dev.Path)
	if err := a.setAdapterPairable(ctx, adapterPath, true); err != nil {
		enableErr := fmt.Errorf("enable adapter bondability: %w", err)
		if restoreErr := a.restoreAdapterPairable(adapterPath); restoreErr != nil {
			return errors.Join(enableErr, fmt.Errorf("restore adapter pairability: %w", restoreErr))
		}
		return enableErr
	}
	pairErr := a.callVoid(ctx, bluezDest, dev.Path, deviceIface, "Pair", "", nil, timeout)
	if pairErr != nil {
		var bluezErr *dbusError
		if !errors.As(pairErr, &bluezErr) {
			if cancelErr := a.cancelPairing(dev.Path); cancelErr != nil {
				if a.pendingPairingCancel == nil {
					a.pendingPairingCancel = map[string]struct{}{}
				}
				a.pendingPairingCancel[dev.Path] = struct{}{}
				delete(a.configuredAdapters, adapterPath)
				return errors.Join(pairErr, fmt.Errorf("cancel pairing before restoring adapter bondability: %w", cancelErr))
			}
		}
	}
	restoreErr := a.restoreAdapterPairable(adapterPath)
	if pairErr != nil {
		if restoreErr != nil {
			return errors.Join(pairErr, fmt.Errorf("restore adapter pairability: %w", restoreErr))
		}
		return pairErr
	}
	if restoreErr != nil {
		// Pair has already created the bond. Retry transient restoration errors,
		// but keep the successful pairing path moving even if BlueZ still refuses
		// to restore its idle state. The next adapter refresh will configure it
		// again because this entry is no longer marked configured.
		if retryErr := a.restoreAdapterPairable(adapterPath); retryErr != nil {
			restoreErr = errors.Join(restoreErr, retryErr)
		} else {
			return nil
		}
		delete(a.configuredAdapters, adapterPath)
		a.logf("restore-pairable:"+adapterPath, "bluetooth: restore adapter pairability %s after successful pairing: %v", adapterPath, restoreErr)
	}
	return nil
}

func (a *agent) restoreAdapterPairable(adapterPath string) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), pairCleanupTimeout)
	defer cancel()
	return a.setAdapterPairable(cleanupCtx, adapterPath, false)
}

func (a *agent) cancelPairing(devicePath string) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), pairCleanupTimeout)
	defer cancel()
	err := a.callVoid(cleanupCtx, bluezDest, devicePath, deviceIface, "CancelPairing", "", nil, pairCleanupTimeout)
	if isName(err, "org.bluez.Error.DoesNotExist") {
		return nil
	}
	return err
}

func (a *agent) setAdapterPairable(ctx context.Context, adapterPath string, pairable bool) error {
	return a.callVoid(ctx, bluezDest, adapterPath, propsIface, "Set", "ssv",
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
	if s, ok := props["Alias"].(string); ok && s != "" {
		dev.Name = s
	} else if s, ok := props["Name"].(string); ok && s != "" {
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
		dev.UUIDs = dev.UUIDs[:0]
		for _, item := range list {
			if s, ok := item.(string); ok {
				dev.UUIDs = append(dev.UUIDs, s)
			}
		}
	}
	a.devices[path] = dev
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

func decodeObjects(body []byte) (map[string]devInfo, []string, error) {
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
	var adapters []string
	for _, ent := range entries {
		pair, ok := ent.([]any)
		if !ok || len(pair) != 2 {
			return nil, nil, errors.New("managed object entry")
		}
		path, ok := pair[0].(string)
		if !ok {
			return nil, nil, errors.New("managed object path")
		}
		ifaces, err := ifaceMap(pair[1])
		if err != nil {
			return nil, nil, err
		}
		if _, ok := ifaces[adapterIface]; ok {
			adapters = append(adapters, path)
		}
		if props, ok := ifaces[deviceIface]; ok {
			dev := devInfo{Path: path}
			applyProps(&dev, props)
			devices[path] = dev
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
