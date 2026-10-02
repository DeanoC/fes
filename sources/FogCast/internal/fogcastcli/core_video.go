package fogcastcli

import (
	"bytes"
	"io"
	"os"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// Hold bounded archive bytes and their parsed identity before starting HTTP.
// The upload does not re-read a path that another process could replace.
func snapshotVideoPart(path string) ([]byte, expansion.Asset, error) {
	invalid := func() ([]byte, expansion.Asset, error) {
		return nil, expansion.Asset{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "video import requires a regular, bounded .fexp archive for the supported video socket"}
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > catalog.MaxCoreMediaBytes {
		return invalid()
	}
	file, err := os.Open(path)
	if err != nil {
		return invalid()
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() != before.Size() {
		return invalid()
	}
	data, err := io.ReadAll(io.LimitReader(file, before.Size()+1))
	if err != nil || int64(len(data)) != before.Size() {
		return invalid()
	}
	asset, err := expansion.ReadAsset(bytes.NewReader(data))
	if err != nil || asset.Manifest.Slot != expansion.VideoSlot {
		return invalid()
	}
	return data, asset, nil
}
