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
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	systemSocket = "/var/run/dbus/system_bus_socket"
	agentPath    = "/org/fes/bluetooth/agent"
	agentCap     = "KeyboardDisplay"
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
)

type dbusError struct{ Name string }

func (e *dbusError) Error() string {
	if e.Name == "" {
		return "dbus error"
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
		bus:      bus,
		attempts: newAttempts(),
		devices:  map[string]devInfo{},
		adapters: map[string]struct{}{},
		logged:   map[string]time.Time{},
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
	bus      *bus
	attempts *attempts
	devices  map[string]devInfo
	adapters map[string]struct{}
	logged   map[string]time.Time
}

func (a *agent) run(ctx context.Context) error {
	if _, err := a.call(ctx, dbusDest, dbusPath, dbusIface, "Hello", "", nil, 10*time.Second); err != nil {
		return err
	}
	if err := a.addSignalMatches(ctx); err != nil {
		return err
	}
	if err := a.callVoid(ctx, bluezDest, "/org/bluez", "org.bluez.AgentManager1", "RegisterAgent", "os", bodyPathString(agentPath, agentCap), 10*time.Second); err != nil {
		return err
	}
	if err := a.callVoid(ctx, bluezDest, "/org/bluez", "org.bluez.AgentManager1", "RequestDefaultAgent", "o", bodyPath(agentPath), 10*time.Second); err != nil {
		return err
	}
	if err := a.refresh(ctx); err != nil {
		a.logf("refresh", "bluetooth: %v", err)
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
			if err := a.refresh(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				a.logf("refresh", "bluetooth: %v", err)
			}
		}
	}
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
	a.adapters = map[string]struct{}{}
	for _, path := range adapters {
		a.adapters[path] = struct{}{}
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
			a.configure(ctx, msg.Path)
		}
	}
	return nil
}

func (a *agent) configure(ctx context.Context, path string) {
	sets := []struct {
		name string
		body []byte
	}{
		{"Powered", bodyProp(adapterIface, "Powered", "b", true, 0)},
		// Pairable controls incoming requests; this agent initiates pairing.
		{"Pairable", bodyProp(adapterIface, "Pairable", "b", false, 0)},
		{"Discoverable", bodyProp(adapterIface, "Discoverable", "b", false, 0)},
	}
	for _, set := range sets {
		if err := a.callVoid(ctx, bluezDest, path, propsIface, "Set", "ssv", set.body, 10*time.Second); err != nil {
			a.logf("set:"+path+set.name, "bluetooth: set %s %s: %v", path, set.name, err)
		}
	}
	err := a.callVoid(ctx, bluezDest, path, adapterIface, "StartDiscovery", "", nil, 10*time.Second)
	if err != nil && !isName(err, "org.bluez.Error.InProgress") {
		a.logf("discover:"+path, "bluetooth: discovery %s: %v", path, err)
	}
}

func (a *agent) act(ctx context.Context, dev devInfo) {
	what := decide(dev)
	if what == actionIgnore {
		return
	}
	if !a.attempts.Start(dev.Path, time.Now(), what) {
		return
	}
	pairSucceeded := what != actionPair
	defer func() { a.attempts.Finish(dev.Path, pairSucceeded) }()
	if what == actionPair {
		log.Printf("bluetooth: pairing %s %s", dev.Path, dev.Name)
		if err := a.callVoid(ctx, bluezDest, dev.Path, deviceIface, "Pair", "", nil, 35*time.Second); err != nil {
			a.logf("pair:"+dev.Path, "bluetooth: pair %s: %v", dev.Path, err)
			return
		}
		pairSucceeded = true
	}
	if err := a.callVoid(ctx, bluezDest, dev.Path, propsIface, "Set", "ssv", bodyProp(deviceIface, "Trusted", "b", true, 0), 10*time.Second); err != nil {
		a.logf("trust:"+dev.Path, "bluetooth: trust %s: %v", dev.Path, err)
	}
	log.Printf("bluetooth: connecting %s %s", dev.Path, dev.Name)
	if err := a.callVoid(ctx, bluezDest, dev.Path, deviceIface, "Connect", "", nil, 35*time.Second); err != nil && !isName(err, "org.bluez.Error.AlreadyConnected") {
		a.logf("connect:"+dev.Path, "bluetooth: connect %s: %v", dev.Path, err)
		return
	}
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
		return nil, &dbusError{Name: reply.ErrorName}
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
	case "Release", "Cancel", "DisplayPinCode", "DisplayPasskey":
		return "", nil, ""
	case "RequestPinCode":
		path, err := bodyObject(call.Body)
		if err != nil || !a.attempts.Has(path) {
			return "", nil, "org.bluez.Error.Rejected"
		}
		e := &encoder{}
		e.str("0000")
		return "s", e.buf, ""
	case "RequestPasskey":
		path, err := bodyObject(call.Body)
		if err != nil || !a.attempts.Has(path) {
			return "", nil, "org.bluez.Error.Rejected"
		}
		e := &encoder{}
		e.u32(0)
		return "u", e.buf, ""
	case "RequestConfirmation", "RequestAuthorization":
		path, err := bodyObject(call.Body)
		if err != nil || !a.attempts.Has(path) {
			return "", nil, "org.bluez.Error.Rejected"
		}
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
