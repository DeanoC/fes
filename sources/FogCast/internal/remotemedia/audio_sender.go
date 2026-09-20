package remotemedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type AudioSenderConfig struct {
	RTPAddress              string
	ControlAddress          string
	Session                 string
	Generation              uint64
	Token                   string
	SSRC                    uint32
	InitialSequence         uint16
	RTPBaseTimestamp        uint32
	MTU                     int
	SampleRate              int
	Channels                int
	FrameSamples            int
	FormatCapabilityVersion uint32
}

type AudioSenderReport struct {
	Packets        uint64     `json:"packets"`
	Bytes          uint64     `json:"bytes"`
	Frames         uint64     `json:"frames"`
	NonZeroSamples uint64     `json:"non_zero_samples"`
	Source         AudioStats `json:"source"`
	Shutdown       bool       `json:"shutdown"`
}

// AudioSender owns exactly one source, UDP destination, authenticated control
// connection, SSRC, and 48 kHz RTP packetizer for one audio worker.
type AudioSender struct {
	config AudioSenderConfig
	source AudioSource
	format AudioFormat

	mu           sync.Mutex
	lifecycleMu  sync.Mutex
	closeMu      sync.Mutex
	runCancel    context.CancelFunc
	closed       bool
	sourceClosed bool
	sourceOwned  bool
	report       AudioSenderReport
}

func NewAudioSender(config AudioSenderConfig, source AudioSource) (*AudioSender, error) {
	if source == nil || config.RTPAddress == "" || config.ControlAddress == "" || config.Session == "" || config.Generation == 0 || config.Token == "" || config.SSRC == 0 || config.FormatCapabilityVersion == 0 {
		return nil, errors.New("audio sender configuration is invalid")
	}
	format := AudioFormat{SampleRate: config.SampleRate, Channels: config.Channels, Encoding: AudioEncodingPCM16LE, FrameSamples: config.FrameSamples}
	if err := ValidateAudioFormat(format); err != nil {
		return nil, err
	}
	if config.MTU == 0 {
		config.MTU = DefaultRTPMTU
	}
	if _, err := NewAudioRTPPacketizer(config.MTU, config.SSRC, config.InitialSequence, config.RTPBaseTimestamp, format); err != nil {
		return nil, err
	}
	return &AudioSender{config: config, source: source, format: format, sourceOwned: true}, nil
}

func (s *AudioSender) WriteMediaHello(w io.Writer) error {
	if s == nil {
		return errors.New("audio sender is nil")
	}
	body, err := json.Marshal(AudioMediaHello{
		MediaKind:               "audio",
		FormatCapabilityVersion: s.config.FormatCapabilityVersion,
		PayloadType:             RTPPayloadTypePCM16,
		ClockRate:               RTPAudioClockRate,
		SSRC:                    s.config.SSRC,
		Encoding:                AudioEncodingPCM16LE,
		SampleRate:              s.format.SampleRate,
		Channels:                s.format.Channels,
		FrameSamples:            s.format.FrameSamples,
	})
	if err != nil {
		return fmt.Errorf("marshal audio media hello: %w", err)
	}
	return WriteControlMessage(w, ControlMessage{Type: ControlMediaHello, Session: s.config.Session, Generation: s.config.Generation, Token: s.config.Token, Body: body})
}

func (s *AudioSender) Run(ctx context.Context) error { return s.run(ctx, nil) }

// RunReady invokes ready after source and transport startup succeeded, before
// reading any PCM. It mirrors the video sender lifecycle without opening an
// additional source or transport.
func (s *AudioSender) RunReady(ctx context.Context, ready func(error)) error {
	return s.run(ctx, ready)
}

func (s *AudioSender) run(ctx context.Context, ready func(error)) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var readyOnce sync.Once
	reportReady := func(startErr error) {
		if ready != nil {
			readyOnce.Do(func() { ready(startErr) })
		}
	}
	defer func() { reportReady(err) }()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.lifecycleMu.Lock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.lifecycleMu.Unlock()
		return errors.New("audio sender is closed")
	}
	if s.runCancel != nil {
		s.mu.Unlock()
		s.lifecycleMu.Unlock()
		return errors.New("audio sender is already running")
	}
	s.runCancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.runCancel = nil
		s.closed = true
		s.report.Shutdown = true
		s.mu.Unlock()
		if closeErr := s.closeSource(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close audio source: %w", closeErr))
		}
	}()
	if startErr := s.source.Start(); startErr != nil {
		s.lifecycleMu.Unlock()
		return fmt.Errorf("start audio source: %w", startErr)
	}
	s.lifecycleMu.Unlock()

	udp, err := (&net.Dialer{}).DialContext(runCtx, "udp", s.config.RTPAddress)
	if err != nil {
		return fmt.Errorf("dial audio RTP destination: %w", err)
	}
	defer udp.Close()

	var control *controlWriter
	var workers sync.WaitGroup
	var asyncErrMu sync.Mutex
	var asyncErr error
	setAsyncErr := func(candidate error) {
		if candidate == nil || errors.Is(candidate, context.Canceled) {
			return
		}
		asyncErrMu.Lock()
		if asyncErr == nil {
			asyncErr = candidate
		}
		asyncErrMu.Unlock()
	}
	getAsyncErr := func() error {
		asyncErrMu.Lock()
		defer asyncErrMu.Unlock()
		return asyncErr
	}
	{
		connection, dialErr := (&net.Dialer{}).DialContext(runCtx, "tcp", s.config.ControlAddress)
		if dialErr != nil {
			return fmt.Errorf("dial audio control destination: %w", dialErr)
		}
		control = &controlWriter{conn: connection}
		defer func() {
			_ = control.Close()
			workers.Wait()
		}()
		if err := s.WriteMediaHello(control.conn); err != nil {
			return fmt.Errorf("send audio MEDIA_HELLO: %w", err)
		}
		workers.Add(2)
		go func() { defer workers.Done(); setAsyncErr(s.audioControlLoop(runCtx, control)); cancel() }()
		go func() { defer workers.Done(); setAsyncErr(s.audioReportLoop(runCtx, control)); cancel() }()
	}
	packetizer, err := NewAudioRTPPacketizer(s.config.MTU, s.config.SSRC, s.config.InitialSequence, s.config.RTPBaseTimestamp, s.format)
	if err != nil {
		return err
	}
	reportReady(nil)
	for {
		sample, nextErr := s.source.Next(runCtx)
		if nextErr != nil {
			if runCtx.Err() != nil {
				if asyncErr := getAsyncErr(); asyncErr != nil {
					return asyncErr
				}
				return runCtx.Err()
			}
			return fmt.Errorf("audio source: %w", nextErr)
		}
		packets, packetErr := packetizer.Packetize(sample)
		if packetErr != nil {
			return fmt.Errorf("packetize audio sample: %w", packetErr)
		}
		for _, packet := range packets {
			wire := packet.Marshal()
			if _, writeErr := udp.Write(wire); writeErr != nil {
				return fmt.Errorf("send audio RTP packet: %w", writeErr)
			}
			nonZero, _ := CountNonZeroPCM16(packet.Payload)
			s.mu.Lock()
			s.report.Packets++
			s.report.Bytes += uint64(len(wire))
			s.report.Frames += uint64(s.format.FrameSamples)
			s.report.NonZeroSamples += nonZero
			s.mu.Unlock()
		}
	}
}

func (s *AudioSender) audioControlLoop(ctx context.Context, control *controlWriter) error {
	for {
		message, err := ReadControlMessage(control.conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read audio media control: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := ValidateControlMessage(message, s.config.Session, s.config.Generation, s.config.Token); err != nil {
			return fmt.Errorf("validate audio media control: %w", err)
		}
		switch message.Type {
		case ControlPing:
			if err := control.Write(ControlMessage{Type: ControlPong, Session: s.config.Session, Generation: s.config.Generation, Token: s.config.Token, Body: message.Body}); err != nil {
				return fmt.Errorf("write audio media control PONG: %w", err)
			}
		case ControlStop:
			return nil
		}
	}
}

func (s *AudioSender) audioReportLoop(ctx context.Context, control *controlWriter) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			body, err := json.Marshal(s.Report())
			if err != nil {
				return fmt.Errorf("marshal audio media report: %w", err)
			}
			if err := control.Write(ControlMessage{Type: ControlMediaReport, Session: s.config.Session, Generation: s.config.Generation, Token: s.config.Token, Body: body}); err != nil {
				return fmt.Errorf("write audio media report: %w", err)
			}
		}
	}
}

func (s *AudioSender) Report() AudioSenderReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	report := s.report
	report.Source = s.source.Stats()
	return report
}

func (s *AudioSender) Close() error {
	s.lifecycleMu.Lock()
	s.mu.Lock()
	s.closed = true
	cancel := s.runCancel
	s.mu.Unlock()
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.closeSource()
}

func (s *AudioSender) closeSource() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	s.mu.Lock()
	if s.sourceClosed {
		s.mu.Unlock()
		return nil
	}
	if !s.sourceOwned {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if err := s.source.Close(); err != nil {
		return err
	}
	s.mu.Lock()
	s.sourceClosed = true
	s.mu.Unlock()
	return nil
}
