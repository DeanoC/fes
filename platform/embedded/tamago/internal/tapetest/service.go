//go:build !tamago

// Package tapetest connects test callers to the actual persistent C tape service.
package tapetest

import (
	"bufio"
	"bytes"
	"crypto/sha512"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Caller starts on first publication so the fixed descriptor and bytes are ready.
// Every invocation crosses stdin/stdout into the same C service state.
func Caller(t *testing.T, executable string, mode int, source func() ([]byte, []byte)) func([7]uint64) ([4]uint64, error) {
	t.Helper()
	var exchange func([7]uint64) ([4]uint64, error)
	return func(args [7]uint64) ([4]uint64, error) {
		if exchange == nil {
			header, image := source()
			dir := t.TempDir()
			digest := sha512.Sum384(image)
			for name, data := range map[string][]byte{"header": header, "image": image, "digest": digest[:]} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(executable, fmt.Sprint(mode), fmt.Sprint(len(image)), filepath.Join(dir, "header"), filepath.Join(dir, "image"), filepath.Join(dir, "digest"), "--step-server")
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(output)
			t.Cleanup(func() {
				input.Close()
				if err := cmd.Wait(); err != nil {
					t.Errorf("C service: %v %s", err, stderr.String())
				}
			})
			exchange = func(a [7]uint64) (r [4]uint64, err error) {
				if _, err = fmt.Fprintf(input, "%d %d %d\n", a[1], a[2], a[4]); err != nil {
					return
				}
				line, e := reader.ReadString('\n')
				if e != nil {
					return r, e
				}
				_, err = fmt.Sscanf(line, "REPLY %d %d %d %d", &r[0], &r[1], &r[2], &r[3])
				return
			}
		}
		return exchange(args)
	}
}
