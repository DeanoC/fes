package mister

import (
	"os"
	"path/filepath"
	"strings"
)

type ProcProcessChecker struct {
	Root string
}

func (p ProcProcessChecker) Running(comm string) bool {
	entries, err := filepath.Glob(filepath.Join(p.Root, "[0-9]*", "comm"))
	if err != nil {
		return false
	}
	for _, path := range entries {
		b, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(b)) == comm {
			return true
		}
	}
	return false
}
