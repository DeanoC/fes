package protocol

import (
	"fmt"
	"regexp"
)

var (
	gameIDPattern    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	digestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	extensionPattern = regexp.MustCompile(`^[a-z0-9]+$`)
)

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

func ValidateDigest(digest string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("SHA-256 digest %q must be 64 lowercase hexadecimal characters", digest)
	}
	return nil
}

func ValidateExtension(extension string) error {
	if !extensionPattern.MatchString(extension) {
		return fmt.Errorf("extension %q must contain only lowercase ASCII letters and digits", extension)
	}
	return nil
}

func ValidateContentKey(key ContentKey) error {
	if err := ValidateDigest(key.SHA256); err != nil {
		return err
	}
	return ValidateExtension(key.Extension)
}

func ValidateContentIdentity(content ContentIdentity) error {
	if err := ValidateContentKey(content.Key()); err != nil {
		return err
	}
	if content.Size < 1 || content.Size > MaxContentBytes {
		return fmt.Errorf("content size %d must be between 1 and %d bytes", content.Size, MaxContentBytes)
	}
	return nil
}
