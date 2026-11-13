// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"encoding/binary"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"testing"
	"time"
)

type portModel struct {
	child   *node
	status  uint32
	resetAt time.Time
}
type node struct {
	device  *controller
	speed   uint32
	parent  *node
	port    uint32
	enabled bool
	ports   []*portModel
}
type hubBus struct {
	freezeFrame  bool
	controlDelay time.Duration

	root                       *node
	nodes                      []*node
	regs                       *controller
	current                    *node
	packet                     transaction
	tt                         *transaction
	ttNode                     *node
	ssFrame                    uint16
	splits, completes, fifoOUT int
	ssNAKs, nyets, intNAKs     int
	mode                       string
}

func keyboardNode(speed uint32) *node { return &node{device: newController(), speed: speed} }
func hubNode(speed uint32, children ...*node) *node {
	n := keyboardNode(speed)
	n.device.device[4] = 9
	n.device.device[6] = 0
	if speed == 0 {
		n.device.device[6] = 1
		n.device.device[7] = 64
	}
	n.device.configuration = []byte{9, 2, 25, 0, 1, 1, 0, 0xc0, 0, 9, 4, 0, 0, 1, 9, 0, 0, 0, 7, 5, 0x81, 3, 2, 0, 12}
	for i, child := range children {
		p := &portModel{child: child, status: 1 << 8}
		if child != nil {
			child.parent = n
			child.port = uint32(i + 1)
			p.status |= 1 | 1<<16
		}
		n.ports = append(n.ports, p)
	}
	n.device.classRequest = func(r *control) bool {
		if r.typ == 0xa0 && r.request == 6 && r.value == 0x2900 {
			size := 7 + 2*((len(n.ports)+8)/8)
			r.data = make([]byte, size)
			copy(r.data, []byte{byte(size), 0x29, byte(len(n.ports)), 1, 0, 10, 0})
			return true
		}
		if r.typ == 0xa0 && r.request == 0 {
			r.data = make([]byte, 4)
			return true
		}
		if r.typ == 0xa3 && r.request == 0 {
			if r.index == 0 || int(r.index) > len(n.ports) {
				panic("invalid hub port")
			}
			p := n.ports[r.index-1]
			if !p.resetAt.IsZero() && !n.device.tick.Before(p.resetAt) {
				p.status &^= 1 << 4
				p.status |= 2 | 1<<20
				p.resetAt = time.Time{}
				p.child.enabled = true
				p.child.device.address = 0
			}
			r.data = make([]byte, 4)
			binary.LittleEndian.PutUint32(r.data, p.status)
			return true
		}
		if r.typ == 0x23 && (r.request == 1 || r.request == 3) {
			if r.index == 0 || int(r.index) > len(n.ports) {
				panic("invalid feature port")
			}
			p := n.ports[r.index-1]
			if r.request == 3 && r.value == 8 {
				p.status |= 1 << 8
				return true
			}
			if r.request == 3 && r.value == 4 {
				if p.child == nil || p.status&1 == 0 {
					panic("reset absent child")
				}
				p.status |= 1 << 4
				p.status &^= 2
				p.resetAt = n.device.tick.Add(50 * time.Millisecond)
				p.status &^= 3 << 9
				if p.child.speed == 2 {
					p.status |= 1 << 9
				}
				if p.child.speed == 0 {
					p.status |= 1 << 10
				}
				return true
			}
			if r.request == 1 && r.value >= 16 && r.value <= 20 {
				p.status &^= 1 << r.value
				return true
			}
			panic("unexpected hub feature")
		}
		return false
	}
	return n
}
func newHubBus(root *node) *hubBus {
	b := &hubBus{root: root, regs: newController()}
	b.regs.regs[hprt] = root.speed<<17 | 5
	root.enabled = true
	var visit func(*node)
	visit = func(n *node) {
		b.nodes = append(b.nodes, n)
		for _, p := range n.ports {
			if p.child != nil {
				visit(p.child)
			}
		}
	}
	visit(root)
	return b
}
func (b *hubBus) Now() time.Time {
	if !b.regs.frozen {
		b.regs.tick = b.regs.tick.Add(5 * time.Microsecond)
	}
	return b.regs.tick
}
func (b *hubBus) Read32(r uint32) uint32 {
	if r == hfnum {
		if b.freezeFrame {
			return 0
		}
		return uint32(b.regs.tick.UnixNano()/int64(125*time.Microsecond)) & 0xffff
	}
	return b.regs.Read32(r)
}
func expectedRoute(n *node) splitRoute {
	child := n
	for p := n.parent; p != nil; p = p.parent {
		if p.speed == 0 {
			if n.speed == 0 {
				return splitRoute{}
			}
			return splitRoute{p.device.address, child.port}
		}
		child = p
	}
	return splitRoute{}
}
func (b *hubBus) selectNode(address uint32) *node {
	var found *node
	for _, n := range b.nodes {
		if n.enabled && n.device.address == address {
			if found != nil {
				panic("ambiguous default address")
			}
			found = n
		}
	}
	if found == nil {
		panic("transaction to an unaddressed device")
	}
	return found
}
func (b *hubBus) result(ints uint32) { b.regs.regs[hcint] = ints }
func (b *hubBus) execute(n *node, p transaction) {
	d := n.device
	d.tick = b.regs.tick
	d.active = p
	if p.in {
		if p.ep != 0 && b.intNAKs > 0 {
			b.intNAKs--
			b.result(1 << 4)
			return
		}
		d.receive()
	} else if p.size == 0 {
		d.finishControl()
		d.complete(nil, false)
	} else {
		if p.pid != 3 || len(p.setup) != 8 {
			panic("bad setup bytes")
		}
		d.setup(p.setup)
		d.complete(nil, false)
	}
	if d.request == nil && p.ep == 0 {
		b.regs.tick = b.regs.tick.Add(b.controlDelay)
	}
	b.regs.regs[hcint] = d.regs[hcint]
	d.regs[hcint] = 0
	b.regs.rx = append(b.regs.rx, d.rx...)
	d.rx = nil
	b.regs.words = append(b.regs.words, d.words...)
	d.words = nil
}
func (b *hubBus) startSplit() {
	if b.tt != nil {
		panic("SSPLIT replayed while TT owns packet")
	}
	p := b.packet
	if b.ssNAKs > 0 {
		b.ssNAKs--
		b.result(1 << 4)
		return
	}
	b.tt = &p
	b.ttNode = b.current
	b.splits++
	b.ssFrame = uint16(b.Read32(hfnum))
	if (b.regs.regs[hcchar]>>18)&3 == 3 {
		b.ssFrame++
	}
	if b.mode == "ss-complete" {
		b.result(1)
		return
	}
	b.result(1 << 5)
}
func (b *hubBus) Write32(r, v uint32) {
	if r == hcchar && v&channelEnable != 0 && v&channelDisable == 0 {
		if b.regs.regs[hcchar]&channelEnable != 0 {
			panic("channel reused before halt")
		}
		b.regs.regs[hcchar] = v
		b.packet = transaction{in: v&(1<<15) != 0, ep: v >> 11 & 15, pid: b.regs.regs[hctsiz] >> 29 & 3, addr: v >> 22 & 127, size: b.regs.regs[hctsiz] & 0x7ffff}
		b.current = b.selectNode(b.packet.addr)
		route := expectedRoute(b.current)
		s := b.regs.regs[hcsplit]
		if route.hub == 0 {
			if s != 0 {
				panic("split routed to HS/direct device")
			}
			if b.packet.in || b.packet.size == 0 {
				b.execute(b.current, b.packet)
			}
			return
		}
		if s&^(uint32(1<<16)) != 1<<31|3<<14|route.hub<<7|route.port {
			panic("wrong TT route/position")
		}
		if s&(1<<16) != 0 {
			b.completes++
			if b.tt == nil || b.ttNode != b.current {
				panic("CSPLIT without TT ownership")
			}
			if b.packet.pid != b.tt.pid || b.packet.in != b.tt.in || b.packet.addr != b.tt.addr || b.packet.ep != b.tt.ep {
				panic("CSPLIT changed packet")
			}
			if !b.packet.in && b.packet.size != 0 {
				panic("CSPLIT OUT repeated payload")
			}
			frame := uint16(b.Read32(hfnum))
			if (v>>18)&3 == 3 {
				frame++
			}
			if uint16(frame-b.ssFrame) < 2 || frame>>3 != b.ssFrame>>3 {
				panic("CSPLIT timing outside TT window")
			}
			if b.mode == "cs-ack" {
				b.result(1 << 5)
				return
			}
			if b.mode == "nyet-forever" || b.nyets > 0 {
				if b.nyets > 0 {
					b.nyets--
				}
				b.result(1 << 6)
				return
			}
			p := *b.tt
			b.tt = nil
			b.execute(b.current, p)
			return
		}
		if b.packet.in || b.packet.size == 0 {
			b.startSplit()
		}
		return
	}
	if r == fifo {
		b.fifoOUT++
		if b.regs.regs[hcsplit]&(1<<16) != 0 {
			panic("payload written to complete split")
		}
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], v)
		b.packet.setup = append(b.packet.setup, word[:]...)
		if len(b.packet.setup) == int(b.packet.size) {
			if b.regs.regs[hcsplit]&(1<<31) != 0 {
				b.startSplit()
			} else {
				b.execute(b.current, b.packet)
			}
		}
		return
	}
	b.regs.Write32(r, v)
}
func TestKeyboardBehindHighAndFullSpeedHubs(t *testing.T) {
	for _, rootSpeed := range []uint32{0, 1} {
		for _, childSpeed := range []uint32{1, 2} {
			key := keyboardNode(childSpeed)
			key.device.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
			b := newHubBus(hubNode(rootSpeed, nil, key))
			h, err := Open(b, Config{PHYClockHz: 60000000})
			if err != nil {
				t.Fatal(rootSpeed, childSpeed, err)
			}
			got := report(t, h)
			if !bytes.Equal(got, []byte{0, 0, 4, 0, 0, 0, 0, 0}) {
				t.Fatal(got)
			}
			if h.address != 2 || len(h.path) != 1 {
				t.Fatal("child not selected", h.address, h.path)
			}
			if rootSpeed == 0 && (b.splits == 0 || b.completes == 0 || b.regs.regs[hfir] != 7499) {
				t.Fatal("HS TT path not exercised")
			}
			if rootSpeed == 1 && (b.splits != 0 || b.regs.regs[hfir] != 59999) {
				t.Fatal("FS hub used splits")
			}
		}
	}
}
func TestNestedHubTranslatorRouteAndHealth(t *testing.T) {
	for _, middleSpeed := range []uint32{0, 1} {
		key := keyboardNode(2)
		key.device.reports = [][]byte{{0, 0, 5, 0, 0, 0, 0, 0}}
		middle := hubNode(middleSpeed, key)
		b := newHubBus(hubNode(0, middle))
		h, err := Open(b, Config{PHYClockHz: 60000000})
		if err != nil {
			t.Fatal(middleSpeed, err)
		}
		if h.address != 3 || len(h.path) != 2 {
			t.Fatal("nested keyboard missing")
		}
		report(t, h)
		// An ancestor change retires the source through asynchronous status polling.
		b.root.ports[0].status &^= 1
		b.regs.tick = b.regs.tick.Add(200 * time.Millisecond)
		failed := false
		for i := 0; i < 100; i++ {
			_, err = h.PollKeyboard()
			if err != nil {
				failed = true
				break
			}
		}
		if !failed {
			t.Fatal("disconnected ancestor was not retired")
		}
		accesses := b.regs.accesses
		if _, err = h.PollKeyboard(); err == nil || b.regs.accesses != accesses {
			t.Fatal("retired host touched hardware")
		}
	}
}
func TestSplitNYETAndNAKDoNotReplaySetupOrAdvanceReportToggle(t *testing.T) {
	key := keyboardNode(2)
	b := newHubBus(hubNode(0, key))
	b.nyets = 1
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	setupCount := len(key.device.requests)
	b.intNAKs = 1
	for i := 0; i < 10; i++ {
		if _, err = h.PollKeyboard(); err != nil {
			t.Fatal(err)
		}
	}
	if h.toggle != 0 || len(key.device.requests) != setupCount {
		t.Fatal("NAK advanced input or replayed setup")
	}
	key.device.reports = [][]byte{{0, 0, 6, 0, 0, 0, 0, 0}}
	report(t, h)
	if h.toggle != 2 {
		t.Fatal("successful report did not advance toggle")
	}
}

func TestHubAdmissionAndSplitFailuresRetireWithoutReports(t *testing.T) {
	for _, mode := range []string{"empty", "ambiguous", "bad-hub-length", "bad-hub-ports", "port-overcurrent", "bad-config", "ss-complete", "cs-ack", "nyet-forever"} {
		t.Run(mode, func(t *testing.T) {
			key := keyboardNode(2)
			root := hubNode(0, key)
			b := newHubBus(root)
			switch mode {
			case "empty":
				root.ports[0].status &^= 1
			case "ambiguous":
				root = hubNode(0, key, keyboardNode(1))
				b = newHubBus(root)
			case "bad-hub-length", "bad-hub-ports":
				original := root.device.classRequest
				root.device.classRequest = func(r *control) bool {
					ok := original(r)
					if r.request == 6 && r.value == 0x2900 {
						if mode == "bad-hub-length" {
							r.data[0]++
						} else {
							r.data[2] = 16
						}
					}
					return ok
				}
			case "port-overcurrent":
				root.ports[0].status |= 1 << 3
			case "bad-config":
				root.device.configuration[3] = 0xff
			default:
				b.mode = mode
			}
			h, err := Open(b, Config{PHYClockHz: 60000000})
			if err == nil || h == nil {
				t.Fatal("bad hub/split admitted", mode)
			}
			if key.device.configured {
				t.Fatal("input configured before topology admission")
			}
			accesses := b.regs.accesses
			for i := 0; i < 3; i++ {
				data, e := h.PollKeyboard()
				if e == nil || data != nil || b.regs.accesses != accesses {
					t.Fatal("failed host retried", mode)
				}
			}
		})
	}
}
func TestHubIgnoresNonKeyboardAndSelectsAlternateZeroSingleTT(t *testing.T) {
	mouse := keyboardNode(1)
	mouse.device.configuration[16] = 2
	key := keyboardNode(2)
	key.device.reports = [][]byte{{0, 0, 7, 0, 0, 0, 0, 0}}
	root := hubNode(0, mouse, key)
	root.device.device[6] = 2
	// Multi-TT hubs also expose the default single-TT alternate zero. Keep it.
	root.device.configuration = append(root.device.configuration, []byte{9, 4, 0, 1, 1, 9, 0, 2, 0, 7, 5, 0x81, 3, 2, 0, 12}...)
	root.device.configuration[2] = byte(len(root.device.configuration))
	b := newHubBus(root)
	b.ssNAKs = 1
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if mouse.device.configured || !key.device.configured || h.address != 3 {
		t.Fatal("non-keyboard/alternate selection")
	}
	report(t, h)
}
func TestSplitFrameWrapAndHubChangeAreBounded(t *testing.T) {
	key := keyboardNode(2)
	key.device.reports = [][]byte{{0, 0, 8, 0, 0, 0, 0, 0}}
	root := hubNode(0, key)
	b := newHubBus(root)
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	b.regs.tick = time.Unix(0, 65535*int64(125*time.Microsecond))
	// Keep the independent health timer in the same epoch for this wrap test.
	h.healthNext = b.regs.tick.Add(time.Second)
	report(t, h)
	root.ports[0].status |= 1 << 20
	b.regs.tick = b.regs.tick.Add(2 * time.Second)
	for i := 0; i < 100; i++ {
		if data, e := h.PollKeyboard(); e != nil {
			if data != nil {
				t.Fatal("report with changed path")
			}
			return
		}
	}
	t.Fatal("reset/change was not rejected")
}
func TestHubDepthLimitRejectsBeforeKeyboardConfiguration(t *testing.T) {
	key := keyboardNode(2)
	root := hubNode(0, hubNode(1, hubNode(1, hubNode(1, key))))
	b := newHubBus(root)
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err == nil || h == nil || key.device.configured {
		t.Fatal("hub depth budget bypassed", err)
	}
}

func TestSplitFrameStallHasFinitePollWorkAndNeverPublishesData(t *testing.T) {
	key := keyboardNode(2)
	key.device.reports = [][]byte{{0, 0, 10, 0, 0, 0, 0, 0}}
	b := newHubBus(hubNode(0, key))
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	b.freezeFrame = true
	h.healthNext = b.regs.tick.Add(time.Second)
	h.PollKeyboard()
	before := b.regs.accesses
	if data, err := h.PollKeyboard(); err != nil || data != nil {
		t.Fatal("stalled frame delivered input", data, err)
	}
	if b.regs.accesses-before > 4096*20 {
		t.Fatal("unbounded split poll")
	}
	b.regs.tick = b.regs.tick.Add(time.Second)
	if data, err := h.PollKeyboard(); err == nil || data != nil {
		t.Fatal("expired split was not retired")
	}
}
func TestDirectHighSpeedKeyboardUsesMicroframeInterval(t *testing.T) {
	key := keyboardNode(0)
	key.device.device[7] = 64
	key.device.configuration[33] = 4
	key.device.reports = [][]byte{{0, 0, 11, 0, 0, 0, 0, 0}}
	b := newHubBus(key)
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	if h.interval != time.Millisecond || b.regs.regs[hfir] != 7499 || b.splits != 0 {
		t.Fatal("incorrect HS interval")
	}
	report(t, h)
}
func TestHubConnectionBounceBeforeResetIsRejected(t *testing.T) {
	key := keyboardNode(2)
	root := hubNode(0, key)
	original := root.device.classRequest
	reads := 0
	root.device.classRequest = func(r *control) bool {
		result := original(r)
		if r.typ == 0xa3 && r.request == 0 {
			reads++
			if reads == 3 {
				r.data[2] |= 1
			}
		}
		return result
	}
	b := newHubBus(root)
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err == nil || h == nil || key.device.configured {
		t.Fatal("bouncing path admitted")
	}
}

func TestHubReportReachesSharedNormalizedInput(t *testing.T) {
	key := keyboardNode(2)
	key.device.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 0, 0, 0}}
	b := newHubBus(hubNode(0, key))
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	keyboard, err := input.NewUSBKeyboard(h)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		events, err := keyboard.Poll()
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Key == "a" {
				return
			}
		}
	}
	t.Fatal("hub report did not reach shared input decoder")
}

func TestHubWithoutTranslatorAndInvalidHSPacketIntervalReject(t *testing.T) {
	for _, mode := range []string{"no-tt", "zero-hub-endpoint", "bad-hs-interval"} {
		key := keyboardNode(2)
		root := hubNode(0, key)
		if mode == "no-tt" {
			root.device.device[6] = 0
		}
		if mode == "zero-hub-endpoint" {
			root.device.configuration[22] = 0
		}
		if mode == "bad-hs-interval" {
			key = keyboardNode(0)
			key.device.device[7] = 64
			key.device.configuration[33] = 17
			root = key
		}
		b := newHubBus(root)
		h, err := Open(b, Config{PHYClockHz: 60000000})
		if err == nil || h == nil || key.device.configured {
			t.Fatal("unsupported transfer was configured", mode, err)
		}
	}
}
func TestChangedRootSpeedRejectsBeforeAnotherChannelStart(t *testing.T) {
	b := newHubBus(hubNode(0, keyboardNode(2)))
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	count := b.splits
	b.regs.regs[hprt] |= 1 << 17
	if data, err := h.PollKeyboard(); err == nil || data != nil || b.splits != count {
		t.Fatal("changed root armed a packet", err)
	}
}

func TestEnumerationHasOneOverallDeadline(t *testing.T) {
	key := keyboardNode(0)
	key.device.device[7] = 64
	b := newHubBus(key)
	// Each request completes inside its 2s limit, but the whole enumeration does
	// not fit the independent 4s startup budget. It must never become a source.
	b.controlDelay = 600 * time.Millisecond
	h, err := Open(b, Config{PHYClockHz: 60000000})
	if err == nil || h == nil {
		t.Fatal("per-request deadlines bypassed overall startup budget")
	}
	before := b.regs.accesses
	if r, e := h.PollKeyboard(); e == nil || r != nil || b.regs.accesses != before {
		t.Fatal("expired startup produced input")
	}
}
