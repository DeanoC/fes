package appliancedata

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
)

const (
	ext4SuperOffset = 1024
	ext4MagicOffset = 56
	ext4Magic       = 0xEF53
	ext4LabelOffset = 120
	ext4LabelLength = 16
)

func readExt4Label(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, ext4SuperOffset+ext4LabelOffset+ext4LabelLength)
	if _, err := io.ReadFull(f, buf); err != nil {
		return "", err
	}
	return ext4LabelFromSuperblock(buf)
}

func ext4LabelFromSuperblock(buf []byte) (string, error) {
	need := ext4SuperOffset + ext4LabelOffset + ext4LabelLength
	if len(buf) < need {
		return "", errors.New("ext4 superblock is truncated")
	}
	magic := binary.LittleEndian.Uint16(buf[ext4SuperOffset+ext4MagicOffset:])
	if magic != ext4Magic {
		return "", errors.New("device is not ext4")
	}
	label := buf[ext4SuperOffset+ext4LabelOffset : ext4SuperOffset+ext4LabelOffset+ext4LabelLength]
	return strings.TrimRight(string(label), "\x00"), nil
}
