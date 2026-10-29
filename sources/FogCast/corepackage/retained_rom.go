package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

// ROMSource is one exact source in sealed descriptor order. Callers must not
// mutate input concurrently with LinkROMs; the returned object owns its copies.
type ROMSource struct {
	ID, Role string
	Data     []byte
}

// ROMActivationReceipt binds a package and its independently linked output.
// It describes content, not hardware admission, authentication or durable state.
type ROMActivationReceipt struct {
	PackageID string            `json:"package_id"`
	ROMLink   *ROMLinkIdentity  `json:"rom_link,omitempty"`
	ROMLinks  *ROMLinksIdentity `json:"rom_links,omitempty"`
}

// LinkedArchive owns the package, source ROMs and derived programming bytes.
// Only LinkROMs constructs a usable value; caller-supplied receipts/bitstreams
// cannot be substituted. No filesystem staging or physical activation occurs.
type LinkedArchive struct {
	archive       *Archive
	sources       []ROMSource
	programmed    []byte
	receipt       ROMActivationReceipt
	receiptSHA256 string
}

// LinkROMs uses the sealed map and the same target linker as ROM-input staging.
// This entrypoint links ROMs alone; expansion/parts and initial media remain on
// their existing paths. Format 3 takes one source; format 4 takes BIOS then cart.
func (a *Archive) LinkROMs(ctx context.Context, sources []ROMSource) (*LinkedArchive, error) {
	if ctx == nil || a == nil || a.inspection.PackageID == "" {
		return nil, errors.New("missing retained ROM package or context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d := a.inspection.Descriptor
	var requirements []ROMRequirement
	switch d.Format {
	case 3:
		if d.ROM == nil {
			return nil, errors.New("missing sealed ROM requirement")
		}
		requirements = []ROMRequirement{{ID: d.ROM.ID, Role: d.ROM.Role, SourceSize: d.ROM.SourceSize}}
	case 4:
		requirements = d.ROMs
	default:
		return nil, errors.New("retained ROM linking requires format 3 or 4")
	}
	if len(sources) != len(requirements) {
		return nil, errors.New("ROM source count differs from sealed requirements")
	}
	// Validate all sizes and identities before allocating private source buffers.
	for i, r := range requirements {
		if sources[i].ID != r.ID || sources[i].Role != r.Role || int64(len(sources[i].Data)) != r.SourceSize {
			return nil, errors.New("ROM source identity, role or size differs from sealed requirement")
		}
	}
	owned := make([]ROMSource, len(sources))
	for i, s := range sources {
		owned[i] = ROMSource{s.ID, s.Role, bytes.Clone(s.Data)}
	}
	descriptor, parsed, err := decodeWithROMMapContext(ctx, a.manifest, a.payload, a.romMap)
	if err != nil {
		return nil, err
	}
	inspection := Inspection{PackageID: a.inspection.PackageID, Descriptor: descriptor}
	linked := &LinkedArchive{archive: a, sources: owned, receipt: ROMActivationReceipt{PackageID: inspection.PackageID}}
	if d.Format == 3 {
		s := owned[0]
		receipt := romInputReceipt{Format: 1, PackageID: inspection.PackageID, ROMID: s.ID, MapSHA256: d.ROM.SHA256, SourceSHA256: romDigest(s.Data), SourceSize: int64(len(s.Data))}
		result, err := linkPreparedROMInput(ctx, preparedROMInput{input: ROMInput{ROM: s.Data}, inspection: inspection, base: a.payload, mapping: a.romMap, receipt: receipt, romMap: parsed}, romInputReceipt{})
		if err != nil {
			return nil, err
		}
		linked.programmed = result.programmed
		linked.receipt.ROMLink = &result.identity
	} else {
		receipt := romInputReceiptV2{Format: 2, PackageID: inspection.PackageID, MapSHA256: d.ROMMap.SHA256}
		for _, s := range owned {
			receipt.Sources = append(receipt.Sources, ROMSourceIdentity{ID: s.ID, Role: s.Role, SourceSHA256: romDigest(s.Data), SourceSize: int64(len(s.Data))})
		}
		result, err := linkPreparedROMInputV2(ctx, preparedROMInputV2{input: ROMInputV2{BIOS: owned[0].Data, Cartridge: owned[1].Data}, inspection: inspection, base: a.payload, mapping: a.romMap, receipt: receipt, romMap: parsed})
		if err != nil {
			return nil, err
		}
		linked.programmed = result.programmed
		linked.receipt.ROMLinks = &result.identity
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(linked.receipt)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(append([]byte("FES-RETAINED-ROM-ACTIVATION-1\n"), encoded...))
	linked.receiptSHA256 = hex.EncodeToString(sum[:])
	return linked, nil
}

func (l *LinkedArchive) Inspection() Inspection {
	if l == nil || l.archive == nil {
		return Inspection{}
	}
	return l.archive.Inspection()
}
func (l *LinkedArchive) Receipt() ROMActivationReceipt {
	if l == nil {
		return ROMActivationReceipt{}
	}
	r := l.receipt
	if r.ROMLink != nil {
		value := *r.ROMLink
		r.ROMLink = &value
	}
	if r.ROMLinks != nil {
		value := *r.ROMLinks
		value.Sources = append([]ROMSourceIdentity(nil), value.Sources...)
		r.ROMLinks = &value
	}
	return r
}
func (l *LinkedArchive) ReceiptSHA256() string {
	if l == nil {
		return ""
	}
	return l.receiptSHA256
}
func (l *LinkedArchive) Programmed() io.Reader {
	if l == nil {
		return bytes.NewReader(nil)
	}
	return &contextReader{ctx: context.Background(), reader: bytes.NewReader(l.programmed)}
}

// Source returns an independent copying cursor over one retained source.
func (l *LinkedArchive) Source(id, role string) (io.Reader, error) {
	if l != nil {
		for _, s := range l.sources {
			if s.ID == id && s.Role == role {
				return &contextReader{ctx: context.Background(), reader: bytes.NewReader(s.Data)}, nil
			}
		}
	}
	return nil, errors.New("ROM source is not retained")
}
