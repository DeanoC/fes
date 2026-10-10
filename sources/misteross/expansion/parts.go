package expansion

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// These contracts describe the developer Coleco video-part proof. They do not
// extend the production expansion buses or any runtime capability registry.
const (
	VideoSlot               = "fes.fabric.video.raster-rgb888"
	ColecoVideoMap          = "fes.coleco-video.socket/1"
	ColecoVideoLayout       = "fes.coleco-video.parts/1"
	NativeVideoSlot         = "fes.fabric.video.native-pixels"
	ColecoNativeVideoMap    = "fes.coleco-native-video.socket/1"
	ColecoNativeVideoLayout = "fes.coleco-native-video.parts/1"
	AtariStVideoMap         = "fes.atari-st-video.socket/1"
	AtariStVideoLayout      = "fes.atari-st-video.parts/1"
	PartRoleExpansion       = "expansion"
	PartRoleVideo           = "video"
)

// The CPU socket ends at row 1800. Starting at the Apple II slot-4 boundary
// (1722) would overlap it, so this video rectangle deliberately begins later.
var colecoVideoSocket = socketPolicy{VideoSlot, ColecoVideoMap, 1769, 1800, 2806, 3442}

// Native frame capture needs the wider placement columns 5..38, rows 23..38.
// These authenticated CRAM bounds include the M10K data and local mux bits.
var colecoNativeVideoSocket = socketPolicy{NativeVideoSlot, ColecoNativeVideoMap, 124, 1800, 3906, 3442}

var atariStVideoSocket = socketPolicy{VideoSlot, AtariStVideoMap, 1769, 3442, 2806, 5162}

func supportedVideoPart(m Manifest) bool {
	return m.SlotMajor == 1 && ((m.Slot == VideoSlot && (m.Map == ColecoVideoMap || m.Map == AtariStVideoMap)) ||
		(m.Slot == NativeVideoSlot && m.Map == ColecoNativeVideoMap))
}

func partsVideoPolicy(layout string) (socketPolicy, bool) {
	switch layout {
	case ColecoVideoLayout:
		return colecoVideoSocket, true
	case ColecoNativeVideoLayout:
		return colecoNativeVideoSocket, true
	case AtariStVideoLayout:
		return atariStVideoSocket, true
	default:
		return socketPolicy{}, false
	}
}

// PartsShell names an explicitly selected developer shell with both sockets.
// Layout is a closed producer contract, not a caller-defined placement recipe.
type PartsShell struct {
	PackageID string
	BuildID   string
	Payload   []byte
	Layout    string
}

type PartSelection struct {
	Role   string `json:"role"`
	PartID string `json:"part_id"`
}

// PartsComposition identifies all selected parts and the final output bytes,
// including the ROM when ComposePartsROM is used. BUILD_ID remains the shell's.
type PartsComposition struct {
	ID            string          `json:"composition_id"`
	PackageID     string          `json:"package_id"`
	Layout        string          `json:"layout"`
	Parts         []PartSelection `json:"parts"`
	ShellSHA256   string          `json:"shell_sha256"`
	PayloadSHA256 string          `json:"payload_sha256"`
	PayloadSize   int64           `json:"payload_size"`
}

func roleForPart(layout string, m Manifest) (string, socketPolicy, error) {
	video, ok := partsVideoPolicy(layout)
	if !ok {
		return "", socketPolicy{}, errors.New("unsupported parts layout")
	}
	switch {
	case supportedVideoPart(m) && m.Slot == video.slot && m.Map == video.mapping:
		return PartRoleVideo, video, nil
	case layout == AtariStVideoLayout && m.Slot == AtariStSlot && m.Map == AtariStMap && m.SlotMajor == 1 && m.SlotIndex == 1:
		return PartRoleExpansion, atariStSockets[1], nil
	case layout != AtariStVideoLayout && m.Slot == ColecoSlot && m.Map == ColecoMapV2 && m.SlotMajor == 2:
		return PartRoleExpansion, colecoSocketV2, nil
	default:
		return "", socketPolicy{}, errors.New("parts layout accepts only its matching video and CPU socket")
	}
}

// AdmitParts checks immutable asset identities and exact shell compatibility
// without decoding frames. Composition additionally verifies frame checksums,
// header preservation and each part's changes against the original shell.
func AdmitParts(shell PartsShell, assets []Asset) error {
	if _, ok := partsVideoPolicy(shell.Layout); !ok || !hex64.MatchString(shell.PackageID) || !hex32.MatchString(shell.BuildID) {
		return errors.New("unsupported parts shell layout or identity")
	}
	if len(assets) < 1 || len(assets) > 2 {
		return errors.New("parts composition requires one video part and an optional CPU expansion")
	}
	seen := map[string]bool{}
	shellHash := hash(shell.Payload)
	for _, asset := range assets {
		if err := asset.Validate(); err != nil {
			return err
		}
		m := asset.Manifest
		role, _, err := roleForPart(shell.Layout, m)
		if err != nil {
			return err
		}
		if seen[role] {
			return fmt.Errorf("parts composition has more than one %s part", role)
		}
		seen[role] = true
		if shell.PackageID != m.ShellPackageID || shell.BuildID != m.ShellBuildID || shellHash != m.ShellSHA256 {
			return fmt.Errorf("%s part was built for a different frozen shell", role)
		}
	}
	if !seen[PartRoleVideo] {
		return errors.New("parts composition requires a video part")
	}
	return nil
}

// PartsCompositionID uses sorted role names so argument order never changes
// identity. This separate domain leaves all existing composition IDs unchanged.
func PartsCompositionID(packageID, layout string, parts []PartSelection, payloadSHA256 string) (string, error) {
	_, knownLayout := partsVideoPolicy(layout)
	if !hex64.MatchString(packageID) || !knownLayout || !hex64.MatchString(payloadSHA256) ||
		len(parts) < 1 || len(parts) > 2 {
		return "", errors.New("invalid parts composition identity")
	}
	ordered := append([]PartSelection(nil), parts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Role < ordered[j].Role })
	material := "fes-parts-composition-v1\x00" + packageID + "\x00" + layout + "\x00"
	video := false
	for i, part := range ordered {
		if (part.Role != PartRoleExpansion && part.Role != PartRoleVideo) || !hex64.MatchString(part.PartID) ||
			(i > 0 && ordered[i-1].Role == part.Role) {
			return "", errors.New("invalid selected part role or identity")
		}
		video = video || part.Role == PartRoleVideo
		material += part.Role + ":" + part.PartID + "\x00"
	}
	if !video {
		return "", errors.New("parts composition identity requires video")
	}
	return hash([]byte(material + payloadSHA256)), nil
}

func identifyParts(shell PartsShell, assets []Asset, linked []byte) (PartsComposition, error) {
	result := PartsComposition{PackageID: shell.PackageID, Layout: shell.Layout, ShellSHA256: hash(shell.Payload),
		PayloadSHA256: hash(linked), PayloadSize: int64(len(linked))}
	for _, asset := range assets {
		role, _, _ := roleForPart(shell.Layout, asset.Manifest) // already checked by AdmitParts
		result.Parts = append(result.Parts, PartSelection{Role: role, PartID: asset.ID})
	}
	sort.Slice(result.Parts, func(i, j int) bool { return result.Parts[i].Role < result.Parts[j].Role })
	var err error
	result.ID, err = PartsCompositionID(result.PackageID, result.Layout, result.Parts, result.PayloadSHA256)
	return result, err
}

// ComposePartsContext overlays each selected part's disjoint closed region.
// Every input is compared with the original sealed shell, never a partial link.
func ComposePartsContext(ctx context.Context, shell PartsShell, assets []Asset) (PartsComposition, []byte, error) {
	if err := ctx.Err(); err != nil {
		return PartsComposition{}, nil, err
	}
	if err := AdmitParts(shell, assets); err != nil {
		return PartsComposition{}, nil, err
	}
	overlays := make([]regionOverlay, 0, len(assets))
	for _, asset := range assets {
		role, policy, _ := roleForPart(shell.Layout, asset.Manifest)
		overlays = append(overlays, regionOverlay{label: role, cart: asset.Cart, policy: policy})
	}
	linked, err := linkRegionOverlaysContext(ctx, shell.Payload, overlays)
	if err != nil {
		return PartsComposition{}, nil, fmt.Errorf("link parts: %w", err)
	}
	result, err := identifyParts(shell, assets, linked)
	return result, linked, err
}

// ValidatePartsROMDestinations excludes both physical sockets even when the
// shell uses its built-in Direct output and no independently linked parts.
func ValidatePartsROMDestinations(ctx context.Context, layout string, m ROMMap) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := partsVideoPolicy(layout); !ok {
		return errors.New("unsupported parts layout")
	}
	cpuPolicy := colecoSocketV2
	if layout == AtariStVideoLayout {
		cpuPolicy = atariStSockets[1]
	}
	videoPolicy, _ := partsVideoPolicy(layout) // layout was checked above
	for _, block := range m.Blocks {
		for _, bits := range block.WordBits {
			for _, destination := range bits {
				x, y := int(destination)%cramWidth, int(destination)/cramWidth
				if cpuPolicy.inside(x, y) || videoPolicy.inside(x, y) {
					return errors.New("ROM destination overlaps a reserved parts socket")
				}
			}
		}
	}
	return nil
}

// ComposePartsROM adds a trusted producer ROM map bound to the original base.
// Both reserved sockets are excluded even when no CPU expansion is selected.
// The returned identity describes programmed; overlay is the pre-ROM RBF.
func ComposePartsROM(ctx context.Context, shell PartsShell, assets []Asset, m ROMMap, rom []byte) (PartsComposition, []byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return PartsComposition{}, nil, nil, err
	}
	if err := AdmitParts(shell, assets); err != nil {
		return PartsComposition{}, nil, nil, err
	}
	if err := validateROMMap(ctx, m, hash(shell.Payload), len(rom)); err != nil {
		return PartsComposition{}, nil, nil, err
	}
	if err := ValidatePartsROMDestinations(ctx, shell.Layout, m); err != nil {
		return PartsComposition{}, nil, nil, err
	}
	_, overlay, err := ComposePartsContext(ctx, shell, assets)
	if err != nil {
		return PartsComposition{}, nil, nil, err
	}
	programmed, err := patchROM(ctx, overlay, m, rom)
	if err != nil {
		return PartsComposition{}, nil, nil, err
	}
	result, err := identifyParts(shell, assets, programmed)
	return result, overlay, programmed, err
}
