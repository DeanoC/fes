// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// NewStoredConfigurationTransfer joins owned storage containment to the cold
// configuration protocol. The package must match the compiled sealed source;
// this is not general runtime compatibility admission. No filesystem handle is
// used after containment. A reservation and uncertain transfer stay owned by
// the caller, including on error; this function never releases either device.
func (v *HPSContextVerifier) NewStoredConfigurationTransfer(control, configuration []byte, handoff *packagestore.Handoff, reserve func([]byte) (*Region, error)) (*ConfigurationTransfer, HPSBootObservation, error) {
	m, err := v.configurationMap(configuration)
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	if _, err = v.checkedControlMap(control); err != nil {
		return nil, HPSBootObservation{}, err
	}
	if reserve == nil {
		return nil, HPSBootObservation{}, errors.New("native fabric: retained configuration reservation missing")
	}
	selection, err := handoff.Selection()
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	inspection := selection.Inspection()
	d, id := inspection.Descriptor, v.Identity()
	if inspection.PackageID != id.PackageID || d.Format != 2 || d.Core.ID != id.CoreID ||
		d.Target.Platform != id.Platform || d.Target.Device != id.Device || d.Target.ProgrammingProfile != id.ProgrammingProfile ||
		d.ABI.ID != id.ABI || d.ABI.Major != int64(id.Major) || d.ABI.Minor != int64(id.Minor) ||
		d.Build.ID != id.BuildID || d.Payload.Size != int64(m.CoreSize) || d.Payload.SHA256 != m.CoreSHA256 {
		return nil, HPSBootObservation{}, errors.New("native fabric: retained package differs from compiled cold source")
	}
	receipt := handoff.Receipt()
	source, err := v.Verify(id)
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	if !v.coldSource(source) || receipt.Active || receipt.Prepared || receipt.Owner != source.Bridges.Owner || receipt.Generation != source.Bridges.Generation {
		return nil, HPSBootObservation{}, errors.New("native fabric: storage containment differs from live cold ownership")
	}
	data, err := io.ReadAll(io.LimitReader(selection.Archive().Payload(), int64(m.CoreSize)+1))
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	digest := sha256.Sum256(data)
	if len(data) != m.CoreSize || hex.EncodeToString(digest[:]) != m.CoreSHA256 {
		return nil, HPSBootObservation{}, errors.New("native fabric: retained cold payload changed")
	}
	region, err := reserve(data)
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	transfer, err := v.NewColdConfigurationTransfer(control, configuration, region, source)
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	prepare := transfer.beforeStart
	transfer.beforeStart = func() error {
		current, err := handoff.Selection()
		if err != nil {
			return err
		}
		if current != selection || handoff.Receipt() != receipt {
			return errors.New("native fabric: retained storage handoff changed before configuration")
		}
		return prepare()
	}
	return transfer, source, nil
}
