package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/DeanoC/FogCast-POC/internal/bridge"
)

func main() {
	addr := flag.String("listen", "0.0.0.0:18183", "TCP listen address")
	token := flag.String("token", "", "session token (hex; generated when omitted)")
	session := flag.Uint64("session", 0, "non-zero session id")
	core := flag.String("core", "", "expected core identity")
	device := flag.String("uinput", "/dev/uinput", "uinput device path")
	flag.Parse()
	if *session == 0 || *core == "" {
		fmt.Fprintln(os.Stderr, "mister-bridge: --session and --core are required")
		os.Exit(2)
	}
	tok, err := parseToken(*token)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	sink, err := bridge.OpenUInput(*device)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mister-bridge: uinput: %v\n", err)
		os.Exit(1)
	}
	s, err := bridge.New(bridge.Config{Addr: *addr, Token: tok, Session: *session, Core: *core, Logger: slog.Default()}, sink)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.ListenAndServe(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "mister-bridge:", err)
		os.Exit(1)
	}
}
func parseToken(v string) ([]byte, error) {
	if v == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "mister-bridge token:", hex.EncodeToString(b))
		return b, nil
	}
	b, err := hex.DecodeString(v)
	if err != nil || len(b) < 16 {
		return nil, fmt.Errorf("token must be at least 16 bytes of hex")
	}
	return b, nil
}
