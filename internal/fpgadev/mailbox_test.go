package fpgadev

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	mailboxHelloWord = uint32(0xd3100000)
	mailboxStartWord = uint32(0xac100000)
	mailboxDoneWord  = uint32(0xd3130c00)
)

type mailboxFakeClock struct {
	now        time.Time
	advances   []time.Duration
	afterCalls []time.Duration
}

func newMailboxFakeClock() *mailboxFakeClock {
	return &mailboxFakeClock{now: time.Unix(100, 0)}
}

func (c *mailboxFakeClock) Now() time.Time { return c.now }

func (c *mailboxFakeClock) After(duration time.Duration) <-chan time.Time {
	c.afterCalls = append(c.afterCalls, duration)
	advance := duration
	if len(c.advances) != 0 {
		advance = c.advances[0]
		c.advances = c.advances[1:]
	}
	if advance < duration {
		panic(fmt.Sprintf("mailbox fake clock scripted advance %s shorter than requested wait %s", advance, duration))
	}
	c.now = c.now.Add(advance)
	ready := make(chan time.Time, 1)
	ready <- c.now
	return ready
}

func TestMailboxFakeClockRejectsShortScriptedAdvance(t *testing.T) {
	clock := newMailboxFakeClock()
	clock.advances = []time.Duration{mailboxPollInterval - time.Nanosecond}
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("fake clock accepted a scripted advance shorter than the requested wait")
		}
	}()
	clock.After(mailboxPollInterval)
}

type mailboxBlockingClock struct {
	called chan struct{}
}

func (c *mailboxBlockingClock) Now() time.Time { return time.Unix(100, 0) }

func (c *mailboxBlockingClock) After(time.Duration) <-chan time.Time {
	select {
	case <-c.called:
	default:
		close(c.called)
	}
	return make(chan time.Time)
}

type mailboxFakeRegisters struct {
	gpiValues        []uint32
	lastGPI          uint32
	gpiReads         int
	gpiErr           error
	gpiErrAt         int
	gpoInitial       uint32
	events           []string
	gpoWrites        []uint32
	gpoReads         int
	gpoReadbacks     []uint32
	gpoReadbackErr   error
	gpoReadbackErrAt int
	writeErr         error
	writeErrAt       int
	closeErr         error
	closeCalls       int
}

func (r *mailboxFakeRegisters) ReadGPI() (uint32, error) {
	r.events = append(r.events, "read GPI")
	r.gpiReads++
	if r.gpiErr != nil && (r.gpiErrAt == 0 || r.gpiReads == r.gpiErrAt) {
		return 0, r.gpiErr
	}
	if len(r.gpiValues) != 0 {
		r.lastGPI = r.gpiValues[0]
		r.gpiValues = r.gpiValues[1:]
	}
	return r.lastGPI, nil
}

func (r *mailboxFakeRegisters) WriteGPO(value uint32) error {
	r.events = append(r.events, "write GPO")
	call := len(r.gpoWrites) + 1
	if r.writeErr != nil && (r.writeErrAt == 0 || r.writeErrAt == call) {
		return r.writeErr
	}
	r.gpoWrites = append(r.gpoWrites, value)
	return nil
}

func (r *mailboxFakeRegisters) ReadGPO() (uint32, error) {
	r.events = append(r.events, "read GPO")
	r.gpoReads++
	if r.gpoReadbackErr != nil && (r.gpoReadbackErrAt == 0 || r.gpoReadbackErrAt == r.gpoReads) {
		return 0, r.gpoReadbackErr
	}
	if len(r.gpoReadbacks) != 0 {
		value := r.gpoReadbacks[0]
		r.gpoReadbacks = r.gpoReadbacks[1:]
		return value, nil
	}
	if len(r.gpoWrites) == 0 {
		return r.gpoInitial, nil
	}
	return r.gpoWrites[len(r.gpoWrites)-1], nil
}

func (r *mailboxFakeRegisters) Close() error {
	r.events = append(r.events, "close")
	r.closeCalls++
	return r.closeErr
}

func mailboxDataWord(sequence uint8, value byte) uint32 {
	return 0xd3110000 | uint32(sequence)<<8 | uint32(value)
}

func mailboxEndWord(sequence uint8) uint32 {
	return 0xd3120000 | uint32(sequence)<<8
}

func mailboxDoneWordFor(sequence uint8) uint32 {
	return 0xd3130000 | uint32(sequence)<<8
}

func mailboxAckWord(word uint32) uint32 {
	return (word & 0x00ffffff) | 0xac000000
}

func mailboxFixture(payload []byte) (*mailboxFakeRegisters, *mailboxFakeClock) {
	return mailboxFixtureFromSequence(payload, 0)
}

func mailboxFixtureFromSequence(payload []byte, startSequence uint8) (*mailboxFakeRegisters, *mailboxFakeClock) {
	values := []uint32{mailboxHelloWord, mailboxHelloWord}
	for sequence, value := range payload {
		dataSequence := startSequence + uint8(sequence)
		values = append(values, mailboxDataWord(dataSequence, value), mailboxDataWord(dataSequence, value))
	}
	endSequence := startSequence + uint8(len(payload))
	values = append(values, mailboxEndWord(endSequence), mailboxEndWord(endSequence))
	values = append(values, mailboxDoneWordFor(endSequence), mailboxDoneWordFor(endSequence), mailboxDoneWordFor(endSequence))
	return &mailboxFakeRegisters{gpoInitial: 0xdeadbeef, gpiValues: values}, newMailboxFakeClock()
}

func mailboxProductionFixture() (*mailboxFakeRegisters, *mailboxFakeClock) {
	return mailboxFixture([]byte("OSS FPGA OK\n"))
}

func TestMailboxCompleteProductionTranscript(t *testing.T) {
	regs, clock := mailboxProductionFixture()
	observation, err := RunMailbox(context.Background(), regs, clock)
	if err != nil {
		t.Fatalf("RunMailbox: %v", err)
	}
	if string(observation.Payload) != "OSS FPGA OK\n" {
		t.Fatalf("payload = %q", observation.Payload)
	}
	if observation.TerminalWord != mailboxDoneWord {
		t.Fatalf("terminal word = %#08x", observation.TerminalWord)
	}
	wantWrites := []uint32{0, mailboxStartWord}
	for sequence, value := range []byte("OSS FPGA OK\n") {
		wantWrites = append(wantWrites, mailboxAckWord(mailboxDataWord(uint8(sequence), value)))
	}
	wantWrites = append(wantWrites, mailboxAckWord(mailboxEndWord(12)))
	if !reflect.DeepEqual(regs.gpoWrites, wantWrites) {
		t.Fatalf("GPO writes = %#v, want %#v", regs.gpoWrites, wantWrites)
	}
	if regs.closeCalls != 1 {
		t.Fatalf("Close calls = %d, want 1", regs.closeCalls)
	}
	if len(regs.events) < 3 || !reflect.DeepEqual(regs.events[:3], []string{"write GPO", "read GPO", "read GPI"}) {
		t.Fatalf("initial register ordering = %v", regs.events)
	}
	if len(clock.afterCalls) < 1 || clock.afterCalls[0] != 10*time.Millisecond {
		t.Fatalf("poll calls = %v", clock.afterCalls)
	}
}

func TestMailboxProgressCallbackFiresOnlyAfterDurableProtocolBoundaries(t *testing.T) {
	regs, clock := mailboxProductionFixture()
	var phases []string
	var terminalAtDone uint32
	observation, err := runMailboxWithProgress(context.Background(), regs, clock, func(progress mailboxProgress) error {
		phases = append(phases, progress.Phase)
		if progress.Phase == mailboxProgressDone {
			terminalAtDone = progress.Observation.TerminalWord
			if progress.TerminalWord != mailboxDoneWord {
				t.Fatalf("done callback word = %#08x, want %#08x", progress.TerminalWord, mailboxDoneWord)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runMailboxWithProgress: %v", err)
	}
	want := []string{mailboxProgressHello, mailboxProgressData, mailboxProgressEnd, mailboxProgressDone}
	if !reflect.DeepEqual(phases, want) {
		t.Fatalf("progress phases = %v, want %v", phases, want)
	}
	if terminalAtDone != 0 {
		t.Fatalf("terminal word was promoted before done callback/store: %#08x", terminalAtDone)
	}
	if observation.TerminalWord != mailboxDoneWord {
		t.Fatalf("returned terminal word = %#08x, want %#08x", observation.TerminalWord, mailboxDoneWord)
	}
}

func TestMailboxProgressCallbackFailureReturnsPartialObservationWithoutPromotion(t *testing.T) {
	regs, clock := mailboxProductionFixture()
	wantErr := errors.New("done owner store failed")
	observation, err := runMailboxWithProgress(context.Background(), regs, clock, func(progress mailboxProgress) error {
		if progress.Phase == mailboxProgressDone {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("callback error = %v, want %v", err, wantErr)
	}
	if observation.TerminalWord != 0 {
		t.Fatalf("failed done callback promoted terminal word: %#08x", observation.TerminalWord)
	}
	if string(observation.Payload) != "OSS FPGA OK\n" {
		t.Fatalf("partial payload = %q", observation.Payload)
	}
	if regs.closeCalls != 1 {
		t.Fatalf("Close calls = %d, want 1", regs.closeCalls)
	}
}

func TestMailboxProgressReportsFirstAcceptedDataBeforeItsACK(t *testing.T) {
	regs, clock := mailboxProductionFixture()
	ackErr := errors.New("DATA ACK failed")
	regs.writeErr = ackErr
	regs.writeErrAt = 3 // zero, START, then the first DATA ACK
	var phases []string
	writesAtData := 0
	observation, err := runMailboxWithProgress(context.Background(), regs, clock, func(progress mailboxProgress) error {
		phases = append(phases, progress.Phase)
		if progress.Phase == mailboxProgressData {
			writesAtData = len(regs.gpoWrites)
		}
		return nil
	})
	if !errors.Is(err, ackErr) {
		t.Fatalf("mailbox error = %v, want DATA ACK failure", err)
	}
	if !reflect.DeepEqual(phases, []string{mailboxProgressHello, mailboxProgressData}) {
		t.Fatalf("progress phases = %v, want HELLO then first DATA", phases)
	}
	if writesAtData != 2 {
		t.Fatalf("GPO writes before first DATA progress = %d, want zero and START only", writesAtData)
	}
	if !reflect.DeepEqual(observation.Payload, []byte{'O'}) || observation.TerminalWord != 0 {
		t.Fatalf("partial observation = %#v, want first byte with no terminal word", observation)
	}
}

func TestMailboxParameterizedSequenceWrapFrom255(t *testing.T) {
	payload := []byte{0xa5, 0x5a}
	regs, clock := mailboxFixtureFromSequence(payload, 255)
	observation, err := runMailboxWithPayloadFromSequence(context.Background(), regs, clock, payload, 255)
	if err != nil {
		t.Fatalf("sequence wrap transcript: %v", err)
	}
	if !reflect.DeepEqual(observation.Payload, payload) {
		t.Fatalf("payload = %#v, want %#v", observation.Payload, payload)
	}
	wantWrites := []uint32{
		0,
		mailboxStartWord,
		mailboxAckWord(mailboxDataWord(255, payload[0])),
		mailboxAckWord(mailboxDataWord(0, payload[1])),
		mailboxAckWord(mailboxEndWord(1)),
	}
	if !reflect.DeepEqual(regs.gpoWrites, wantWrites) {
		t.Fatalf("GPO writes = %#v, want %#v", regs.gpoWrites, wantWrites)
	}
}

func TestMailboxRejectsGPOWriteAndReadbackFailuresBeforeGPI(t *testing.T) {
	writeErr := errors.New("write GPO")
	readErr := errors.New("read GPO")
	for name, regs := range map[string]*mailboxFakeRegisters{
		"write":    {gpoInitial: 7, writeErr: writeErr},
		"read":     {gpoInitial: 7, gpoReadbackErr: readErr},
		"mismatch": {gpoInitial: 7, gpoReadbacks: []uint32{1}},
	} {
		name, regs := name, regs
		t.Run(name, func(t *testing.T) {
			_, err := RunMailbox(context.Background(), regs, newMailboxFakeClock())
			if err == nil {
				t.Fatal("GPO failure was accepted")
			}
			if regs.gpiReads != 0 {
				t.Fatalf("GPI reads = %d, want 0", regs.gpiReads)
			}
			if regs.closeCalls != 1 {
				t.Fatalf("Close calls = %d, want 1", regs.closeCalls)
			}
		})
	}
}

func TestMailboxRejectsStartWriteAndReadbackFailures(t *testing.T) {
	writeErr := errors.New("write START")
	readErr := errors.New("read START")
	for name, configure := range map[string]func(*mailboxFakeRegisters){
		"write": func(regs *mailboxFakeRegisters) {
			regs.writeErr = writeErr
			regs.writeErrAt = 2
		},
		"read": func(regs *mailboxFakeRegisters) {
			regs.gpoReadbackErr = readErr
			regs.gpoReadbackErrAt = 2
		},
		"mismatch": func(regs *mailboxFakeRegisters) {
			regs.gpoReadbacks = []uint32{0, 1}
		},
	} {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			regs, clock := mailboxProductionFixture()
			configure(regs)
			_, err := RunMailbox(context.Background(), regs, clock)
			if err == nil {
				t.Fatal("START failure was accepted")
			}
			if len(regs.gpoWrites) > 2 {
				t.Fatalf("writes after START failure = %#v", regs.gpoWrites)
			}
		})
	}
}

func TestMailboxRejectsDATAAndENDAcknowledgementFailures(t *testing.T) {
	production := []byte("OSS FPGA OK\n")
	tests := map[string]func(*mailboxFakeRegisters){
		"DATA write": func(regs *mailboxFakeRegisters) {
			regs.writeErr = errors.New("write DATA ACK")
			regs.writeErrAt = 3
		},
		"DATA readback error": func(regs *mailboxFakeRegisters) {
			regs.gpoReadbackErr = errors.New("read DATA ACK")
			regs.gpoReadbackErrAt = 3
		},
		"DATA readback mismatch": func(regs *mailboxFakeRegisters) {
			regs.gpoReadbacks = append([]uint32{0, mailboxStartWord, 1}, regs.gpoReadbacks...)
		},
		"END write": func(regs *mailboxFakeRegisters) {
			regs.writeErr = errors.New("write END ACK")
			regs.writeErrAt = 15
		},
		"END readback error": func(regs *mailboxFakeRegisters) {
			regs.gpoReadbackErr = errors.New("read END ACK")
			regs.gpoReadbackErrAt = 15
		},
		"END readback mismatch": func(regs *mailboxFakeRegisters) {
			readbacks := []uint32{0, mailboxStartWord}
			for index, value := range production {
				readbacks = append(readbacks, mailboxAckWord(mailboxDataWord(uint8(index), value)))
			}
			readbacks = append(readbacks, 1)
			regs.gpoReadbacks = readbacks
		},
	}
	for name, configure := range tests {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			regs, clock := mailboxProductionFixture()
			configure(regs)
			_, err := RunMailbox(context.Background(), regs, clock)
			if err == nil {
				t.Fatal("acknowledgement failure was accepted")
			}
			if regs.closeCalls != 1 {
				t.Fatalf("Close calls = %d, want 1", regs.closeCalls)
			}
			if len(regs.gpoWrites) > 15 {
				t.Fatalf("writes after acknowledgement failure = %#v", regs.gpoWrites)
			}
		})
	}
}

func TestMailboxPropagatesGPIReadErrorsAndCloses(t *testing.T) {
	readErr := errors.New("read GPI")
	for _, readNumber := range []int{1, 2} {
		regs, clock := mailboxProductionFixture()
		regs.gpiErr = readErr
		regs.gpiErrAt = readNumber
		_, err := RunMailbox(context.Background(), regs, clock)
		if !errors.Is(err, readErr) {
			t.Fatalf("GPI read %d error = %v, want %v", readNumber, err, readErr)
		}
		if regs.closeCalls != 1 {
			t.Fatalf("GPI read %d Close calls = %d, want 1", readNumber, regs.closeCalls)
		}
	}
}

func TestMailboxRejectsIndependentWordFieldMutationsWithoutACK(t *testing.T) {
	payload := []byte("x")
	tests := map[string]func([]uint32) []uint32{
		"hello signature": func(values []uint32) []uint32 { values[0] ^= 0x01000000; values[1] = values[0]; return values },
		"hello version":   func(values []uint32) []uint32 { values[0] ^= 0x00100000; values[1] = values[0]; return values },
		"hello opcode":    func(values []uint32) []uint32 { values[0] ^= 0x00010000; values[1] = values[0]; return values },
		"hello sequence":  func(values []uint32) []uint32 { values[0] ^= 0x00000100; values[1] = values[0]; return values },
		"hello byte":      func(values []uint32) []uint32 { values[0] ^= 0x00000001; values[1] = values[0]; return values },
		"data signature": func(values []uint32) []uint32 {
			values[2] ^= 0x01000000
			values[3] = values[2]
			return values
		},
		"data version": func(values []uint32) []uint32 {
			values[2] ^= 0x00100000
			values[3] = values[2]
			return values
		},
		"data opcode": func(values []uint32) []uint32 {
			values[2] ^= 0x00010000
			values[3] = values[2]
			return values
		},
		"data sequence": func(values []uint32) []uint32 {
			values[2] ^= 0x00000100
			values[3] = values[2]
			return values
		},
		"data byte": func(values []uint32) []uint32 {
			values[2] ^= 0x00000001
			values[3] = values[2]
			return values
		},
		"end signature": func(values []uint32) []uint32 {
			values[4] ^= 0x01000000
			values[5] = values[4]
			return values
		},
		"end version": func(values []uint32) []uint32 {
			values[4] ^= 0x00100000
			values[5] = values[4]
			return values
		},
		"end opcode": func(values []uint32) []uint32 {
			values[4] ^= 0x00010000
			values[5] = values[4]
			return values
		},
		"end sequence": func(values []uint32) []uint32 {
			values[4] ^= 0x00000100
			values[5] = values[4]
			return values
		},
		"end byte": func(values []uint32) []uint32 {
			values[4] ^= 0x00000001
			values[5] = values[4]
			return values
		},
		"done signature": func(values []uint32) []uint32 {
			values[6] ^= 0x01000000
			values[7] = values[6]
			return values
		},
		"done version": func(values []uint32) []uint32 {
			values[6] ^= 0x00100000
			values[7] = values[6]
			return values
		},
		"done opcode": func(values []uint32) []uint32 {
			values[6] ^= 0x00010000
			values[7] = values[6]
			return values
		},
		"done sequence": func(values []uint32) []uint32 {
			values[6] ^= 0x00000100
			values[7] = values[6]
			return values
		},
		"done byte": func(values []uint32) []uint32 {
			values[6] ^= 0x00000001
			values[7] = values[6]
			return values
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			regs, clock := mailboxFixture(payload)
			values := append([]uint32(nil), regs.gpiValues...)
			regs.gpiValues = mutate(values)
			_, err := runMailboxWithPayload(context.Background(), regs, clock, []byte("x"))
			if err == nil {
				t.Fatal("mutated word was accepted")
			}
			wantWrites := []uint32{uint32(0)}
			if !strings.HasPrefix(name, "hello") {
				wantWrites = append(wantWrites, mailboxStartWord)
			}
			if strings.HasPrefix(name, "end") || strings.HasPrefix(name, "done") {
				wantWrites = append(wantWrites, mailboxAckWord(mailboxDataWord(0, 'x')))
			}
			if strings.HasPrefix(name, "done") {
				wantWrites = append(wantWrites, mailboxAckWord(mailboxEndWord(1)))
			}
			if !reflect.DeepEqual(regs.gpoWrites, wantWrites) {
				t.Fatalf("GPO writes = %#v, want prefix %#v", regs.gpoWrites, wantWrites)
			}
		})
	}
}

func TestMailboxRejectsUnstableDoubleSampleWithoutACK(t *testing.T) {
	regs, clock := mailboxProductionFixture()
	regs.gpiValues[1] ^= 0x00000001
	_, err := RunMailbox(context.Background(), regs, clock)
	if err == nil || !strings.Contains(err.Error(), "stable") {
		t.Fatalf("unstable HELLO error = %v", err)
	}
	if len(regs.gpoWrites) != 1 {
		t.Fatalf("GPO writes = %#v, want clear only", regs.gpoWrites)
	}
}

func TestMailboxRejectsUnstableDATAENDAndDONESamplesWithoutACK(t *testing.T) {
	tests := map[string]int{
		"DATA": 3,
		"END":  5,
		"DONE": 7,
	}
	for name, secondIndex := range tests {
		name, secondIndex := name, secondIndex
		t.Run(name, func(t *testing.T) {
			regs, clock := mailboxFixture([]byte("x"))
			regs.gpiValues[secondIndex] ^= 0x00000001
			_, err := runMailboxWithPayload(context.Background(), regs, clock, []byte("x"))
			if err == nil || !strings.Contains(err.Error(), "unstable") {
				t.Fatalf("unstable %s error = %v", name, err)
			}
			wantWrites := []uint32{0, mailboxStartWord}
			if name == "END" || name == "DONE" {
				wantWrites = append(wantWrites, mailboxAckWord(mailboxDataWord(0, 'x')))
			}
			if name == "DONE" {
				wantWrites = append(wantWrites, mailboxAckWord(mailboxEndWord(1)))
			}
			if !reflect.DeepEqual(regs.gpoWrites, wantWrites) {
				t.Fatalf("GPO writes = %#v, want %#v", regs.gpoWrites, wantWrites)
			}
		})
	}
}

func TestMailboxRejectsDuplicateSkippedEarlyAndMismatchedWordsWithoutACK(t *testing.T) {
	tests := map[string]func([]uint32){
		"duplicate": func(values []uint32) {
			values[4] = mailboxDataWord(0, 'x')
			values[5] = values[4]
		},
		"skipped": func(values []uint32) {
			values[4] = mailboxDataWord(2, 'x')
			values[5] = values[4]
		},
		"early END": func(values []uint32) {
			values[2] = mailboxEndWord(0)
			values[3] = values[2]
		},
		"early DONE": func(values []uint32) {
			values[2] = mailboxDoneWordFor(0)
			values[3] = values[2]
		},
		"payload mismatch": func(values []uint32) {
			values[2] = mailboxDataWord(0, 'y')
			values[3] = values[2]
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			regs, clock := mailboxFixture([]byte("x"))
			values := append([]uint32(nil), regs.gpiValues...)
			mutate(values)
			regs.gpiValues = values
			_, err := runMailboxWithPayload(context.Background(), regs, clock, []byte("x"))
			if err == nil {
				t.Fatal("invalid word was accepted")
			}
			wantWrites := []uint32{0, mailboxStartWord}
			if name == "duplicate" || name == "skipped" {
				wantWrites = append(wantWrites, mailboxAckWord(mailboxDataWord(0, 'x')))
			}
			if !reflect.DeepEqual(regs.gpoWrites, wantWrites) {
				t.Fatalf("GPO writes = %#v, want prefix %#v", regs.gpoWrites, wantWrites)
			}
		})
	}
}

func TestMailboxRejectsEndAndDoneChangesAfterStablePair(t *testing.T) {
	tests := map[string][]uint32{
		"end unstable":      {mailboxHelloWord, mailboxHelloWord, mailboxDataWord(0, 'x'), mailboxDataWord(0, 'x'), mailboxEndWord(1), mailboxEndWord(1) ^ 1},
		"done unstable":     {mailboxHelloWord, mailboxHelloWord, mailboxDataWord(0, 'x'), mailboxDataWord(0, 'x'), mailboxEndWord(1), mailboxEndWord(1), mailboxDoneWordFor(1), mailboxDoneWordFor(1) ^ 1},
		"done changed hold": {mailboxHelloWord, mailboxHelloWord, mailboxDataWord(0, 'x'), mailboxDataWord(0, 'x'), mailboxEndWord(1), mailboxEndWord(1), mailboxDoneWordFor(1), mailboxDoneWordFor(1), mailboxDataWord(0, 'x')},
	}
	for name, values := range tests {
		name, values := name, values
		t.Run(name, func(t *testing.T) {
			regs := &mailboxFakeRegisters{gpoInitial: 1, gpiValues: values}
			_, err := runMailboxWithPayload(context.Background(), regs, newMailboxFakeClock(), []byte("x"))
			if err == nil {
				t.Fatal("changed terminal word was accepted")
			}
			wantWrites := []uint32{0, mailboxStartWord, mailboxAckWord(mailboxDataWord(0, 'x'))}
			if name != "end unstable" {
				wantWrites = append(wantWrites, mailboxAckWord(mailboxEndWord(1)))
			}
			if !reflect.DeepEqual(regs.gpoWrites, wantWrites) {
				t.Fatalf("GPO writes = %#v, want %#v", regs.gpoWrites, wantWrites)
			}
		})
	}
}

func TestMailboxAccepts256BytePayloadAndRejects257thByte(t *testing.T) {
	payload := make([]byte, 256)
	for index := range payload {
		payload[index] = byte(index)
	}
	regs, clock := mailboxFixture(payload)
	observation, err := runMailboxWithPayload(context.Background(), regs, clock, payload)
	if err != nil {
		t.Fatalf("wrap transcript: %v", err)
	}
	if !reflect.DeepEqual(observation.Payload, payload) {
		t.Fatal("wrap payload changed")
	}
	found255 := false
	for _, value := range regs.gpoWrites {
		if value == mailboxAckWord(mailboxDataWord(255, 255)) {
			found255 = true
		}
	}
	if !found255 {
		t.Fatalf("final 256-byte DATA ACK missing in %#v", regs.gpoWrites)
	}

	overflowPayload := make([]byte, 257)
	for index := range overflowPayload {
		overflowPayload[index] = byte(index)
	}
	regs, clock = mailboxFixture(overflowPayload)
	_, err = runMailboxWithPayload(context.Background(), regs, clock, overflowPayload)
	if err == nil || !strings.Contains(err.Error(), "256") {
		t.Fatalf("257th byte error = %v", err)
	}
	if len(regs.gpoWrites) != 2+256 {
		t.Fatalf("257th byte ACK count = %d, want %d", len(regs.gpoWrites), 2+256)
	}
}

func TestMailboxTimeoutsAreIndependentAndBounded(t *testing.T) {
	t.Run("hello", func(t *testing.T) {
		regs, clock := mailboxProductionFixture()
		clock.advances = []time.Duration{2*time.Second + time.Nanosecond}
		_, err := RunMailbox(context.Background(), regs, clock)
		if err == nil || !strings.Contains(err.Error(), "HELLO") {
			t.Fatalf("HELLO timeout = %v", err)
		}
		if len(regs.gpoWrites) != 1 {
			t.Fatalf("HELLO timeout writes = %#v", regs.gpoWrites)
		}
	})

	for _, name := range []string{"DATA", "END", "DONE"} {
		name := name
		t.Run(name, func(t *testing.T) {
			payload := []byte("x")
			regs, clock := mailboxFixture(payload)
			// The first wait is HELLO; the second is DATA, then END, then DONE.
			stage := map[string]int{"DATA": 1, "END": 2, "DONE": 3}[name]
			clock.advances = make([]time.Duration, stage+1)
			for index := range clock.advances {
				clock.advances[index] = 10 * time.Millisecond
			}
			clock.advances[stage] = time.Second + time.Nanosecond
			_, err := runMailboxWithPayload(context.Background(), regs, clock, payload)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s timeout = %v", name, err)
			}
		})
	}

	t.Run("total", func(t *testing.T) {
		payload := make([]byte, 256)
		regs, clock := mailboxFixture(payload)
		clock.advances = make([]time.Duration, 256)
		for index := range clock.advances {
			clock.advances[index] = 60 * time.Millisecond
		}
		_, err := runMailboxWithPayload(context.Background(), regs, clock, payload)
		if err == nil || !strings.Contains(err.Error(), "total") {
			t.Fatalf("total timeout = %v", err)
		}
	})
}

func TestMailboxHelloDeadlineBoundaries(t *testing.T) {
	for _, test := range []struct {
		name    string
		advance time.Duration
		wantErr bool
	}{
		{name: "just inside", advance: mailboxHelloTimeout - time.Nanosecond},
		{name: "equality", advance: mailboxHelloTimeout, wantErr: true},
		{name: "one nanosecond beyond", advance: mailboxHelloTimeout + time.Nanosecond, wantErr: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			regs, clock := mailboxProductionFixture()
			clock.advances = []time.Duration{test.advance}
			_, err := RunMailbox(context.Background(), regs, clock)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "HELLO") {
					t.Fatalf("HELLO boundary error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("HELLO just-inside error = %v", err)
			}
		})
	}
}

func TestMailboxAdvanceDeadlineBoundaries(t *testing.T) {
	for _, state := range []string{"DATA", "END", "DONE"} {
		state := state
		tests := []struct {
			name    string
			advance time.Duration
			hold    time.Duration
			wantErr bool
		}{
			{name: "just inside", advance: mailboxAdvanceTimeout - time.Nanosecond},
			{name: "equality", advance: mailboxAdvanceTimeout, wantErr: true},
			{name: "one nanosecond beyond", advance: mailboxAdvanceTimeout + time.Nanosecond, wantErr: true},
		}
		if state == "DONE" {
			tests = []struct {
				name    string
				advance time.Duration
				hold    time.Duration
				wantErr bool
			}{
				{name: "just inside", advance: mailboxAdvanceTimeout - mailboxPollInterval - time.Nanosecond, hold: mailboxPollInterval},
				{name: "equality", advance: mailboxAdvanceTimeout - mailboxPollInterval, hold: mailboxPollInterval, wantErr: true},
				{name: "one nanosecond beyond", advance: mailboxAdvanceTimeout - mailboxPollInterval + time.Nanosecond, hold: mailboxPollInterval, wantErr: true},
			}
		}
		for _, test := range tests {
			test := test
			t.Run(state+"/"+test.name, func(t *testing.T) {
				regs, clock := mailboxFixture([]byte("x"))
				stage := map[string]int{"DATA": 1, "END": 2, "DONE": 3}[state]
				clock.advances = make([]time.Duration, 5)
				for index := range clock.advances {
					clock.advances[index] = mailboxPollInterval
				}
				clock.advances[stage] = test.advance
				if state == "DONE" {
					clock.advances[4] = test.hold
				}
				_, err := runMailboxWithPayload(context.Background(), regs, clock, []byte("x"))
				if test.wantErr {
					if err == nil || !strings.Contains(err.Error(), state) {
						t.Fatalf("%s boundary error = %v", state, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("%s just-inside error = %v", state, err)
				}
			})
		}
	}
}

func TestMailboxDoneHoldHonorsAdvanceAndTotalDeadlines(t *testing.T) {
	t.Run("DONE advance", func(t *testing.T) {
		for _, test := range []struct {
			name         string
			stageAdvance time.Duration
			hold         time.Duration
			finalDelta   time.Duration
			wantErr      bool
		}{
			{name: "just inside", stageAdvance: mailboxAdvanceTimeout - mailboxPollInterval - time.Nanosecond, hold: mailboxPollInterval, finalDelta: -time.Nanosecond},
			{name: "equality", stageAdvance: mailboxAdvanceTimeout - mailboxPollInterval, hold: mailboxPollInterval, wantErr: true},
			{name: "one nanosecond beyond", stageAdvance: mailboxAdvanceTimeout - mailboxPollInterval + time.Nanosecond, hold: mailboxPollInterval, finalDelta: time.Nanosecond, wantErr: true},
		} {
			test := test
			t.Run(test.name, func(t *testing.T) {
				regs, clock := mailboxFixture([]byte("x"))
				start := clock.Now()
				clock.advances = []time.Duration{
					mailboxPollInterval,
					mailboxPollInterval,
					mailboxPollInterval,
					test.stageAdvance,
					test.hold,
				}
				_, err := runMailboxWithPayload(context.Background(), regs, clock, []byte("x"))
				wantNow := start.Add(3*mailboxPollInterval + mailboxAdvanceTimeout + test.finalDelta)
				if !clock.Now().Equal(wantNow) {
					t.Fatalf("clock now = %s, want %s", clock.Now(), wantNow)
				}
				if test.wantErr {
					if err == nil || !strings.Contains(err.Error(), "DONE") {
						t.Fatalf("DONE hold boundary error = %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("DONE hold just-inside error = %v", err)
				}
			})
		}
	})

	t.Run("total", func(t *testing.T) {
		payload := []byte("123456789")
		for _, test := range []struct {
			name        string
			doneAdvance time.Duration
			hold        time.Duration
			finalDelta  time.Duration
			wantErr     bool
		}{
			{name: "just inside", doneAdvance: 500*time.Millisecond - mailboxPollInterval - time.Nanosecond, hold: mailboxPollInterval, finalDelta: -time.Nanosecond},
			{name: "equality", doneAdvance: 500*time.Millisecond - mailboxPollInterval, hold: mailboxPollInterval, wantErr: true},
			{name: "one nanosecond beyond", doneAdvance: 500*time.Millisecond - mailboxPollInterval + time.Nanosecond, hold: mailboxPollInterval, finalDelta: time.Nanosecond, wantErr: true},
		} {
			test := test
			t.Run(test.name, func(t *testing.T) {
				regs, clock := mailboxFixture(payload)
				start := clock.Now()
				clock.advances = make([]time.Duration, len(payload)+4)
				clock.advances[0] = mailboxPollInterval
				for index := 1; index <= len(payload)+1; index++ {
					clock.advances[index] = 950 * time.Millisecond
				}
				clock.advances[len(payload)+2] = test.doneAdvance
				clock.advances[len(clock.advances)-1] = test.hold
				_, err := runMailboxWithPayload(context.Background(), regs, clock, payload)
				wantNow := start.Add(mailboxPollInterval + mailboxTotalTimeout + test.finalDelta)
				if !clock.Now().Equal(wantNow) {
					t.Fatalf("clock now = %s, want %s", clock.Now(), wantNow)
				}
				if test.wantErr {
					if err == nil || !strings.Contains(err.Error(), "total") {
						t.Fatalf("total hold boundary error = %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("total hold just-inside error = %v", err)
				}
			})
		}
	})
}

func TestMailboxContextCancellationClosesPromptly(t *testing.T) {
	regs, _ := mailboxProductionFixture()
	clock := &mailboxBlockingClock{called: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := RunMailbox(ctx, regs, clock)
		done <- result{err: err}
	}()
	select {
	case <-clock.called:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("mailbox did not reach the poll wait")
	}
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not return promptly")
	}
	if regs.closeCalls != 1 {
		t.Fatalf("Close calls = %d, want 1", regs.closeCalls)
	}
}

func TestMailboxPreservesPrimaryErrorWhenCloseFails(t *testing.T) {
	primary := errors.New("primary")
	closeErr := errors.New("close")
	regs, clock := mailboxProductionFixture()
	regs.writeErr = primary
	regs.writeErrAt = 1
	regs.closeErr = closeErr
	_, err := RunMailbox(context.Background(), regs, clock)
	if !errors.Is(err, primary) || !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want primary and close errors", err)
	}
}

func TestMailboxReturnsCloseErrorAfterSuccessfulTransaction(t *testing.T) {
	closeErr := errors.New("close")
	regs, clock := mailboxProductionFixture()
	regs.closeErr = closeErr
	observation, err := RunMailbox(context.Background(), regs, clock)
	if !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want %v", err, closeErr)
	}
	if string(observation.Payload) != "OSS FPGA OK\n" || observation.TerminalWord != mailboxDoneWord {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestMailboxNoDONEAcknowledgementAndExactENDAcknowledgement(t *testing.T) {
	regs, clock := mailboxProductionFixture()
	_, err := RunMailbox(context.Background(), regs, clock)
	if err != nil {
		t.Fatalf("RunMailbox: %v", err)
	}
	for _, value := range regs.gpoWrites {
		if value&0x000f0000 == 0x00030000 {
			t.Fatalf("DONE was acknowledged: %#08x", value)
		}
	}
	if got := regs.gpoWrites[len(regs.gpoWrites)-1]; got != mailboxAckWord(mailboxEndWord(12)) {
		t.Fatalf("END ACK = %#08x", got)
	}
}
