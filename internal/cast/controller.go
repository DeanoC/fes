package cast

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
)

var (
	ErrInvalid = errors.New("cast configuration is invalid")
	ErrBusy    = errors.New("cast session is already active")
	ErrStart   = errors.New("cast session failed to start")
	ErrStop    = errors.New("cast session failed to stop")
	ErrStale   = errors.New("cast session identity does not match")
	// ErrDevelopmentProfile prevents accidental construction of cast workers
	// in the restricted hardware-development profile.
	ErrDevelopmentProfile = errors.New("cast controller is disabled in development profile")
)

const defaultStartGrace = 100 * time.Millisecond

type Config struct {
	Binary      string
	RTPAddress  string
	Control     string
	Framebuffer string
	NativeCmd   string
	NativeMode  string
	StopTimeout time.Duration
	StartGrace  time.Duration
	TokenFile   string
	Generation  uint64
}

type Process interface {
	Wait() error
	Kill() error
}

type StartProcess func(context.Context, string, ...string) (Process, error)

type Status struct {
	State      string                    `json:"state"`
	Session    string                    `json:"session,omitempty"`
	Generation uint64                    `json:"generation,omitempty"`
	Media      *protocol.CastStatusMedia `json:"media,omitempty"`
}

const (
	Idle   = "idle"
	Active = "active"
)

type Controller struct {
	config Config
	start  StartProcess
	mu     sync.Mutex
	sess   *session
}

type session struct {
	process         Process
	done            chan struct{}
	session         string
	generation      uint64
	tokenFile       string
	removeTokenFile bool
	cleanupErr      error
	media           *protocol.CastStatusMedia
}

type tokenInstallError struct {
	cause         error
	temporaryPath string
}

func (e *tokenInstallError) Error() string { return e.cause.Error() }
func (e *tokenInstallError) Unwrap() error { return e.cause }

func New(config Config, start StartProcess) (*Controller, error) {
	if config.Binary == "" || config.RTPAddress == "" || config.Control == "" || config.Framebuffer == "" || config.NativeCmd == "" || config.NativeMode == "" {
		return nil, ErrInvalid
	}
	if config.StopTimeout <= 0 {
		config.StopTimeout = 2 * time.Second
	}
	if config.StartGrace <= 0 {
		config.StartGrace = defaultStartGrace
	}
	if start == nil {
		start = func(_ context.Context, name string, args ...string) (Process, error) {
			cmd := exec.Command(name, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return commandProcess{cmd}, nil
		}
	}
	return &Controller{config: config, start: start}, nil
}

// NewForProfile constructs the compatibility cast worker only for profiles
// that explicitly permit auxiliary media facilities. The M2 development
// profile passes development=true and receives no controller.
func NewForProfile(config Config, start StartProcess, development bool) (*Controller, error) {
	if development {
		return nil, ErrDevelopmentProfile
	}
	return New(config, start)
}

func (c *Controller) Start(ctx context.Context, sessionID, token string, generation uint64) error {
	return c.startSession(ctx, sessionID, token, generation, nil)
}

// StartWithMedia accepts the public media extension. This compatibility
// controller does not own a coordinator-managed sink and therefore refuses
// audio admission until that capability exists.
func (c *Controller) StartWithMedia(ctx context.Context, sessionID, token string, generation uint64, media protocol.CastMediaSet) error {
	if err := protocol.ValidateCastMediaSet(media); err != nil {
		return ErrInvalid
	}
	capabilities := c.CastMediaCapabilities(ctx)
	if capabilities.Version != protocol.CastMediaSetVersion || !capabilities.Video || !capabilities.Audio {
		return ErrInvalid
	}
	return c.startSession(ctx, sessionID, token, generation, &protocol.CastStatusMedia{Version: media.Version, Video: media.Video, Audio: media.Audio, Ready: true, Capabilities: capabilities})
}

func (*Controller) CastMediaCapabilities(context.Context) protocol.CastMediaCapabilities {
	return protocol.CastMediaCapabilities{Version: protocol.CastMediaSetVersion, Video: true, Audio: false}
}

func (c *Controller) startSession(ctx context.Context, sessionID, token string, generation uint64, media *protocol.CastStatusMedia) error {
	if c == nil || sessionID == "" || token == "" || generation == 0 {
		return ErrInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ErrStart
	}
	c.mu.Lock()
	if c.sess != nil {
		c.mu.Unlock()
		return ErrBusy
	}
	args := []string{
		"-rtp", c.config.RTPAddress,
		"-control", c.config.Control,
		"-framebuffer", c.config.Framebuffer,
		"-native-cmd", c.config.NativeCmd,
		"-native-mode", c.config.NativeMode,
		"-session", sessionID,
		"-generation", fmt.Sprint(generation),
	}
	tokenFile := c.config.TokenFile
	controllerOwnedTokenFile := false
	if tokenFile == "" {
		file, err := os.CreateTemp("", "fogcast-cast-token-*")
		if err != nil {
			c.mu.Unlock()
			return ErrStart
		}
		tokenFile = file.Name()
		_ = file.Close()
		controllerOwnedTokenFile = true
	}
	cleanupTokenFile := func() error { return cleanupTokenPath(tokenFile, controllerOwnedTokenFile) }
	if err := installTokenFile(tokenFile, []byte(token)); err != nil {
		cleanupPath, remove := tokenFile, controllerOwnedTokenFile
		var installErr *tokenInstallError
		if errors.As(err, &installErr) && installErr.temporaryPath != "" {
			cleanupPath, remove = installErr.temporaryPath, true
		}
		cleanupErr := cleanupTokenPath(cleanupPath, remove)
		if cleanupErr != nil {
			done := make(chan struct{})
			close(done)
			c.sess = &session{done: done, session: sessionID, generation: generation, tokenFile: cleanupPath, removeTokenFile: remove, cleanupErr: cleanupErr, media: media}
		}
		c.mu.Unlock()
		return ErrStart
	}
	args = append(args, "-token-file", tokenFile)
	process, err := c.start(ctx, c.config.Binary, args...)
	if err != nil && process != nil {
		s := &session{process: process, done: make(chan struct{}), session: sessionID, generation: generation, tokenFile: tokenFile, removeTokenFile: controllerOwnedTokenFile, media: media}
		c.sess = s
		c.mu.Unlock()
		go c.reap(s)
		_ = process.Kill()
		timer := time.NewTimer(c.config.StopTimeout)
		defer timer.Stop()
		select {
		case <-s.done:
		case <-ctx.Done():
		case <-timer.C:
		}
		return ErrStart
	}
	if err != nil || process == nil {
		cleanupErr := cleanupTokenFile()
		if cleanupErr != nil {
			done := make(chan struct{})
			close(done)
			c.sess = &session{
				done: done, session: sessionID, generation: generation,
				tokenFile: tokenFile, removeTokenFile: controllerOwnedTokenFile, cleanupErr: cleanupErr,
			}
		}
		c.mu.Unlock()
		return ErrStart
	}
	s := &session{process: process, done: make(chan struct{}), session: sessionID, generation: generation, tokenFile: tokenFile, removeTokenFile: controllerOwnedTokenFile, media: media}
	c.sess = s
	c.mu.Unlock()
	go c.reap(s)
	timer := time.NewTimer(c.config.StartGrace)
	defer timer.Stop()
	select {
	case <-s.done:
		return ErrStart
	case <-ctx.Done():
		_ = s.process.Kill()
		return ErrStart
	case <-timer.C:
		select {
		case <-s.done:
			return ErrStart
		default:
			return nil
		}
	}
}

func installTokenFile(path string, token []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	cleanupTemporary := func(cause error) error {
		closeErr := file.Close()
		removeErr := os.Remove(temporaryPath)
		leftover := ""
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			leftover = temporaryPath
		}
		return &tokenInstallError{cause: errors.Join(cause, closeErr, removeErr), temporaryPath: leftover}
	}
	if err := file.Chmod(0o600); err != nil {
		return cleanupTemporary(err)
	}
	// Claim the configured path atomically while the temporary inode is still
	// empty. Any later write/close failure is therefore cleanup-owned through
	// the configured path; no anonymous temporary file can retain token bytes.
	if err := os.Rename(temporaryPath, path); err != nil {
		return cleanupTemporary(err)
	}
	if _, err := file.Write(token); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

func cleanupTokenPath(path string, remove bool) error {
	if path == "" {
		return nil
	}
	if remove {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return installTokenFile(path, nil)
}

func (c *Controller) reap(s *session) {
	_ = s.process.Wait()
	cleanupErr := cleanupTokenPath(s.tokenFile, s.removeTokenFile)
	c.mu.Lock()
	s.cleanupErr = cleanupErr
	if cleanupErr == nil && c.sess == s {
		c.sess = nil
	}
	c.mu.Unlock()
	close(s.done)
}

func (c *Controller) Stop(ctx context.Context, sessionID string, generation uint64) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	s := c.sess
	if s != nil && (s.session != sessionID || s.generation != generation) {
		c.mu.Unlock()
		return ErrStale
	}
	c.mu.Unlock()
	if s == nil {
		return nil
	}
	if s.process != nil {
		if err := s.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return ErrStop
		}
	}
	timer := time.NewTimer(c.config.StopTimeout)
	defer timer.Stop()
	select {
	case <-s.done:
		c.mu.Lock()
		cleanupErr := s.cleanupErr
		c.mu.Unlock()
		if cleanupErr == nil {
			return nil
		}
		if err := cleanupTokenPath(s.tokenFile, s.removeTokenFile); err != nil {
			return ErrStop
		}
		c.mu.Lock()
		s.cleanupErr = nil
		if c.sess == s {
			c.sess = nil
		}
		c.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ErrStop
	case <-timer.C:
		return ErrStop
	}
}

func (c *Controller) Status(context.Context) Status {
	if c == nil {
		return Status{State: Idle}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess == nil {
		return Status{State: Idle}
	}
	return Status{State: Active, Session: c.sess.session, Generation: c.sess.generation, Media: c.sess.media}
}

type commandProcess struct{ cmd *exec.Cmd }

func (p commandProcess) Wait() error { return p.cmd.Wait() }
func (p commandProcess) Kill() error {
	if p.cmd.Process == nil {
		return os.ErrProcessDone
	}
	return p.cmd.Process.Kill()
}
