package protocol

import (
	"fmt"
	"regexp"
)

var gameIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func ValidateGameID(id string) error {
	if !gameIDPattern.MatchString(id) {
		return fmt.Errorf("game ID %q must be a lowercase ASCII slug", id)
	}
	return nil
}

func ValidateSystem(system System) error {
	switch system {
	case SystemMegaDrive, SystemSNES:
		return nil
	default:
		return fmt.Errorf("unsupported system %q", system)
	}
}
