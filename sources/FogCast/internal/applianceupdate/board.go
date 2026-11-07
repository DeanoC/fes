package applianceupdate

import (
	"errors"
	"fmt"
	"io"
	"os"

	release "github.com/DeanoC/FogCast/appliance"
)

// ImageBoard reads the immutable image's board selection when a bootstrap
// factory exists. Direct-root images keep their existing no-update path.
// Older ARM DE10 images may lack the board file; AArch64 requires DE25's exact
// file. A release manifest or request never supplies this selection.
func ImageBoard(factoryPath, boardPath, architecture string) (string, error) {
	if _, err := os.Lstat(factoryPath); errors.Is(err, os.ErrNotExist) {
		return release.Board, nil
	} else if err != nil {
		return "", err
	}
	info, err := os.Lstat(boardPath)
	if errors.Is(err, os.ErrNotExist) && architecture == "arm" {
		return release.Board, nil
	}
	if err != nil {
		return "", fmt.Errorf("read image board selection: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("image board selection must be a regular file")
	}
	f, err := os.Open(boardPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65))
	if err != nil {
		return "", err
	}
	if architecture == "arm" && string(raw) == release.Board+"\n" {
		return release.Board, nil
	}
	if architecture == "arm64" && string(raw) == release.BoardDE25+"\n" {
		return release.BoardDE25, nil
	}
	return "", errors.New("image board selection differs from the supported CPU target")
}
