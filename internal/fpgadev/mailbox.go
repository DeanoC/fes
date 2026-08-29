package fpgadev

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Registers is the platform-neutral register surface used by the mailbox
// protocol. Implementations own the mapping for the lifetime of a call and
// must make Close safe on every exit path.
type Registers interface {
	ReadGPI() (uint32, error)
	WriteGPO(uint32) error
	ReadGPO() (uint32, error)
	Close() error
}

// Clock supplies deterministic time and waits to the protocol engine. The
// production caller uses the real clock; tests provide a fake without wall
// clock sleeps.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

// Observation is the immutable protocol result returned after a complete
// mailbox transaction. Payload is copied before return so callers cannot
// mutate protocol state after the mapping has been closed.
type Observation struct {
	Payload      []byte
	TerminalWord uint32
}

const (
	mailboxPollInterval   = 10 * time.Millisecond
	mailboxHelloTimeout   = 2 * time.Second
	mailboxAdvanceTimeout = 1 * time.Second
	mailboxTotalTimeout   = 10 * time.Second
	mailboxMaxPayload     = 256

	mailboxSignature    = uint8(0xd3)
	mailboxAckSignature = uint8(0xac)
	mailboxVersion      = uint8(1)

	mailboxOpcodeHello = uint8(0)
	mailboxOpcodeData  = uint8(1)
	mailboxOpcodeEnd   = uint8(2)
	mailboxOpcodeDone  = uint8(3)
)

var (
	errMailboxTimeout         = errors.New("mailbox timeout")
	errMailboxProtocol        = errors.New("mailbox protocol violation")
	errMailboxPayloadMismatch = errors.New("mailbox payload mismatch")
)

const mailboxProductionPayload = "OSS FPGA OK\n"

// RunMailbox executes the fixed protocol-v1 development transaction.
//
// The GPO clear and its exact readback happen before the first GPI read. Each
// GPI word is sampled twice with a 10 ms clock interval, and no acknowledgement
// is written until both samples and every field have passed validation.
func RunMailbox(ctx context.Context, regs Registers, clock Clock) (observation Observation, err error) {
	return runMailboxWithPayloadAndClock(ctx, regs, clock, []byte(mailboxProductionPayload))
}

// runMailboxWithPayload is a package-local protocol fixture seam. It keeps
// the production API fixed while allowing tests to exercise the defined
// modulo-256 sequence wrap independently of the 12-byte production message.
func runMailboxWithPayload(ctx context.Context, regs Registers, clock Clock, expectedPayload []byte) (observation Observation, err error) {
	return runMailboxWithPayloadAndClock(ctx, regs, clock, expectedPayload)
}

func runMailboxWithPayloadAndClock(ctx context.Context, regs Registers, clock Clock, expectedPayload []byte) (observation Observation, err error) {
	return runMailboxWithPayloadFromSequenceProgress(ctx, regs, clock, expectedPayload, 0, nil)
}

func runMailboxWithPayloadFromSequence(ctx context.Context, regs Registers, clock Clock, expectedPayload []byte, startSequence uint8) (observation Observation, err error) {
	return runMailboxWithPayloadFromSequenceProgress(ctx, regs, clock, expectedPayload, startSequence, nil)
}

// mailboxProgress is intentionally package-local.  The lifecycle runner uses
// it to persist only protocol boundaries that have actually completed; the
// public RunMailbox API remains unchanged.  Observation.TerminalWord is kept
// zero in the DONE callback until the callback (the durable owner store) has
// accepted the terminal evidence.
type mailboxProgress struct {
	Phase        string
	Observation  Observation
	TerminalWord uint32
}

const (
	mailboxProgressHello = "hello_observed"
	mailboxProgressData  = "message_partial"
	mailboxProgressEnd   = "end_ack_written"
	mailboxProgressDone  = "done_observed"
)

type mailboxProgressCallback func(mailboxProgress) error

// mailboxExecutionError preserves the independent provenance of the protocol
// operation and register cleanup while retaining the public RunMailbox error
// contract through multi-error unwrapping.
type mailboxExecutionError struct {
	operation error
	cleanup   error
}

func (e *mailboxExecutionError) Error() string {
	return errors.Join(e.operation, e.cleanup).Error()
}

func (e *mailboxExecutionError) Unwrap() []error {
	errs := make([]error, 0, 2)
	if e.operation != nil {
		errs = append(errs, e.operation)
	}
	if e.cleanup != nil {
		errs = append(errs, e.cleanup)
	}
	return errs
}

func splitMailboxExecutionError(err error) (operation, cleanup error) {
	var execution *mailboxExecutionError
	if errors.As(err, &execution) {
		return execution.operation, execution.cleanup
	}
	return err, nil
}

func runMailboxWithProgress(ctx context.Context, regs Registers, clock Clock, callback mailboxProgressCallback) (observation Observation, err error) {
	return runMailboxWithPayloadFromSequenceProgress(ctx, regs, clock, []byte(mailboxProductionPayload), 0, callback)
}

func runMailboxWithPayloadFromSequenceProgress(ctx context.Context, regs Registers, clock Clock, expectedPayload []byte, startSequence uint8, callback mailboxProgressCallback) (observation Observation, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if regs == nil {
		return Observation{}, errors.New("mailbox registers are nil")
	}
	if clock == nil {
		clock = mailboxRealClock{}
	}

	// Close is part of the mapping ownership contract. Preserve operation and
	// cleanup provenance internally; public callers still observe both causes.
	defer func() {
		closeErr := regs.Close()
		if closeErr == nil {
			return
		}
		wrapped := fmt.Errorf("close mailbox registers: %w", closeErr)
		err = &mailboxExecutionError{operation: err, cleanup: wrapped}
	}()

	if err := ctx.Err(); err != nil {
		return observation, err
	}

	if err := regs.WriteGPO(0); err != nil {
		return observation, fmt.Errorf("write GPO zero: %w", err)
	}
	zero, err := regs.ReadGPO()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		return observation, fmt.Errorf("read GPO zero readback: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return observation, err
	}
	if zero != 0 {
		return observation, fmt.Errorf("%w: GPO zero readback is %#08x", errMailboxProtocol, zero)
	}

	helloDeadline := clock.Now().Add(mailboxHelloTimeout)
	hello, err := readStableMailboxWord(ctx, regs, clock, helloDeadline, "HELLO")
	if err != nil {
		return observation, err
	}
	if err := validateHelloWord(hello); err != nil {
		return observation, err
	}
	if err := emitMailboxProgress(callback, mailboxProgress{Phase: mailboxProgressHello, Observation: cloneObservation(observation)}); err != nil {
		return observation, err
	}

	// The total transaction bound starts with the START write and is
	// independent of the HELLO qualification bound above.
	totalDeadline := clock.Now().Add(mailboxTotalTimeout)
	if err := ctx.Err(); err != nil {
		return observation, err
	}
	if err := regs.WriteGPO(startWord()); err != nil {
		return observation, fmt.Errorf("write START: %w", err)
	}
	startReadback, err := regs.ReadGPO()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		return observation, fmt.Errorf("read START readback: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return observation, err
	}
	if startReadback != startWord() {
		return observation, fmt.Errorf("%w: START readback is %#08x", errMailboxProtocol, startReadback)
	}
	if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
		return observation, err
	}

	expectedSequence := startSequence
	for len(observation.Payload) < len(expectedPayload) {
		deadline := mailboxAdvanceDeadline(clock, totalDeadline)
		word, sampleErr := readStableMailboxWord(ctx, regs, clock, deadline, "DATA")
		if sampleErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return observation, ctxErr
			}
			if !clock.Now().Before(totalDeadline) {
				return observation, mailboxTimeoutError("START-through-DONE total")
			}
			return observation, sampleErr
		}
		if err := validateCommonMailboxWord(word, "DATA"); err != nil {
			return observation, err
		}
		if len(observation.Payload) >= mailboxMaxPayload {
			return observation, fmt.Errorf("%w: DATA byte %d exceeds %d-byte payload bound", errMailboxProtocol, len(observation.Payload)+1, mailboxMaxPayload)
		}
		if err := validateDataWord(word, expectedSequence, expectedPayload[len(observation.Payload)]); err != nil {
			return observation, err
		}

		// The byte is part of the partial observation as soon as its complete,
		// stable word has been validated. A failed ACK therefore retains the
		// bytes the FPGA actually offered without ever ACKing an invalid word.
		observation.Payload = append(observation.Payload, word.data)
		if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
			return observation, err
		}
		if len(observation.Payload) == 1 {
			if err := emitMailboxProgress(callback, mailboxProgress{Phase: mailboxProgressData, Observation: cloneObservation(observation)}); err != nil {
				return observation, err
			}
		}
		if err := writeAndReadbackACK(ctx, regs, word.raw, "DATA"); err != nil {
			return observation, err
		}
		if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
			return observation, err
		}
		expectedSequence++
	}

	endDeadline := mailboxAdvanceDeadline(clock, totalDeadline)
	end, err := readStableMailboxWord(ctx, regs, clock, endDeadline, "END")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		if !clock.Now().Before(totalDeadline) {
			return observation, mailboxTimeoutError("START-through-DONE total")
		}
		return observation, err
	}
	if err := validateCommonMailboxWord(end, "END"); err != nil {
		return observation, err
	}
	if err := validateEndWord(end, expectedSequence); err != nil {
		return observation, err
	}
	if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
		return observation, err
	}
	if err := writeAndReadbackACK(ctx, regs, end.raw, "END"); err != nil {
		return observation, err
	}
	if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
		return observation, err
	}
	if err := emitMailboxProgress(callback, mailboxProgress{Phase: mailboxProgressEnd, Observation: cloneObservation(observation)}); err != nil {
		return observation, err
	}

	doneAdvanceDeadline := clock.Now().Add(mailboxAdvanceTimeout)
	doneDeadline := doneAdvanceDeadline
	if totalDeadline.Before(doneDeadline) {
		doneDeadline = totalDeadline
	}
	done, err := readStableMailboxWord(ctx, regs, clock, doneDeadline, "DONE")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		if !clock.Now().Before(totalDeadline) {
			return observation, mailboxTimeoutError("START-through-DONE total")
		}
		return observation, err
	}
	if err := validateCommonMailboxWord(done, "DONE"); err != nil {
		return observation, err
	}
	if err := validateDoneWord(done, expectedSequence); err != nil {
		return observation, err
	}
	if err := confirmDoneHold(ctx, regs, clock, done.raw, doneAdvanceDeadline, totalDeadline); err != nil {
		return observation, err
	}
	if err := emitMailboxProgress(callback, mailboxProgress{Phase: mailboxProgressDone, Observation: cloneObservation(observation), TerminalWord: done.raw}); err != nil {
		return observation, err
	}
	observation.TerminalWord = done.raw
	return observation, nil
}

func cloneObservation(observation Observation) Observation {
	observation.Payload = append([]byte(nil), observation.Payload...)
	return observation
}

func emitMailboxProgress(callback mailboxProgressCallback, progress mailboxProgress) error {
	if callback == nil {
		return nil
	}
	if err := callback(progress); err != nil {
		return err
	}
	return nil
}

type mailboxWord struct {
	raw       uint32
	signature uint8
	version   uint8
	opcode    uint8
	sequence  uint8
	data      byte
}

func decodeMailboxWord(raw uint32) mailboxWord {
	return mailboxWord{
		raw:       raw,
		signature: uint8(raw >> 24),
		version:   uint8((raw >> 20) & 0x0f),
		opcode:    uint8((raw >> 16) & 0x0f),
		sequence:  uint8((raw >> 8) & 0xff),
		data:      byte(raw),
	}
}

func startWord() uint32 {
	return uint32(mailboxAckSignature)<<24 | uint32(mailboxVersion)<<20 | uint32(mailboxOpcodeHello)<<16
}

func mailboxACK(raw uint32) uint32 {
	return raw&0x00ffffff | uint32(mailboxAckSignature)<<24
}

func validateHelloWord(word mailboxWord) error {
	if err := validateCommonMailboxWord(word, "HELLO"); err != nil {
		return err
	}
	if word.opcode != mailboxOpcodeHello {
		return fmt.Errorf("%w: HELLO opcode is %d", errMailboxProtocol, word.opcode)
	}
	if word.sequence != 0 {
		return fmt.Errorf("%w: HELLO sequence is %d", errMailboxProtocol, word.sequence)
	}
	if word.data != 0 {
		return fmt.Errorf("%w: HELLO byte is %#02x", errMailboxProtocol, word.data)
	}
	return nil
}

func validateCommonMailboxWord(word mailboxWord, state string) error {
	if word.signature != mailboxSignature {
		return fmt.Errorf("%w: %s signature is %#02x", errMailboxProtocol, state, word.signature)
	}
	if word.version != mailboxVersion {
		return fmt.Errorf("%w: %s version is %d", errMailboxProtocol, state, word.version)
	}
	return nil
}

func validateDataWord(word mailboxWord, expectedSequence uint8, expected byte) error {
	if word.opcode != mailboxOpcodeData {
		switch word.opcode {
		case mailboxOpcodeEnd:
			return fmt.Errorf("%w: early END at DATA sequence %d", errMailboxProtocol, word.sequence)
		case mailboxOpcodeDone:
			return fmt.Errorf("%w: early DONE at DATA sequence %d", errMailboxProtocol, word.sequence)
		default:
			return fmt.Errorf("%w: DATA opcode is %d", errMailboxProtocol, word.opcode)
		}
	}
	if word.sequence != expectedSequence {
		return fmt.Errorf("%w: DATA sequence is %d, want %d", errMailboxProtocol, word.sequence, expectedSequence)
	}
	if word.data != expected {
		return fmt.Errorf("%w: DATA byte at sequence %d is %#02x, want %#02x", errMailboxPayloadMismatch, word.sequence, word.data, expected)
	}
	return nil
}

func validateEndWord(word mailboxWord, expectedSequence uint8) error {
	if word.opcode != mailboxOpcodeEnd {
		switch word.opcode {
		case mailboxOpcodeData:
			return fmt.Errorf("%w: duplicate or late DATA while waiting for END", errMailboxProtocol)
		case mailboxOpcodeDone:
			return fmt.Errorf("%w: early DONE while waiting for END", errMailboxProtocol)
		default:
			return fmt.Errorf("%w: END opcode is %d", errMailboxProtocol, word.opcode)
		}
	}
	if word.sequence != expectedSequence {
		return fmt.Errorf("%w: END sequence is %d, want %d", errMailboxProtocol, word.sequence, expectedSequence)
	}
	if word.data != 0 {
		return fmt.Errorf("%w: END byte is %#02x", errMailboxProtocol, word.data)
	}
	return nil
}

func validateDoneWord(word mailboxWord, expectedSequence uint8) error {
	if word.opcode != mailboxOpcodeDone {
		switch word.opcode {
		case mailboxOpcodeData:
			return fmt.Errorf("%w: late DATA while waiting for DONE", errMailboxProtocol)
		case mailboxOpcodeEnd:
			return fmt.Errorf("%w: DONE observed before END acknowledgement was consumed", errMailboxProtocol)
		default:
			return fmt.Errorf("%w: DONE opcode is %d", errMailboxProtocol, word.opcode)
		}
	}
	if word.sequence != expectedSequence {
		return fmt.Errorf("%w: DONE sequence is %d, want %d", errMailboxProtocol, word.sequence, expectedSequence)
	}
	if word.data != 0 {
		return fmt.Errorf("%w: DONE byte is %#02x", errMailboxProtocol, word.data)
	}
	return nil
}

func writeAndReadbackACK(ctx context.Context, regs Registers, raw uint32, state string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ack := mailboxACK(raw)
	if err := regs.WriteGPO(ack); err != nil {
		return fmt.Errorf("write %s ACK: %w", state, err)
	}
	readback, err := regs.ReadGPO()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("read %s ACK readback: %w", state, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if readback != ack {
		return fmt.Errorf("%w: %s ACK readback is %#08x, want %#08x", errMailboxProtocol, state, readback, ack)
	}
	return nil
}

func readStableMailboxWord(ctx context.Context, regs Registers, clock Clock, deadline time.Time, state string) (mailboxWord, error) {
	if err := ctx.Err(); err != nil {
		return mailboxWord{}, err
	}
	if err := checkMailboxDeadline(clock, deadline, state); err != nil {
		return mailboxWord{}, err
	}
	first, err := regs.ReadGPI()
	if err != nil {
		return mailboxWord{}, fmt.Errorf("read %s first sample: %w", state, err)
	}
	if err := ctx.Err(); err != nil {
		return mailboxWord{}, err
	}
	if err := waitMailboxPoll(ctx, clock, deadline, state); err != nil {
		return mailboxWord{}, err
	}
	if err := ctx.Err(); err != nil {
		return mailboxWord{}, err
	}
	if err := checkMailboxDeadline(clock, deadline, state); err != nil {
		return mailboxWord{}, err
	}
	second, err := regs.ReadGPI()
	if err != nil {
		return mailboxWord{}, fmt.Errorf("read %s second sample: %w", state, err)
	}
	if err := ctx.Err(); err != nil {
		return mailboxWord{}, err
	}
	if err := checkMailboxDeadline(clock, deadline, state); err != nil {
		return mailboxWord{}, err
	}
	if first != second {
		return mailboxWord{}, fmt.Errorf("%w: unstable %s samples changed from %#08x to %#08x", errMailboxProtocol, state, first, second)
	}
	return decodeMailboxWord(first), nil
}

func waitMailboxPoll(ctx context.Context, clock Clock, deadline time.Time, state string) error {
	wait := mailboxPollInterval
	// Derive the remaining duration from the injected Now value so fake clocks
	// and the production clock share the exact same boundary arithmetic.
	remaining := deadline.Sub(clock.Now())
	if remaining > 0 && remaining < wait {
		wait = remaining
	}
	if remaining <= 0 {
		return mailboxTimeoutError(state)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-clock.After(wait):
		return nil
	}
}

func checkMailboxDeadline(clock Clock, deadline time.Time, state string) error {
	if !clock.Now().Before(deadline) {
		return mailboxTimeoutError(state)
	}
	return nil
}

func mailboxTimeoutError(state string) error {
	return fmt.Errorf("%w: %s deadline exceeded", errMailboxTimeout, state)
}

func mailboxAdvanceDeadline(clock Clock, totalDeadline time.Time) time.Time {
	stage := clock.Now().Add(mailboxAdvanceTimeout)
	if totalDeadline.Before(stage) {
		return totalDeadline
	}
	return stage
}

func confirmDoneHold(ctx context.Context, regs Registers, clock Clock, word uint32, doneDeadline, totalDeadline time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkMailboxDeadline(clock, doneDeadline, "DONE"); err != nil {
		return err
	}
	if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
		return err
	}

	holdDeadline, holdState := doneDeadline, "DONE"
	if totalDeadline.Before(holdDeadline) {
		holdDeadline, holdState = totalDeadline, "START-through-DONE total"
	}
	if err := waitMailboxPoll(ctx, clock, holdDeadline, holdState); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkMailboxDeadline(clock, doneDeadline, "DONE"); err != nil {
		return err
	}
	if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
		return err
	}
	held, err := regs.ReadGPI()
	if err != nil {
		return fmt.Errorf("read DONE terminal hold: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkMailboxDeadline(clock, doneDeadline, "DONE"); err != nil {
		return err
	}
	if err := checkMailboxDeadline(clock, totalDeadline, "START-through-DONE total"); err != nil {
		return err
	}
	if held != word {
		return fmt.Errorf("%w: DONE terminal hold changed from %#08x to %#08x", errMailboxProtocol, word, held)
	}
	return nil
}

type mailboxRealClock struct{}

func (mailboxRealClock) Now() time.Time                                { return time.Now() }
func (mailboxRealClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }
