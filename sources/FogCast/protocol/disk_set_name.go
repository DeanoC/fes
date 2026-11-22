package protocol

import (
	"regexp"
	"strconv"
	"strings"
)

var diskSetNameRE = regexp.MustCompile(`(?i)\s*\(\s*(?:disk|disc|tape)\s*(\d{1,2})(?:\s*of\s*(\d{1,2}))?\s*\)`)

// DiskSetPosition reads a set file name such as "Game (Disk 1 of 2).d64" or
// "Game (Disk 2).st". Files of one set share key, the lower-case name with
// the disk tag taken out. count is 0 when the name does not give it. ok is
// false for a name with no disk tag, or with disk 0 or a disk past its count.
func DiskSetPosition(name string) (key string, index, count int, ok bool) {
	loc := diskSetNameRE.FindStringSubmatchIndex(name)
	if loc == nil {
		return "", 0, 0, false
	}
	index, _ = strconv.Atoi(name[loc[2]:loc[3]])
	if loc[4] >= 0 {
		count, _ = strconv.Atoi(name[loc[4]:loc[5]])
	}
	if index < 1 || (count > 0 && index > count) {
		return "", 0, 0, false
	}
	key = strings.ToLower(name[:loc[0]] + name[loc[1]:])
	return key, index, count, true
}
