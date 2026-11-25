// SPDX-License-Identifier: GPL-3.0-or-later
package bt

import (
	"context"
	"errors"
	"io/fs"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
)

// BondStorage is the minimal packagestore.Store subset required to load/save
// FES/BTBONDS.JSON and read FES/BTFW/* blobs without coupling bt to FAT32.
type BondStorage interface {
	ReadBTBonds(ctx context.Context) ([]byte, error)
	WriteBTBonds(ctx context.Context, data []byte) error
	ReadBTFirmware(ctx context.Context, name string) ([]byte, error)
}

type storeFWLoader struct {
	ctx   context.Context
	store BondStorage
}

func (l storeFWLoader) LoadFirmware(name string) ([]byte, error) {
	if l.store == nil {
		return nil, fs.ErrNotExist
	}
	return l.store.ReadBTFirmware(l.ctx, name)
}

// NewStoreFirmwareLoader adapts a packagestore.Store (or mock BondStorage) to
// hci.FirmwareLoader for loading Realtek rtl8761bu_fw.bin / rtl8761bu_config.bin.
func NewStoreFirmwareLoader(ctx context.Context, store BondStorage) hci.FirmwareLoader {
	return storeFWLoader{ctx: ctx, store: store}
}

// LoadBonds reads FES/BTBONDS.JSON into a BondStore. A missing bond file
// returns an empty BondStore and nil error so fresh SD cards boot cleanly.
func LoadBonds(ctx context.Context, store BondStorage) (*l2cap.BondStore, error) {
	bonds := l2cap.NewBondStore()
	if store == nil {
		return bonds, nil
	}
	raw, err := store.ReadBTBonds(ctx)
	if errors.Is(err, fs.ErrNotExist) {
		return bonds, nil
	}
	if err != nil {
		return bonds, err
	}
	if err := bonds.UnmarshalJSON(raw); err != nil {
		return bonds, err
	}
	return bonds, nil
}

// SaveBondsIfDirty checkpoints FES/BTBONDS.JSON when bonds have changed and
// clears the dirty flag on success.
func SaveBondsIfDirty(ctx context.Context, store BondStorage, bonds *l2cap.BondStore) error {
	if store == nil || bonds == nil || !bonds.Dirty() {
		return nil
	}
	raw, err := bonds.MarshalJSON()
	if err != nil {
		return err
	}
	if err := store.WriteBTBonds(ctx, raw); err != nil {
		return err
	}
	bonds.ClearDirty()
	return nil
}
