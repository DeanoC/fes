package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/internal/remotemedia"
)

const commandName = "remote-play-sender"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", commandName, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("mode is required: devices, sender, or probe")
	}
	switch args[0] {
	case "devices":
		return runDevices(args[1:], stdout, stderr)
	case "sender":
		return runSender(ctx, args[1:], stdout, stderr)
	case "probe":
		return runProbe(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
}

func runDevices(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("devices", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	devices, err := remotemedia.ListCaptureDevices()
	if err != nil {
		return fmt.Errorf("enumerate physical HDMI capture devices: %w", err)
	}
	if err := requireCaptureDevices(devices); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = stdout.Write(encoded)
	return err
}

func runSender(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("sender", flag.ContinueOnError)
	flags.SetOutput(stderr)
	captureDevice := flags.String("capture-device", "", "required AVFoundation capture-device unique ID or name")
	rtpAddress := flags.String("rtp", "127.0.0.1:5004", "RTP/H.264 UDP destination")
	controlAddress := flags.String("control", "", "optional authenticated media-control TCP destination")
	session := flags.String("session", "", "session identifier")
	generation := flags.Uint64("generation", 1, "session generation")
	token := flags.String("token", "", "session token; generated when omitted and never printed")
	width := flags.Int("width", 0, "requested capture width; zero uses the device mode")
	height := flags.Int("height", 0, "requested capture height; zero uses the device mode")
	fps := flags.String("fps", "", "requested source frame rate, e.g. 60000/1001")
	bitrate := flags.Int("bitrate", 8_000_000, "H.264 bitrate in bits per second")
	gop := flags.Int("gop", 30, "H.264 keyframe interval in frames")
	output := flags.String("metrics", "", "optional final JSON report output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *captureDevice == "" {
		return errors.New("--capture-device is required; no physical HDMI capture device was supplied (synthetic frames are not supported)")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if *session == "" {
		*session = randomID()
	}
	if *token == "" {
		*token = randomID()
	}
	rate, err := parseFrameRate(*fps)
	if err != nil {
		return err
	}
	config := remotemedia.CaptureConfig{Device: *captureDevice, Width: *width, Height: *height, FPS: rate, Bitrate: *bitrate, GOP: *gop}
	if err := validateCaptureConfig(config); err != nil {
		return err
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("physical AVFoundation HDMI capture requires macOS; current OS is %s", runtime.GOOS)
	}
	if shouldEnumerateCaptureDevices(*captureDevice) {
		devices, err := remotemedia.ListCaptureDevices()
		if err != nil {
			return fmt.Errorf("enumerate physical HDMI capture devices: %w", err)
		}
		if err := requireCaptureDevices(devices); err != nil {
			return err
		}
	}
	capture, err := remotemedia.OpenNativeCapture(config)
	if err != nil {
		return fmt.Errorf("open physical HDMI capture device: %w", err)
	}
	defer capture.Close()

	metrics := remotemedia.NewMetrics(time.Now())
	sender, err := remotemedia.NewSender(remotemedia.SenderConfig{
		RTPAddress:        *rtpAddress,
		ControlAddress:    *controlAddress,
		Session:           *session,
		Generation:        *generation,
		Token:             *token,
		SSRC:              randomNonZeroUint32(),
		InitialSequence:   randomUint16(),
		RTPBaseTimestamp:  randomUint32(),
		MTU:               remotemedia.DefaultRTPMTU,
		Bitrate:           *bitrate,
		PeriodicKeyframes: *gop,
		KeyframeInterval:  500 * time.Millisecond,
	}, capture, metrics)
	if err != nil {
		return err
	}
	runErr := sender.Run(ctx)
	stats, _ := sender.CaptureStats()
	report := struct {
		Mode            string                      `json:"mode"`
		Session         string                      `json:"session"`
		Generation      uint64                      `json:"generation"`
		PhysicalCapture bool                        `json:"physical_capture"`
		ScreenCapture   bool                        `json:"screen_capture"`
		Metrics         remotemedia.MetricsSnapshot `json:"metrics"`
		Capture         remotemedia.CaptureStats    `json:"capture"`
		ShutdownReason  string                      `json:"shutdown_reason,omitempty"`
	}{
		Mode:            "sender",
		Session:         *session,
		Generation:      *generation,
		PhysicalCapture: !remotemedia.IsScreenCaptureDevice(*captureDevice),
		ScreenCapture:   remotemedia.IsScreenCaptureDevice(*captureDevice),
		Metrics:         metrics.Snapshot(time.Now(), stats.QueueDepth, stats.QueueHighWater),
		Capture:         stats,
	}
	if runErr != nil {
		report.ShutdownReason = runErr.Error()
	}
	if err := writeReport(*output, stdout, report); err != nil {
		return err
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return nil
	}
	return runErr
}

func runProbe(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "optional redacted capability JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	result := map[string]any{
		"os":                        runtime.GOOS,
		"arch":                      runtime.GOARCH,
		"capture_backend":           "avfoundation-videotoolbox",
		"physical_capture_required": true,
		"synthetic_frames":          false,
		"video_toolbox":             "not verified",
		"assumptions": []string{
			"macOS with a UVC HDMI capture device",
			"capture mode sustains the requested MiSTer resolution and frame rate",
			"Apple VideoToolbox H.264 encoder is available",
		},
	}
	devices, err := remotemedia.ListCaptureDevices()
	if err != nil {
		result["capture_device_error"] = err.Error()
	} else {
		result["capture_device_count"] = len(devices)
		redactedDevices := make([]map[string]any, 0, len(devices))
		for _, device := range devices {
			redactedDevices = append(redactedDevices, map[string]any{"name": device.Name, "external": device.External})
		}
		result["capture_devices"] = redactedDevices
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(*output, encoded, 0o644); err != nil {
			return err
		}
	}
	_, err = stdout.Write(encoded)
	return err
}

func writeReport(path string, stdout io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return err
		}
	}
	_, err = stdout.Write(encoded)
	return err
}

func validateCaptureConfig(config remotemedia.CaptureConfig) error {
	if config.Width < 0 || config.Height < 0 || config.Bitrate < 0 || config.GOP < 0 {
		return errors.New("capture dimensions, bitrate, and GOP must be non-negative")
	}
	if config.FPS.Numerator < 0 || config.FPS.Denominator < 0 {
		return errors.New("capture FPS cannot be negative")
	}
	if config.FPS.Numerator > 0 && config.FPS.Denominator <= 0 {
		return errors.New("capture FPS denominator must be positive")
	}
	return nil
}

func requireCaptureDevices(devices []remotemedia.CaptureDevice) error {
	if len(devices) == 0 {
		return errors.New("no physical HDMI capture devices detected; connect a UVC HDMI capture device (synthetic frames are not supported)")
	}
	return nil
}

func shouldEnumerateCaptureDevices(device string) bool {
	return !remotemedia.IsScreenCaptureDevice(device)
}

func parseFrameRate(value string) (remotemedia.FrameRate, error) {
	if strings.TrimSpace(value) == "" {
		return remotemedia.FrameRate{}, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return remotemedia.FrameRate{}, fmt.Errorf("invalid FPS %q", value)
	}
	numerator, err := strconv.Atoi(parts[0])
	if err != nil || numerator <= 0 {
		return remotemedia.FrameRate{}, fmt.Errorf("invalid FPS %q", value)
	}
	denominator := 1
	if len(parts) == 2 {
		denominator, err = strconv.Atoi(parts[1])
		if err != nil || denominator <= 0 {
			return remotemedia.FrameRate{}, fmt.Errorf("invalid FPS %q", value)
		}
	}
	return remotemedia.FrameRate{Numerator: numerator, Denominator: denominator}, nil
}

func randomID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate cryptographically random identifier: %v", err))
	}
	return hex.EncodeToString(bytes[:])
}

func randomUint32() uint32 {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate cryptographically random RTP value: %v", err))
	}
	return binary.BigEndian.Uint32(bytes[:])
}

func randomNonZeroUint32() uint32 {
	for {
		value := randomUint32()
		if value != 0 {
			return value
		}
	}
}

func randomUint16() uint16 { return uint16(randomUint32()) }
