package romsource

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
)

func exclusiveRenamePrepared(root *os.Root, candidate, quarantine string, expected fs.FileInfo) error {
	if expected == nil || candidate == "" || path.IsAbs(candidate) || path.Clean(candidate) != candidate || candidate == "." {
		return errors.New("invalid prepared ROM candidate")
	}
	parentName, base := path.Split(candidate)
	parentName = strings.TrimSuffix(parentName, "/")
	if parentName == "" {
		parentName = "."
	}
	if base == "" || base == "." || base == ".." {
		return errors.New("invalid prepared ROM candidate basename")
	}
	parent, err := root.OpenRoot(parentName)
	if err != nil {
		return err
	}
	defer parent.Close()
	current, err := parent.Lstat(base)
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(expected, current) {
		return errors.New("prepared ROM candidate identity changed")
	}
	return exclusiveRenamePreparedAt(parent, base, root, quarantine)
}
