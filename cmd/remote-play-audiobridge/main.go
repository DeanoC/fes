// Command remote-play-audiobridge is a target-private diagnostic audio
// receiver. It is intentionally not part of the public cast controller or
// host_cast lifecycle; physical target audio is not wired into this helper.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/internal/remotemedia"
	"golang.org/x/sys/unix"
)

const (
	bridgeStartupTimeout = 5 * time.Second
	bridgeCleanupTimeout = 2 * time.Second
	bridgeDefaultRate    = remotemedia.AudioSampleRate
	bridgeDefaultChannel = 2
)

type bridgeConfig struct {
	rtpAddress     string
	controlAddress string
	session        string
	generation     uint64
	tokenFile      string
	sampleRate     int
	channels       int
	ssrc           uint32
	audioDevice    string
	dumpPCM        string
}

func parseBridgeArgs(args []string, stderr io.Writer) (bridgeConfig, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	flags := flag.NewFlagSet("remote-play-audiobridge", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rtp := flags.String("rtp", "", "target-private UDP RTP listen address")
	control := flags.String("control", "", "target-private TCP control listen address")
	session := flags.String("session", "", "authenticated media session")
	generation := flags.Uint64("generation", 0, "authenticated media generation")
	tokenFile := flags.String("token-file", "", "file containing the authenticated session token")
	sampleRate := flags.Int("sample-rate", bridgeDefaultRate, "PCM sample rate")
	channels := flags.Int("channels", bridgeDefaultChannel, "PCM channel count")
	ssrc := flags.Uint64("ssrc", 1, "authenticated audio RTP SSRC")
	audioDevice := flags.String("audio-device", "null", "target-private sink selector: null or dump")
	dumpPCM := flags.String("dump-pcm", "", "PCM16LE output path when -audio-device=dump")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return bridgeConfig{}, errors.New("invalid audio bridge arguments")
	}
	if *ssrc > uint64(^uint32(0)) {
		return bridgeConfig{}, errors.New("audio bridge SSRC is out of range")
	}
	config := bridgeConfig{
		rtpAddress:     strings.TrimSpace(*rtp),
		controlAddress: strings.TrimSpace(*control),
		session:        strings.TrimSpace(*session),
		generation:     *generation,
		tokenFile:      strings.TrimSpace(*tokenFile),
		sampleRate:     *sampleRate,
		channels:       *channels,
		ssrc:           uint32(*ssrc),
		audioDevice:    strings.ToLower(strings.TrimSpace(*audioDevice)),
		dumpPCM:        strings.TrimSpace(*dumpPCM),
	}
	if err := validateBridgeConfig(config); err != nil {
		return bridgeConfig{}, err
	}
	return config, nil
}

func validateBridgeConfig(config bridgeConfig) error {
	if config.rtpAddress == "" || !validListenAddress(config.rtpAddress) {
		return errors.New("audio bridge RTP address is invalid")
	}
	if config.controlAddress == "" || !validListenAddress(config.controlAddress) {
		return errors.New("audio bridge control address is invalid")
	}
	if config.rtpAddress == config.controlAddress && !ephemeralAddress(config.rtpAddress) {
		return errors.New("audio bridge RTP and control addresses must differ")
	}
	if config.session == "" || config.generation == 0 || config.tokenFile == "" {
		return errors.New("audio bridge session, generation, and token file are required")
	}
	if config.sampleRate != bridgeDefaultRate || (config.channels != 1 && config.channels != 2) || config.ssrc == 0 {
		return errors.New("audio bridge format must be 48000 Hz mono or stereo PCM16")
	}
	switch config.audioDevice {
	case "null":
		if config.dumpPCM != "" {
			return errors.New("PCM dump requires -audio-device=dump")
		}
	case "dump":
		if config.dumpPCM == "" {
			return errors.New("dump sink requires -dump-pcm")
		}
	default:
		return errors.New("audio bridge sink selector must be null or dump")
	}
	return nil
}

func validListenAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return false
	}
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
			return false
		}
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber >= 0 && portNumber <= 65535
}

func ephemeralAddress(address string) bool {
	_, port, err := net.SplitHostPort(address)
	return err == nil && port == "0"
}

func readTokenFile(path string) (string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", errors.New("audio bridge token file must be a private regular file")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return "", errors.New("audio bridge token file could not be opened")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("audio bridge token file must be a private regular file")
	}
	contents, err := io.ReadAll(io.LimitReader(file, 16<<10+1))
	if err != nil {
		return "", errors.New("audio bridge token file could not be read")
	}
	if len(contents) > 16<<10 {
		return "", errors.New("audio bridge token file is too large")
	}
	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", errors.New("audio bridge token file is empty")
	}
	return token, nil
}

type audioBridge struct {
	config   bridgeConfig
	token    string
	receiver *remotemedia.AudioReceiver
	playout  remotemedia.AudioPlayout
	sink     remotemedia.AudioSink

	mu         sync.Mutex
	udpAddress string
	tcpAddress string
}

func newAudioBridge(config bridgeConfig, token string, sink remotemedia.AudioSink) (*audioBridge, error) {
	if err := validateBridgeConfig(config); err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("audio bridge token is required")
	}
	format := remotemedia.AudioFormat{
		SampleRate: config.sampleRate, Channels: config.channels,
		Encoding: remotemedia.AudioEncodingPCM16LE, FrameSamples: remotemedia.DefaultAudioFrameSamples,
	}
	receiver, err := remotemedia.NewAudioReceiver(remotemedia.AudioReceiverConfig{
		ListenAddress: config.rtpAddress, ControlAddress: config.controlAddress,
		Session: config.session, Generation: config.generation, Token: token,
		SSRC: config.ssrc, PayloadType: remotemedia.RTPPayloadTypePCM16,
		SampleRate: config.sampleRate, Channels: config.channels,
		FrameSamples: remotemedia.DefaultAudioFrameSamples, FormatCapabilityVersion: 1,
	})
	if err != nil {
		return nil, err
	}
	playout, err := remotemedia.NewAudioPlayout(remotemedia.AudioPlayoutConfig{
		Format: format, StartupPrebuffer: 1, MaxBufferedFrames: 32,
	})
	if err != nil {
		return nil, err
	}
	if sink == nil {
		sink = newNullAudioSink()
	}
	return &audioBridge{config: config, token: token, receiver: receiver, playout: playout, sink: sink}, nil
}

func (b *audioBridge) addresses() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.udpAddress, b.tcpAddress
}

func (b *audioBridge) Run(ctx context.Context) error {
	if b == nil || b.receiver == nil || b.playout == nil || b.sink == nil {
		return errors.New("audio bridge is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defer b.receiver.Close()
	udpAddr, err := net.ResolveUDPAddr("udp", b.config.rtpAddress)
	if err != nil {
		return errors.New("audio bridge RTP address is invalid")
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return errors.New("audio bridge RTP listen failed")
	}
	tcpListener, err := net.Listen("tcp", b.config.controlAddress)
	if err != nil {
		_ = udpConn.Close()
		return errors.New("audio bridge control listen failed")
	}
	b.mu.Lock()
	b.udpAddress = udpConn.LocalAddr().String()
	b.tcpAddress = tcpListener.Addr().String()
	b.mu.Unlock()
	defer udpConn.Close()
	defer tcpListener.Close()

	acceptCtx, acceptCancel := context.WithTimeout(ctx, bridgeStartupTimeout)
	conn, err := acceptWithContext(acceptCtx, tcpListener)
	acceptCancel()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(bridgeStartupTimeout))
	message, err := remotemedia.ReadControlMessage(conn)
	if err != nil {
		return errors.New("audio bridge control hello failed")
	}
	if err := b.receiver.AcceptControl(message, conn.RemoteAddr()); err != nil {
		return errors.New("audio bridge control authentication failed")
	}
	_ = conn.SetReadDeadline(time.Time{})

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	udpErr := make(chan error, 1)
	controlErr := make(chan error, 1)
	playoutErr := make(chan error, 1)
	playoutDone := make(chan struct{})
	var networkWorkers sync.WaitGroup
	networkWorkers.Add(2)
	go func() {
		defer networkWorkers.Done()
		udpErr <- b.readRTP(runCtx, udpConn)
	}()
	go func() {
		defer networkWorkers.Done()
		controlErr <- b.readControl(runCtx, conn)
	}()
	go func() {
		defer close(playoutDone)
		playoutErr <- b.playout.Run(runCtx, b.sink)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		runErr = ctx.Err()
	case err := <-udpErr:
		if !errors.Is(err, context.Canceled) {
			runErr = err
		}
	case err := <-controlErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			runErr = err
		}
	case err := <-playoutErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			runErr = err
		}
	}
	cancel()
	_ = udpConn.Close()
	_ = conn.Close()
	networkCleanupCtx, networkCleanupCancel := context.WithTimeout(context.Background(), bridgeCleanupTimeout)
	workersDone := make(chan struct{})
	go func() {
		networkWorkers.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-networkCleanupCtx.Done():
		bridgeRecordCleanupFailure(&runErr, errors.New("audio bridge network workers did not stop"))
	}
	networkCleanupCancel()
	if cleanupErr := stopAudioBridgePlayout(b.playout, playoutDone, bridgeCleanupTimeout); cleanupErr != nil {
		bridgeRecordCleanupFailure(&runErr, cleanupErr)
	}
	return runErr
}

func stopAudioBridgePlayout(playout remotemedia.AudioPlayout, done <-chan struct{}, timeout time.Duration) error {
	if playout == nil {
		return errors.New("audio bridge playout cleanup failed")
	}
	if timeout <= 0 {
		timeout = bridgeCleanupTimeout
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), timeout)
	stopErr := playout.Stop(stopCtx)
	stopCancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), timeout)
	defer waitCancel()
	select {
	case <-done:
	case <-waitCtx.Done():
		if stopErr == nil {
			stopErr = errors.New("audio bridge playout worker did not stop")
		}
	}
	if stopErr != nil {
		return errors.New("audio bridge playout cleanup failed")
	}
	return nil
}

func bridgeRecordCleanupFailure(runErr *error, cleanupErr error) {
	if runErr == nil || cleanupErr == nil {
		return
	}
	if *runErr == nil || errors.Is(*runErr, context.Canceled) || errors.Is(*runErr, context.DeadlineExceeded) {
		*runErr = cleanupErr
		return
	}
	*runErr = errors.Join(*runErr, cleanupErr)
}

func acceptWithContext(ctx context.Context, listener net.Listener) (net.Conn, error) {
	accepted := make(chan struct {
		conn net.Conn
		err  error
	}, 1)
	go func() {
		conn, err := listener.Accept()
		accepted <- struct {
			conn net.Conn
			err  error
		}{conn: conn, err: err}
	}()
	select {
	case result := <-accepted:
		return result.conn, result.err
	case <-ctx.Done():
		_ = listener.Close()
		return nil, ctx.Err()
	}
}

func (b *audioBridge) readRTP(ctx context.Context, conn *net.UDPConn) error {
	buffer := make([]byte, 64<<10)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, peer, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, net.ErrClosed) {
				continue
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return err
		}
		frame, err := b.receiver.Ingest(buffer[:n], peer)
		if err != nil {
			continue
		}
		if err := b.playout.Push(ctx, frame); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (b *audioBridge) readControl(ctx context.Context, conn net.Conn) error {
	for {
		message, err := remotemedia.ReadControlMessage(conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if err := remotemedia.ValidateControlMessage(message, b.config.session, b.config.generation, b.token); err != nil {
			return err
		}
		if message.Type == remotemedia.ControlStop {
			return nil
		}
	}
}

type nullAudioSink struct {
	mu     sync.Mutex
	opened bool
	stats  remotemedia.AudioSinkStats
}

func newNullAudioSink() *nullAudioSink { return &nullAudioSink{} }

func (s *nullAudioSink) Open(ctx context.Context, config remotemedia.AudioSinkConfig) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := remotemedia.ValidateAudioSinkConfig(config); err != nil {
		return err
	}
	s.mu.Lock()
	s.opened = true
	s.mu.Unlock()
	return nil
}
func (s *nullAudioSink) Ready(ctx context.Context) error { return ctxErr(ctx) }
func (s *nullAudioSink) Write(ctx context.Context, frame remotemedia.AudioFrame) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := remotemedia.ValidateAudioFrame(frame); err != nil {
		return err
	}
	s.mu.Lock()
	if !s.opened {
		s.mu.Unlock()
		return errors.New("audio sink is not open")
	}
	s.stats.WrittenFrames += uint64(frame.Frames)
	s.mu.Unlock()
	return nil
}
func (s *nullAudioSink) Drain(ctx context.Context) error { return ctxErr(ctx) }
func (s *nullAudioSink) Mute(ctx context.Context) error  { return ctxErr(ctx) }
func (s *nullAudioSink) Reset(ctx context.Context) error { return ctxErr(ctx) }
func (s *nullAudioSink) Stats() remotemedia.AudioSinkStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}
func (s *nullAudioSink) Close(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.opened = false
	s.stats.Shutdown = true
	s.mu.Unlock()
	return nil
}

type pcmDumpAudioSink struct {
	mu    sync.Mutex
	path  string
	file  *os.File
	stats remotemedia.AudioSinkStats
}

func newPCMDumpAudioSink(path string) *pcmDumpAudioSink { return &pcmDumpAudioSink{path: path} }

func (s *pcmDumpAudioSink) Open(ctx context.Context, config remotemedia.AudioSinkConfig) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := remotemedia.ValidateAudioSinkConfig(config); err != nil {
		return err
	}
	if s.path == "" {
		return errors.New("audio bridge PCM dump path is required")
	}
	if _, err := os.Lstat(s.path); err == nil {
		return errors.New("audio bridge PCM dump path must be a new regular file")
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("audio bridge PCM dump path could not be inspected")
	}
	// O_EXCL prevents following a symlink or replacing a device/FIFO that
	// appears between the Lstat and open. A fresh regular file is the only
	// supported diagnostic sink target.
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("audio bridge PCM dump could not be opened")
	}
	s.mu.Lock()
	s.file = file
	s.mu.Unlock()
	return nil
}
func (s *pcmDumpAudioSink) Ready(ctx context.Context) error { return ctxErr(ctx) }
func (s *pcmDumpAudioSink) Write(ctx context.Context, frame remotemedia.AudioFrame) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := remotemedia.ValidateAudioFrame(frame); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return errors.New("audio bridge PCM dump is not open")
	}
	if _, err := s.file.Write(frame.PCM16); err != nil {
		return err
	}
	s.stats.WrittenFrames += uint64(frame.Frames)
	return nil
}
func (s *pcmDumpAudioSink) Drain(ctx context.Context) error { return ctxErr(ctx) }
func (s *pcmDumpAudioSink) Mute(ctx context.Context) error  { return ctxErr(ctx) }
func (s *pcmDumpAudioSink) Reset(ctx context.Context) error { return ctxErr(ctx) }
func (s *pcmDumpAudioSink) Stats() remotemedia.AudioSinkStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}
func (s *pcmDumpAudioSink) Close(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		s.stats.Shutdown = true
		return err
	}
	s.stats.Shutdown = true
	return nil
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func sinkForConfig(config bridgeConfig) (remotemedia.AudioSink, error) {
	switch config.audioDevice {
	case "null":
		return newNullAudioSink(), nil
	case "dump":
		return newPCMDumpAudioSink(config.dumpPCM), nil
	default:
		return nil, errors.New("audio bridge sink selector is invalid")
	}
}

func runBridge(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	config, err := parseBridgeArgs(args, stderr)
	if err != nil {
		return err
	}
	token, err := readTokenFile(config.tokenFile)
	if err != nil {
		return err
	}
	sink, err := sinkForConfig(config)
	if err != nil {
		return err
	}
	bridge, err := newAudioBridge(config, token, sink)
	if err != nil {
		return errors.New("audio bridge configuration failed")
	}
	err = bridge.Run(ctx)
	report := bridge.receiver.Report()
	if stdout != nil {
		fmt.Fprintf(stdout, "audio bridge packets=%d frames=%d non_zero_samples=%d\n", report.Packets, report.Frames, report.NonZeroSamples)
	}
	return err
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runBridge(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "remote-play-audiobridge: failed")
		os.Exit(1)
	}
}
