// SPDX-License-Identifier: GPL-3.0-or-later
package l2cap

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
)

const (
	MaxBondedDevices = 16
	MaxBTBondsBytes  = 16384
)

// BDAddr is a 6-byte Bluetooth device address stored in HCI wire order
// (little-endian: byte 0 is the LSB, byte 5 is the MSB).
type BDAddr [6]byte

// String formats BDAddr in standard colon-separated MSB-first hex notation.
func (a BDAddr) String() string {
	const hexDigits = "0123456789ABCDEF"
	var out [17]byte
	for i := 0; i < 6; i++ {
		b := a[5-i]
		out[i*3] = hexDigits[b>>4]
		out[i*3+1] = hexDigits[b&0x0f]
		if i < 5 {
			out[i*3+2] = ':'
		}
	}
	return string(out[:])
}

// ParseBDAddr parses an "AA:BB:CC:DD:EE:FF" MAC string into HCI wire order.
func ParseBDAddr(s string) (BDAddr, error) {
	var a BDAddr
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 6 {
		return a, fmt.Errorf("bt/l2cap: invalid BD_ADDR %q", s)
	}
	for i := 0; i < 6; i++ {
		if len(parts[i]) != 2 {
			return a, fmt.Errorf("bt/l2cap: invalid BD_ADDR octet %q", parts[i])
		}
		b, err := hex.DecodeString(parts[i])
		if err != nil || len(b) != 1 {
			return a, fmt.Errorf("bt/l2cap: invalid BD_ADDR hex %q", parts[i])
		}
		a[5-i] = b[0]
	}
	return a, nil
}

// BondRecord persists one SSP-paired Classic Bluetooth controller.
type BondRecord struct {
	Addr      BDAddr
	LinkKey   [16]byte
	Name      string
	VendorID  uint16
	ProductID uint16
	Version   uint16
	Kind      controllers.Kind
	Slot      uint8
}

type bondJSON struct {
	Addr      string `json:"addr"`
	LinkKey   string `json:"link_key"`
	Name      string `json:"name,omitempty"`
	VendorID  uint16 `json:"vendor_id,omitempty"`
	ProductID uint16 `json:"product_id,omitempty"`
	Version   uint16 `json:"version,omitempty"`
	Kind      uint8  `json:"kind,omitempty"`
	Slot      uint8  `json:"slot,omitempty"`
}

type bondsFileJSON struct {
	Version int        `json:"version"`
	Bonds   []bondJSON `json:"bonds"`
}

// BondStore maintains up to MaxBondedDevices paired Classic Bluetooth devices
// with bounded JSON persistence compatible with FES/BTBONDS.JSON.
type BondStore struct {
	records []BondRecord
	dirty   bool
}

// NewBondStore creates an empty in-memory BondStore.
func NewBondStore() *BondStore { return &BondStore{} }

// Lookup finds a bonded controller by BDAddr.
func (s *BondStore) Lookup(addr BDAddr) (BondRecord, bool) {
	if s == nil {
		return BondRecord{}, false
	}
	for _, r := range s.records {
		if r.Addr == addr {
			return r, true
		}
	}
	return BondRecord{}, false
}

// Upsert inserts or updates a bonded controller record and marks the store dirty.
func (s *BondStore) Upsert(rec BondRecord) error {
	if s == nil {
		return errors.New("bt/l2cap: nil bond store")
	}
	for i := range s.records {
		if s.records[i].Addr == rec.Addr {
			if rec.Name == "" {
				rec.Name = s.records[i].Name
			}
			if rec.Kind == controllers.KindUnknown {
				rec.Kind = s.records[i].Kind
			}
			if rec.VendorID == 0 {
				rec.VendorID = s.records[i].VendorID
				rec.ProductID = s.records[i].ProductID
				rec.Version = s.records[i].Version
			} else if rec.Version == 0 && rec.VendorID == s.records[i].VendorID && rec.ProductID == s.records[i].ProductID {
				rec.Version = s.records[i].Version
			}
			s.records[i] = rec
			s.dirty = true
			return nil
		}
	}
	if len(s.records) >= MaxBondedDevices {
		// Evict oldest bond when full so new pairings always succeed deterministically.
		s.records = s.records[1:]
	}
	s.records = append(s.records, rec)
	s.dirty = true
	return nil
}

// Remove deletes a bonded controller by address.
func (s *BondStore) Remove(addr BDAddr) bool {
	if s == nil {
		return false
	}
	for i := range s.records {
		if s.records[i].Addr == addr {
			s.records = append(s.records[:i], s.records[i+1:]...)
			s.dirty = true
			return true
		}
	}
	return false
}

// List returns a copy of all bonded controllers.
func (s *BondStore) List() []BondRecord {
	if s == nil || len(s.records) == 0 {
		return nil
	}
	out := make([]BondRecord, len(s.records))
	copy(out, s.records)
	return out
}

// Dirty reports whether bond records changed since the last ClearDirty.
func (s *BondStore) Dirty() bool { return s != nil && s.dirty }

// ClearDirty resets the dirty flag after persisting FES/BTBONDS.JSON.
func (s *BondStore) ClearDirty() {
	if s != nil {
		s.dirty = false
	}
}

// MarshalJSON encodes the bond store as a bounded, deterministic JSON object.
func (s *BondStore) MarshalJSON() ([]byte, error) {
	file := bondsFileJSON{Version: 1, Bonds: make([]bondJSON, 0, len(s.records))}
	sorted := s.List()
	slices.SortFunc(sorted, func(a, b BondRecord) int {
		return bytes.Compare(a.Addr[:], b.Addr[:])
	})
	for _, r := range sorted {
		file.Bonds = append(file.Bonds, bondJSON{
			Addr:      r.Addr.String(),
			LinkKey:   hex.EncodeToString(r.LinkKey[:]),
			Name:      r.Name,
			VendorID:  r.VendorID,
			ProductID: r.ProductID,
			Version:   r.Version,
			Kind:      uint8(r.Kind),
			Slot:      r.Slot,
		})
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxBTBondsBytes {
		return nil, fmt.Errorf("bt/l2cap: serialized bonds exceed %d bytes", MaxBTBondsBytes)
	}
	return raw, nil
}

// UnmarshalJSON loads bounded FES/BTBONDS.JSON bytes into the BondStore.
func (s *BondStore) UnmarshalJSON(data []byte) error {
	if s == nil {
		return errors.New("bt/l2cap: nil bond store")
	}
	if len(data) == 0 || len(data) > MaxBTBondsBytes {
		return fmt.Errorf("bt/l2cap: invalid bond file size %d", len(data))
	}
	var file bondsFileJSON
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	if file.Version != 1 {
		return fmt.Errorf("bt/l2cap: unsupported bond file version %d", file.Version)
	}
	if len(file.Bonds) > MaxBondedDevices {
		return fmt.Errorf("bt/l2cap: too many bond records (%d)", len(file.Bonds))
	}
	next := make([]BondRecord, 0, len(file.Bonds))
	for _, b := range file.Bonds {
		addr, err := ParseBDAddr(b.Addr)
		if err != nil {
			return err
		}
		keyBytes, err := hex.DecodeString(b.LinkKey)
		if err != nil || len(keyBytes) != 16 {
			return fmt.Errorf("bt/l2cap: invalid link key for %s", b.Addr)
		}
		var lk [16]byte
		copy(lk[:], keyBytes)
		next = append(next, BondRecord{
			Addr:      addr,
			LinkKey:   lk,
			Name:      b.Name,
			VendorID:  b.VendorID,
			ProductID: b.ProductID,
			Version:   b.Version,
			Kind:      controllers.Kind(b.Kind),
			Slot:      b.Slot,
		})
	}
	s.records = next
	s.dirty = false
	return nil
}
