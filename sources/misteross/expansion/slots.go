package expansion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// SlotExpansion names the card composed into one physical socket.
type SlotExpansion struct {
	Slot        int    `json:"slot"`
	ExpansionID string `json:"expansion_id"`
}

// SlotComposition identifies a multi-socket shell programmed with one or more
// independently built cards. Like Composition it retains the sealed shell
// package and on-FPGA BUILD_ID; it is not a new package.
type SlotComposition struct {
	ID            string          `json:"composition_id"`
	PackageID     string          `json:"package_id"`
	Expansions    []SlotExpansion `json:"expansions"`
	ShellSHA256   string          `json:"shell_sha256"`
	PayloadSHA256 string          `json:"payload_sha256"`
	PayloadSize   int64           `json:"payload_size"`
}

func slotPolicies(slot, mapping string) map[int]socketPolicy {
	switch {
	case slot == Apple2Slot && mapping == Apple2Map:
		return apple2Sockets
	case slot == SpectrumSlot && mapping == SpectrumMap:
		return spectrumSockets
	case slot == C64Slot && mapping == C64Map:
		return c64Sockets
	case slot == AtariStSlot && mapping == AtariStMap:
		return atariStSockets
	default:
		return nil
	}
}

// SlotMap returns the multi-socket layout a shell's expansion interface uses.
func SlotMap(slot string, major int) (string, bool) {
	return slotMap(slot, major)
}

// slotMap returns the multi-socket layout a shell's expansion interface uses.
func slotMap(slot string, major int) (string, bool) {
	switch {
	case slot == Apple2Slot && major == 1:
		return Apple2Map, true
	case slot == SpectrumSlot && major == 1:
		return SpectrumMap, true
	case slot == C64Slot && major == 1:
		return C64Map, true
	case slot == AtariStSlot && major == 1:
		return AtariStMap, true
	default:
		return "", false
	}
}

// SlotSockets lists the physical socket indices of a multi-socket layout in
// ascending order, or nil when the slot/map pair is not multi-socket.
func SlotSockets(slot, mapping string) []int {
	policies := slotPolicies(slot, mapping)
	if policies == nil {
		return nil
	}
	sockets := make([]int, 0, len(policies))
	for index := range policies {
		sockets = append(sockets, index)
	}
	sort.Ints(sockets)
	return sockets
}

func validSlotExpansions(expansions []SlotExpansion) bool {
	if len(expansions) == 0 || len(expansions) > 7 {
		return false
	}
	for i, e := range expansions {
		if e.Slot < 1 || e.Slot > 7 || !hex64.MatchString(e.ExpansionID) ||
			(i > 0 && expansions[i-1].Slot >= e.Slot) {
			return false
		}
	}
	return true
}

// SlotCompositionID is SHA-256 of "fes-composition-v2", NUL, the package ID,
// NUL, then "slot:expansion-id" and NUL for each card in ascending slot order,
// then the linked payload SHA-256.
func SlotCompositionID(packageID string, expansions []SlotExpansion, payloadSHA256 string) (string, error) {
	if !hex64.MatchString(packageID) || !hex64.MatchString(payloadSHA256) || !validSlotExpansions(expansions) {
		return "", errors.New("invalid slot composition identity")
	}
	material := "fes-composition-v2\x00" + packageID + "\x00"
	for _, e := range expansions {
		material += strconv.Itoa(e.Slot) + ":" + e.ExpansionID + "\x00"
	}
	return hash([]byte(material + payloadSHA256)), nil
}

func sortedBySlot(assets []Asset) []Asset {
	ordered := append([]Asset(nil), assets...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Manifest.SlotIndex < ordered[j].Manifest.SlotIndex
	})
	return ordered
}

// AdmitSlots checks every card against the exact frozen multi-socket shell
// without linking. Each card must name a distinct physical socket.
func AdmitSlots(shell Shell, assets []Asset) error {
	mapping, ok := slotMap(shell.Slot, shell.SlotMajor)
	if !ok || shell.SlotMinor != 0 {
		return errors.New("shell does not declare a supported multi-socket expansion bus")
	}
	policies := slotPolicies(shell.Slot, mapping)
	seen := map[int]bool{}
	payloadHash := hash(shell.Payload)
	for _, asset := range assets {
		if err := asset.Validate(); err != nil {
			return err
		}
		m := asset.Manifest
		if m.Slot != shell.Slot || m.Map != mapping || m.SlotMajor != shell.SlotMajor || m.SlotMinor != shell.SlotMinor {
			return errors.New("card does not target this shell's expansion bus")
		}
		if _, ok := policies[m.SlotIndex]; !ok {
			return fmt.Errorf("slot %d is not a physical socket of this shell", m.SlotIndex)
		}
		if seen[m.SlotIndex] {
			return fmt.Errorf("slot %d has more than one card", m.SlotIndex)
		}
		seen[m.SlotIndex] = true
		if shell.PackageID != m.ShellPackageID || shell.BuildID != m.ShellBuildID || payloadHash != m.ShellSHA256 {
			return errors.New("card was built for a different frozen shell")
		}
	}
	return nil
}

// ComposeSlotsContext links one or more cards into their sockets. Every card
// is compared with the original shell, never with a partly linked result, and
// may change CRAM only inside its own socket rectangle.
func ComposeSlotsContext(ctx context.Context, shell Shell, assets []Asset) (SlotComposition, []byte, error) {
	if err := ctx.Err(); err != nil {
		return SlotComposition{}, nil, err
	}
	if len(assets) == 0 {
		return SlotComposition{}, nil, errors.New("slot composition requires at least one card")
	}
	if err := AdmitSlots(shell, assets); err != nil {
		return SlotComposition{}, nil, err
	}
	ordered := sortedBySlot(assets)
	mapping, _ := slotMap(shell.Slot, shell.SlotMajor)
	policies := slotPolicies(shell.Slot, mapping)
	linked, err := linkSlotsContext(ctx, shell.Payload, ordered, policies)
	if err != nil {
		return SlotComposition{}, nil, fmt.Errorf("link slots: %w", err)
	}
	result := SlotComposition{PackageID: shell.PackageID, ShellSHA256: hash(shell.Payload),
		PayloadSHA256: hash(linked), PayloadSize: int64(len(linked))}
	for _, asset := range ordered {
		result.Expansions = append(result.Expansions, SlotExpansion{Slot: asset.Manifest.SlotIndex, ExpansionID: asset.ID})
	}
	result.ID, err = SlotCompositionID(result.PackageID, result.Expansions, result.PayloadSHA256)
	return result, linked, err
}

func linkSlotsContext(ctx context.Context, shell []byte, assets []Asset, policies map[int]socketPolicy) ([]byte, error) {
	overlays := make([]regionOverlay, 0, len(assets))
	for _, asset := range assets {
		overlays = append(overlays, regionOverlay{label: fmt.Sprintf("slot %d", asset.Manifest.SlotIndex),
			cart: asset.Cart, policy: policies[asset.Manifest.SlotIndex]})
	}
	return linkRegionOverlaysContext(ctx, shell, overlays)
}

// regionOverlay is internal: callers can select only the closed socket policies
// admitted by their public composition API, never upload their own rectangles.
type regionOverlay struct {
	label  string
	cart   []byte
	policy socketPolicy
}

func linkRegionOverlaysContext(ctx context.Context, shell []byte, overlays []regionOverlay) ([]byte, error) {
	base, err := loadFramesContext(ctx, shell)
	if err != nil {
		return nil, fmt.Errorf("shell: %w", err)
	}
	result := bytes.Clone(base.frames)
	dirty := make([]bool, cramWidth)
	for _, overlay := range overlays {
		policy := overlay.policy
		addition, err := loadFramesContext(ctx, overlay.cart)
		if err != nil {
			return nil, fmt.Errorf("%s cart: %w", overlay.label, err)
		}
		if !bytes.Equal(base.header, addition.header) {
			return nil, fmt.Errorf("%s cart changes shell ORAM/PRAM header", overlay.label)
		}
		for x := 0; x < cramWidth; x++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			before := base.frames[x*frameBytes : (x+1)*frameBytes]
			after := addition.frames[x*frameBytes : (x+1)*frameBytes]
			// The wide native fence includes real routing bits in columns the
			// legacy profiles retain from the shell. Admit every decoded native
			// change against its exact rectangle; frame CRCs are refreshed below.
			if (policy != colecoNativeVideoSocket && policy != atariStVideoSocket && crcCompanionColumn(x)) || bytes.Equal(before, after) {
				continue
			}
			target := result[x*frameBytes : (x+1)*frameBytes]
			for y := 32; y < cramHeight; y++ {
				pos := frameBit(y)
				mask := byte(1 << (pos % 8))
				if (before[pos/8]^after[pos/8])&mask == 0 {
					continue
				}
				if !policy.inside(x, y) {
					return nil, fmt.Errorf("%s cart changes CRAM outside its socket at %d,%d", overlay.label, x, y)
				}
				target[pos/8] = (target[pos/8] &^ mask) | (after[pos/8] & mask)
				dirty[x] = true
			}
		}
	}
	for x, changed := range dirty {
		if changed {
			refreshFrameChecksums(result[x*frameBytes:(x+1)*frameBytes], x)
		}
	}
	packed, err := compressContext(ctx, result)
	if err != nil {
		return nil, err
	}
	linked := append(bytes.Clone(base.header), packed...)
	return append(linked, postamble()...), nil
}

// ComposeSlotsROM binds the ROM map to the original sealed multi-socket shell,
// rejects ROM destinations inside any physical socket, links the selected
// cards (possibly none) and patches the ROM. It returns the composition (zero
// when no card is selected), the card overlay and the final programmed RBF.
func ComposeSlotsROM(ctx context.Context, shell Shell, assets []Asset, m ROMMap, rom []byte) (SlotComposition, []byte, []byte, error) {
	mapping, ok := slotMap(shell.Slot, shell.SlotMajor)
	if !ok {
		return SlotComposition{}, nil, nil, errors.New("shell does not declare a supported multi-socket expansion bus")
	}
	if err := validateROMMap(ctx, m, hash(shell.Payload), len(rom)); err != nil {
		return SlotComposition{}, nil, nil, err
	}
	policies := slotPolicies(shell.Slot, mapping)
	for _, block := range m.Blocks {
		for _, bits := range block.WordBits {
			for _, p := range bits {
				x, y := int(p)%cramWidth, int(p)/cramWidth
				for index, policy := range policies {
					if policy.inside(x, y) {
						return SlotComposition{}, nil, nil, fmt.Errorf("ROM destination overlaps slot %d socket", index)
					}
				}
			}
		}
	}
	overlay := shell.Payload
	var composition SlotComposition
	if len(assets) != 0 {
		var err error
		composition, overlay, err = ComposeSlotsContext(ctx, shell, assets)
		if err != nil {
			return SlotComposition{}, nil, nil, err
		}
	}
	programmed, err := patchROM(ctx, overlay, m, rom)
	if err != nil {
		return SlotComposition{}, nil, nil, err
	}
	return composition, overlay, programmed, nil
}
