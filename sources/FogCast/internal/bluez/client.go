// SPDX-License-Identifier: GPL-3.0-or-later

package bluez

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultSystemSocket is the standard BlueZ D-Bus system bus Unix socket.
	DefaultSystemSocket = "/var/run/dbus/system_bus_socket"

	dbusDest     = "org.freedesktop.DBus"
	dbusPath     = "/org/freedesktop/DBus"
	dbusIface    = "org.freedesktop.DBus"
	bluezDest    = "org.bluez"
	adapterIface = "org.bluez.Adapter1"
	deviceIface  = "org.bluez.Device1"
	propsIface   = "org.freedesktop.DBus.Properties"
	objectsIface = "org.freedesktop.DBus.ObjectManager"

	defaultCallTimeout = 5 * time.Second
	defaultPairWindow  = 30 * time.Second
	hidUUID            = "00001124-0000-1000-8000-00805f9b34fb"
	hogUUID            = "00001812-0000-1000-8000-00805f9b34fb"
)

// Device describes one paired or connected BlueZ device for Settings › Bluetooth.
type Device struct {
	Address   string
	Name      string
	Kind      string
	Connected bool
}

// Status is a nonblocking snapshot of the local BlueZ adapter, pairing window,
// and paired/connected devices.
type Status struct {
	AdapterConnected bool
	AdapterName      string
	PairingActive    bool
	PairingRemaining time.Duration
	ConnectedCount   int
	Devices          []Device
}

// Config configures a Client monitoring the BlueZ D-Bus system bus.
type Config struct {
	SocketPath  string
	Dial        func(context.Context) (io.ReadWriteCloser, error)
	Now         func() time.Time
	CallTimeout time.Duration
}

// Client tracks BlueZ adapters/devices asynchronously and executes pairing and
// forget requests over D-Bus without blocking UI rendering.
type Client struct {
	cfg      Config
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	notifyCh chan struct{}

	opMu    sync.Mutex
	stateMu sync.RWMutex

	bus             *busConn
	raw             io.Closer
	bluezOwner      string
	adapters        map[string]adapterProps
	devices         map[string]deviceProps
	pairingActive   bool
	pairingAdapter  string
	pairingDeadline time.Time
	discoveryOwned  bool
}

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

func isDBusError(err error, name string) bool {
	var de *dbusError
	return errors.As(err, &de) && de.Name == name
}

type busConn struct {
	r     *bufio.Reader
	w     io.Writer
	wmu   sync.Mutex
	mu    sync.Mutex
	next  uint32
	wait  map[uint32]chan *message
	queue []*message
	kick  chan struct{}
	done  chan struct{}
	err   error
	once  sync.Once
}

func newBusConn(r *bufio.Reader, w io.Writer) *busConn {
	return &busConn{
		r:    r,
		w:    w,
		wait: map[uint32]chan *message{},
		kick: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

func (b *busConn) fail(err error) {
	b.once.Do(func() {
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		close(b.done)
	})
}

func (b *busConn) readLoop() {
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
		}
	}
}

func (b *busConn) call(ctx context.Context, msg *message, body []byte) (*message, error) {
	ch := make(chan *message, 1)
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

func (b *busConn) drain() []*message {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.queue
	b.queue = nil
	return out
}

func handshake(raw io.ReadWriteCloser, callTimeout time.Duration) (*busConn, error) {
	if d, ok := raw.(interface{ SetDeadline(time.Time) error }); ok {
		_ = d.SetDeadline(time.Now().Add(callTimeout))
		defer func() { _ = d.SetDeadline(time.Time{}) }()
	}
	r := bufio.NewReader(raw)
	token := hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))
	if _, err := io.WriteString(raw, "\x00AUTH EXTERNAL "+token+"\r\n"); err != nil {
		return nil, err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "OK") {
		return nil, fmt.Errorf("dbus auth: %s", line)
	}
	if _, err := io.WriteString(raw, "BEGIN\r\n"); err != nil {
		return nil, err
	}
	return newBusConn(r, raw), nil
}

// Start launches a background D-Bus watcher maintaining live BlueZ state.
func Start(ctx context.Context, cfg Config) *Client {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSystemSocket
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = defaultCallTimeout
	}
	if cfg.Dial == nil {
		socketPath := cfg.SocketPath
		cfg.Dial = func(dctx context.Context) (io.ReadWriteCloser, error) {
			var d net.Dialer
			return d.DialContext(dctx, "unix", socketPath)
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	c := &Client{
		cfg:      cfg,
		ctx:      cctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		notifyCh: make(chan struct{}, 1),
		adapters: map[string]adapterProps{},
		devices:  map[string]deviceProps{},
	}
	go c.run()
	return c
}

// Close stops background BlueZ watching and waits for exit.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.cancel()
	c.stateMu.Lock()
	if c.raw != nil {
		_ = c.raw.Close()
	}
	c.stateMu.Unlock()
	<-c.done
}

func (c *Client) now() time.Time {
	return c.cfg.Now()
}

func (c *Client) notify() {
	select {
	case c.notifyCh <- struct{}{}:
	default:
	}
}

func (c *Client) run() {
	defer close(c.done)
	backoff := 500 * time.Millisecond
	for {
		if c.ctx.Err() != nil {
			return
		}
		dialCtx, dialCancel := context.WithTimeout(c.ctx, c.cfg.CallTimeout)
		raw, err := c.cfg.Dial(dialCtx)
		dialCancel()
		if err != nil {
			c.clearAll()
			if !c.waitBackoff(backoff) {
				return
			}
			if backoff < 5*time.Second {
				backoff *= 2
				if backoff > 5*time.Second {
					backoff = 5 * time.Second
				}
			}
			continue
		}
		backoff = 500 * time.Millisecond
		_ = c.serveConn(raw)
		c.clearAll()
		if !c.waitBackoff(500 * time.Millisecond) {
			return
		}
	}
}

func (c *Client) waitBackoff(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Client) clearAll() {
	c.stateMu.Lock()
	c.bus = nil
	c.raw = nil
	c.bluezOwner = ""
	c.adapters = map[string]adapterProps{}
	c.devices = map[string]deviceProps{}
	c.pairingActive = false
	c.pairingAdapter = ""
	c.pairingDeadline = time.Time{}
	c.discoveryOwned = false
	c.stateMu.Unlock()
}

func (c *Client) serveConn(raw io.ReadWriteCloser) error {
	b, err := handshake(raw, c.cfg.CallTimeout)
	if err != nil {
		_ = raw.Close()
		return err
	}
	c.stateMu.Lock()
	c.bus = b
	c.raw = raw
	c.stateMu.Unlock()
	defer func() {
		c.stateMu.Lock()
		c.bus = nil
		c.raw = nil
		c.stateMu.Unlock()
		_ = raw.Close()
	}()

	go b.readLoop()

	if _, err := c.callBus(c.ctx, b, dbusDest, dbusPath, dbusIface, "Hello", "", nil); err != nil {
		return err
	}
	for _, rule := range []string{
		"type='signal',sender='org.bluez',path='/',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesAdded'",
		"type='signal',sender='org.bluez',path='/',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesRemoved'",
		"type='signal',sender='org.bluez',path_namespace='/org/bluez',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'",
		"type='signal',sender='org.freedesktop.DBus',path='/org/freedesktop/DBus',interface='org.freedesktop.DBus',member='NameOwnerChanged',arg0='org.bluez'",
	} {
		if _, err := c.callBus(c.ctx, b, dbusDest, dbusPath, dbusIface, "AddMatch", "s", bodyString(rule)); err != nil {
			return err
		}
	}

	reply, err := c.callBus(c.ctx, b, dbusDest, dbusPath, dbusIface, "GetNameOwner", "s", bodyString(bluezDest))
	if err == nil {
		if owner, decErr := decodeStringResult(reply.Body); decErr == nil && owner != "" {
			c.stateMu.Lock()
			c.bluezOwner = owner
			c.stateMu.Unlock()
			_ = c.refreshObjects(c.ctx, b)
		}
	} else if !isDBusError(err, "org.freedesktop.DBus.Error.NameHasNoOwner") {
		return err
	}

	var timer *time.Timer
	var timerCh <-chan time.Time
	resetTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timerCh = nil
		}
		c.stateMu.RLock()
		active, deadline := c.pairingActive, c.pairingDeadline
		c.stateMu.RUnlock()
		if !active {
			return
		}
		wait := deadline.Sub(c.now())
		if wait <= 0 {
			wait = time.Millisecond
		}
		if timer == nil {
			timer = time.NewTimer(wait)
		} else {
			timer.Reset(wait)
		}
		timerCh = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	resetTimer()

	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-b.done:
			return b.err
		case <-c.notifyCh:
			resetTimer()
		case <-timerCh:
			c.stateMu.RLock()
			active, deadline := c.pairingActive, c.pairingDeadline
			c.stateMu.RUnlock()
			if active && !c.now().Before(deadline) {
				_ = c.StopPairing()
			}
			resetTimer()
		case <-b.kick:
			for _, msg := range b.drain() {
				c.handleSignal(b, msg)
			}
			resetTimer()
		}
	}
}

func (c *Client) callBus(ctx context.Context, b *busConn, dest, objPath, iface, member, sig string, body []byte) (*message, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()
	msg := &message{Type: msgMethodCall, Dest: dest, Path: objPath, Iface: iface, Member: member, Signature: sig}
	reply, err := b.call(callCtx, msg, body)
	if err != nil {
		return nil, err
	}
	if reply.Type == msgError {
		errMsg, _ := decodeStringResult(reply.Body)
		return nil, &dbusError{Name: reply.ErrorName, Message: errMsg}
	}
	if reply.Type != msgMethodReturn {
		return nil, fmt.Errorf("dbus %s: unexpected reply type %d", member, reply.Type)
	}
	return reply, nil
}

func (c *Client) refreshObjects(ctx context.Context, b *busConn) error {
	reply, err := c.callBus(ctx, b, bluezDest, "/", objectsIface, "GetManagedObjects", "", nil)
	if err != nil {
		return err
	}
	adapters, devices, err := decodeManagedObjects(reply.Body)
	if err != nil {
		return err
	}
	c.stateMu.Lock()
	c.adapters = adapters
	c.devices = devices
	if c.pairingActive {
		if _, ok := c.adapters[c.pairingAdapter]; !ok {
			c.pairingActive = false
			c.pairingAdapter = ""
			c.pairingDeadline = time.Time{}
			c.discoveryOwned = false
		}
	}
	c.stateMu.Unlock()
	return nil
}

func (c *Client) handleSignal(b *busConn, msg *message) {
	switch msg.Member {
	case "NameOwnerChanged":
		name, _, newOwner, err := decodeNameOwnerChanged(msg.Body)
		if err != nil || name != bluezDest {
			return
		}
		c.stateMu.Lock()
		changed := newOwner != c.bluezOwner
		c.bluezOwner = newOwner
		if changed {
			c.adapters = map[string]adapterProps{}
			c.devices = map[string]deviceProps{}
			c.pairingActive = false
			c.pairingAdapter = ""
			c.pairingDeadline = time.Time{}
			c.discoveryOwned = false
		}
		c.stateMu.Unlock()
		if changed && newOwner != "" {
			_ = c.refreshObjects(c.ctx, b)
		}
	case "InterfacesAdded":
		objPath, ifaces, err := decodeAdded(msg.Body)
		if err != nil {
			return
		}
		c.stateMu.Lock()
		if props, ok := ifaces[adapterIface]; ok {
			ad := c.adapters[objPath]
			ad.Path = objPath
			applyAdapterProps(&ad, props)
			c.adapters[objPath] = ad
		}
		if props, ok := ifaces[deviceIface]; ok {
			dev := c.devices[objPath]
			dev.Path = objPath
			applyDeviceProps(&dev, props)
			c.devices[objPath] = dev
		}
		c.stateMu.Unlock()
	case "InterfacesRemoved":
		objPath, ifaces, err := decodeRemoved(msg.Body)
		if err != nil {
			return
		}
		c.stateMu.Lock()
		for _, iface := range ifaces {
			switch iface {
			case deviceIface:
				delete(c.devices, objPath)
			case adapterIface:
				delete(c.adapters, objPath)
				if c.pairingActive && c.pairingAdapter == objPath {
					c.pairingActive = false
					c.pairingAdapter = ""
					c.pairingDeadline = time.Time{}
					c.discoveryOwned = false
				}
			}
		}
		c.stateMu.Unlock()
	case "PropertiesChanged":
		iface, props, err := decodeChanged(msg.Body)
		if err != nil || msg.Path == "" {
			return
		}
		c.stateMu.Lock()
		switch iface {
		case adapterIface:
			if ad, ok := c.adapters[msg.Path]; ok {
				applyAdapterProps(&ad, props)
				c.adapters[msg.Path] = ad
			}
		case deviceIface:
			if dev, ok := c.devices[msg.Path]; ok {
				applyDeviceProps(&dev, props)
				c.devices[msg.Path] = dev
			}
		}
		c.stateMu.Unlock()
	}
}

func pickAdapterLocked(adapters map[string]adapterProps) (adapterProps, bool) {
	if len(adapters) == 0 {
		return adapterProps{}, false
	}
	paths := make([]string, 0, len(adapters))
	for p := range adapters {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if adapters[p].Powered {
			return adapters[p], true
		}
	}
	return adapters[paths[0]], true
}

// Status returns a nonblocking snapshot of the adapter, pairing state, and devices.
func (c *Client) Status() Status {
	if c == nil {
		return Status{}
	}
	now := c.now()
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	ad, hasAdapter := pickAdapterLocked(c.adapters)
	adapterConnected := c.bluezOwner != "" && hasAdapter
	adapterName := ""
	if adapterConnected {
		adapterName = strings.TrimSpace(ad.Name)
		if adapterName == "" {
			adapterName = "Bluetooth"
		}
	}

	pairingActive := c.pairingActive && adapterConnected
	var remaining time.Duration
	if pairingActive {
		remaining = c.pairingDeadline.Sub(now)
		if remaining <= 0 {
			pairingActive = false
			remaining = 0
		}
	}

	var outDevices []Device
	connectedCount := 0
	for _, dev := range c.devices {
		if !dev.Paired && !dev.Connected {
			continue
		}
		addr := strings.ToUpper(strings.TrimSpace(dev.Address))
		if addr == "" {
			continue
		}
		name := strings.TrimSpace(dev.Name)
		if dev.Connected {
			connectedCount++
		}
		outDevices = append(outDevices, Device{
			Address:   addr,
			Name:      name,
			Kind:      inferKind(name, dev.Modalias, dev.Class, dev.UUIDs),
			Connected: dev.Connected,
		})
	}
	sort.Slice(outDevices, func(i, j int) bool {
		if outDevices[i].Connected != outDevices[j].Connected {
			return outDevices[i].Connected
		}
		ni, nj := strings.ToLower(outDevices[i].Name), strings.ToLower(outDevices[j].Name)
		if ni != nj {
			return ni < nj
		}
		return outDevices[i].Address < outDevices[j].Address
	})

	return Status{
		AdapterConnected: adapterConnected,
		AdapterName:      adapterName,
		PairingActive:    pairingActive,
		PairingRemaining: remaining,
		ConnectedCount:   connectedCount,
		Devices:          outDevices,
	}
}

// StartPairing ensures adapter power and starts BlueZ discovery on this client's
// D-Bus session without altering Pairable or Discoverable managed by fes-bluetooth.
func (c *Client) StartPairing(duration time.Duration) error {
	if c == nil {
		return errors.New("bluetooth unavailable")
	}
	if duration <= 0 {
		duration = defaultPairWindow
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.stateMu.RLock()
	b := c.bus
	owner := c.bluezOwner
	ad, hasAdapter := pickAdapterLocked(c.adapters)
	c.stateMu.RUnlock()

	if b == nil || owner == "" || !hasAdapter {
		return errors.New("connect a Bluetooth adapter first")
	}
	if !ad.Powered {
		if _, err := c.callBus(c.ctx, b, bluezDest, ad.Path, propsIface, "Set", "ssv",
			bodyPropBool(adapterIface, "Powered", true)); err != nil {
			return fmt.Errorf("power Bluetooth adapter: %w", err)
		}
	}
	if _, err := c.callBus(c.ctx, b, bluezDest, ad.Path, adapterIface, "StartDiscovery", "", nil); err != nil && !isDBusError(err, "org.bluez.Error.InProgress") {
		return fmt.Errorf("start Bluetooth discovery: %w", err)
	}

	c.stateMu.Lock()
	if cur, ok := c.adapters[ad.Path]; ok {
		cur.Powered = true
		c.adapters[ad.Path] = cur
	}
	c.pairingActive = true
	c.pairingAdapter = ad.Path
	c.pairingDeadline = c.now().Add(duration)
	c.discoveryOwned = true
	c.stateMu.Unlock()
	c.notify()
	return nil
}

// StopPairing stops any active UI discovery session owned by this client.
func (c *Client) StopPairing() error {
	if c == nil {
		return nil
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.stateMu.Lock()
	b := c.bus
	adapterPath := c.pairingAdapter
	owned := c.discoveryOwned
	c.pairingActive = false
	c.pairingAdapter = ""
	c.pairingDeadline = time.Time{}
	c.discoveryOwned = false
	c.stateMu.Unlock()
	c.notify()

	if !owned || b == nil || adapterPath == "" {
		return nil
	}
	if _, err := c.callBus(c.ctx, b, bluezDest, adapterPath, adapterIface, "StopDiscovery", "", nil); err != nil &&
		!isDBusError(err, "org.bluez.Error.Failed") &&
		!isDBusError(err, "org.bluez.Error.DoesNotExist") &&
		!isDBusError(err, "org.freedesktop.DBus.Error.UnknownObject") {
		return fmt.Errorf("stop Bluetooth discovery: %w", err)
	}
	return nil
}

// ForgetDevice removes a paired device bond from BlueZ via Adapter1.RemoveDevice.
func (c *Client) ForgetDevice(address string) error {
	if c == nil {
		return errors.New("bluetooth unavailable")
	}
	want := strings.TrimSpace(address)
	if want == "" {
		return errors.New("bluetooth address is required")
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.stateMu.RLock()
	b := c.bus
	owner := c.bluezOwner
	var devPath, adapterPath string
	for p, dev := range c.devices {
		if strings.EqualFold(dev.Address, want) {
			devPath = p
			adapterPath = dev.Adapter
			break
		}
	}
	c.stateMu.RUnlock()

	if b == nil || owner == "" {
		return errors.New("bluetooth service unavailable")
	}
	if devPath == "" {
		return fmt.Errorf("bluetooth device %s not found", want)
	}
	if adapterPath == "" {
		adapterPath = path.Dir(devPath)
	}
	if _, err := c.callBus(c.ctx, b, bluezDest, adapterPath, adapterIface, "RemoveDevice", "o", bodyPath(devPath)); err != nil &&
		!isDBusError(err, "org.bluez.Error.DoesNotExist") {
		return fmt.Errorf("forget Bluetooth device: %w", err)
	}

	c.stateMu.Lock()
	delete(c.devices, devPath)
	c.stateMu.Unlock()
	return nil
}

func parseModaliasVIDPID(modalias string) (uint16, uint16, bool) {
	_, rest, ok := strings.Cut(strings.ToLower(modalias), ":v")
	if !ok || len(rest) < 9 || rest[4] != 'p' {
		return 0, 0, false
	}
	vid, err1 := strconv.ParseUint(rest[:4], 16, 16)
	pid, err2 := strconv.ParseUint(rest[5:9], 16, 16)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return uint16(vid), uint16(pid), true
}

func inferKind(name, modalias string, class uint32, uuids []string) string {
	if vid, pid, ok := parseModaliasVIDPID(modalias); ok {
		switch vid {
		case 0x054c:
			switch pid {
			case 0x0ce6, 0x0df2:
				return "DualSense"
			case 0x05c4, 0x09cc, 0x0ba0:
				return "DualShock 4"
			case 0x0268:
				return "DualShock 3"
			}
		case 0x057e:
			if pid == 0x2009 {
				return "Switch Pro"
			}
		case 0x045e:
			return "Xbox"
		case 0x2dc8:
			return "8BitDo"
		}
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "dualsense"):
		return "DualSense"
	case strings.Contains(lower, "dualshock 4") || strings.Contains(lower, "wireless controller"):
		return "DualShock 4"
	case strings.Contains(lower, "pro controller"):
		return "Switch Pro"
	case strings.Contains(lower, "xbox"):
		return "Xbox"
	case strings.Contains(lower, "8bitdo"):
		return "8BitDo"
	}
	major := (class >> 8) & 0x1f
	minor := (class >> 2) & 0x0f
	if major == 5 && (minor == 1 || minor == 2) {
		return "Gamepad"
	}
	for _, u := range uuids {
		lu := strings.ToLower(u)
		if lu == hidUUID || lu == hogUUID {
			return "Gamepad"
		}
	}
	return ""
}
