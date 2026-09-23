// fes-rom-link is a host/kit diagnostic for trusted producer ROM maps. It does
// not admit packages or program hardware; production launch integration is
// deliberately left to the target agent.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/DeanoC/misteross/expansion"
)

func boundedRead(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds input limit", path)
	}
	return data, nil
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("fes-rom-link", flag.ContinueOnError)
	basePath := flags.String("base", "", "blank RBF")
	mapPath := flags.String("map", "", "trusted producer ROM map JSON")
	romPath := flags.String("rom", "", "exact binary ROM bytes")
	outPath := flags.String("output", "", "output RBF (atomically replaced on success)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *basePath == "" || *mapPath == "" || *romPath == "" || *outPath == "" {
		return errors.New("require -base, -map, -rom and -output")
	}
	base, err := boundedRead(*basePath, 32<<20)
	if err != nil {
		return err
	}
	rawMap, err := boundedRead(*mapPath, 32<<20)
	if err != nil {
		return err
	}
	rom, err := boundedRead(*romPath, 256<<10)
	if err != nil {
		return err
	}
	mapping, err := expansion.ParseROMMap(ctx, rawMap, fmt.Sprintf("%x", sha256.Sum256(base)), len(rom))
	if err != nil {
		return err
	}
	linked, err := expansion.LinkROM(ctx, base, mapping, rom)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(*outPath), ".fes-rom-link-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(linked); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), *outPath)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
