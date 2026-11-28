package misterruntime

import (
	"errors"
	"sort"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func validProtocol2Response(response Protocol2Response) bool {
	if response.RuntimeInstance != nil && (!protocol2Instance.MatchString(*response.RuntimeInstance) || *response.RuntimeInstance == "00000000000000000000000000000000") {
		return false
	}
	if response.Capabilities.MediaStream != nil {
		if response.ActivePackage == nil || !positive(response.Generation) {
			return false
		}
		activation := activationFromProtocol2(response.ActivePackage.PackageID, response.ActivePackage.Descriptor, response)
		if !protocol.MediaStreamCapable(corePackageStatus(activation)) {
			return false
		}
		declared := protocol.DeclaredCoreMediaCapabilities(response.ActivePackage.Descriptor)
		if len(declared) != 1 || declared[0].Interface != protocol.MediaStreamInterface() {
			return false
		}
	}
	if len(response.Capabilities.MediaUnits) != 0 && !validMediaUnits(response) {
		return false
	}
	if response.CoreData != nil && !response.CoreData.Valid() {
		return false
	}
	if response.Protocol != 2 || response.Version == "" ||
		(response.Error == nil && !response.OK) ||
		(response.Error != nil && !validProtocol2Error(*response.Error)) ||
		!validCapabilities(response.Capabilities) ||
		(response.InspectedPackage != nil && !response.OK) {
		return false
	}
	identity := protocol2Identity(response.System, response.Core)
	noActive := response.ActivePackage == nil && len(response.Capabilities.ActiveInterfaces) == 0
	switch response.State {
	case "idle":
		if response.Execution != "none" || identity != identityNone || response.Generation != nil || !noActive {
			return false
		}
	case "starting":
		if response.Generation != nil || !noActive {
			return false
		}
		switch response.Execution {
		case "none":
			if identity != identityNone {
				return false
			}
		case "development":
			if identity != identityNone {
				return false
			}
		default:
			return false
		}
	case "running_development":
		if response.Execution != "development" || (identity != identityNone && identity != identityCore) || !positive(response.Generation) {
			return false
		}
		if response.ActivePackage == nil {
			if identity != identityNone || len(response.Capabilities.ActiveInterfaces) != 0 {
				return false
			}
		} else if !validActivePackage(*response.ActivePackage, response.Capabilities) {
			return false
		}
	case "reboot_required":
		if response.Error == nil || response.Error.Code != "idle_failed" {
			return false
		}
		if response.ActivePackage != nil {
			if response.Execution != "development" || identity != identityCore || !positive(response.Generation) || response.ActivePackage.PersistenceMode != "persistent" || response.Error.Phase != "recovery" || !validActivePackage(*response.ActivePackage, response.Capabilities) {
				return false
			}
		} else if response.Execution != "none" || identity != identityNone || response.Generation != nil || !noActive {
			return false
		}
	default:
		return false
	}
	if response.InspectedPackage != nil && !validInspection(*response.InspectedPackage, response.Capabilities) {
		return false
	}
	return true
}

func validProtocol2Error(remote Protocol2Error) bool {
	switch remote.Code {
	case "invalid_request", "unsupported_protocol", "unknown_system", "missing_media", "busy", "program_failed", "core_mismatch", "io_failed", "idle_failed", "save_failed", "invalid_package", "unsupported_target", "unsupported_programming_profile", "unsupported_abi", "unsupported_interface", "corrupt_data", "incompatible_data", "stale_revision":
	default:
		return false
	}
	switch remote.Phase {
	case "core_data", "media_data", "request", "admission", "compatibility", "save", "quiesce", "programming", "transport", "identity", "video", "input", "recovery", "lifecycle", "menu", "menu_window":
		return true
	default:
		return false
	}
}

func validCapabilities(capabilities Protocol2Capabilities) bool {
	// One ABI may be advertised at several minors (fes.computer 1.0 and 1.1),
	// each with its own interfaces; a package matches exactly one.
	if !sortedUniqueStrings(capabilities.ProgrammingProfiles) ||
		!sort.SliceIsSorted(capabilities.ABIs, func(i, j int) bool {
			a, b := capabilities.ABIs[i], capabilities.ABIs[j]
			if a.ID != b.ID {
				return a.ID < b.ID
			}
			if a.Major != b.Major {
				return a.Major < b.Major
			}
			return a.Minor < b.Minor
		}) {
		return false
	}
	type abiVersion struct {
		id           string
		major, minor uint16
	}
	seen := map[abiVersion]bool{}
	for _, abi := range capabilities.ABIs {
		version := abiVersion{id: abi.ID, major: abi.Major, minor: abi.Minor}
		if seen[version] || !validContract(abi.ID, abi.Major, abi.Minor) || !validSupportedInterfaces(abi.Interfaces) {
			return false
		}
		seen[version] = true
	}
	return validSupportedInterfaces(capabilities.ActiveInterfaces)
}

func validSupportedInterfaces(interfaces []Protocol2Interface) bool {
	if !sort.SliceIsSorted(interfaces, func(i, j int) bool { return interfaces[i].ID < interfaces[j].ID }) {
		return false
	}
	seen := map[string]bool{}
	for _, value := range interfaces {
		if seen[value.ID] || !validContract(value.ID, value.Major, value.Minor) {
			return false
		}
		seen[value.ID] = true
	}
	return true
}

// validMediaUnits admits live unit reports only for an active fes.computer
// generation. Units are ascending and unique, and each interface is active.
func validMediaUnits(response Protocol2Response) bool {
	active := response.ActivePackage
	if active == nil || !positive(response.Generation) ||
		!protocol.ComputerABI(active.Descriptor.ABI.ID, active.Descriptor.ABI.Major, active.Descriptor.ABI.Minor) {
		return false
	}
	packageStatus := computerMediaStatus(response).CorePackage
	for index, unit := range response.Capabilities.MediaUnits {
		if !unit.Valid() || (unit.Interface == protocol.AtaDiskInterface() &&
			!protocol.ComputerAtaDiskABI(active.Descriptor.ABI.ID, active.Descriptor.ABI.Major, active.Descriptor.ABI.Minor)) ||
			(index > 0 && response.Capabilities.MediaUnits[index-1].Unit >= unit.Unit) {
			return false
		}
		if unit.Interface == protocol.Zx81TapeInterface() && int64(unit.MaxBytes) != protocol.Zx81TapeMaxBytesFor(active.Descriptor) {
			return false
		}
		if unit.Interface == protocol.PcxtFloppyInterface() && active.Descriptor.ABI.Minor < protocol.ComputerExtendedMinor {
			return false
		}
		found := false
		for _, contract := range response.Capabilities.ActiveInterfaces {
			if contract.ID == unit.Interface.ID && contract.Major == unit.Interface.Major && contract.Minor == unit.Interface.Minor {
				found = true
			}
		}
		if !found {
			return false
		}
		if unit.Interface == protocol.AtariStFloppyInterface() && int64(unit.MaxBytes) != protocol.AtariStFloppyBytes {
			if !corepackage.DeclaresAtariStGeometry(active.Descriptor) || !protocol.AtariStFloppyGeometryCapable(computerMediaStatus(response).CorePackage) {
				return false
			}
		}
		if unit.Persistence != nil && (active.PersistenceMode != "persistent" || !protocol.PersistentMediaUnitCapable(packageStatus, unit)) {
			return false
		}
	}
	if active.PersistenceMode == "persistent" &&
		(!protocol.PersistentMediaStatusValid(packageStatus) || !descriptorPersistentMedia(active.Descriptor, response.Capabilities.MediaUnits)) {
		return false
	}
	return true
}

// validSlotComposition binds a v2 tuple to an exact supported fes.computer shell
// declaring one optional slot bus; every slot is a physical socket.
func validSlotComposition(c expansion.SlotComposition, active Protocol2ActivePackage) bool {
	id, err := expansion.SlotCompositionID(c.PackageID, c.Expansions, c.PayloadSHA256)
	if err != nil || id != c.ID || c.PackageID != active.PackageID || !protocol2Hex64.MatchString(c.ShellSHA256) ||
		c.ShellSHA256 != active.Descriptor.Payload.SHA256 || c.PayloadSize < 40408 || c.PayloadSize > corepackage.MaxPayloadSize ||
		(active.PersistenceMode == "persistent" && !descriptorMediaWrite(active.Descriptor)) || !protocol.ComputerMediaABI(active.Descriptor.ABI.ID, active.Descriptor.ABI.Major, active.Descriptor.ABI.Minor) {
		return false
	}
	var bus string
	for _, i := range active.Descriptor.Interfaces {
		switch i.ID {
		case expansion.Apple2Slot, expansion.C64Slot, expansion.SpectrumSlot, expansion.AtariStSlot:
			if bus != "" || i.Major != 1 || i.Minor != 0 || i.Required {
				return false
			}
			bus = i.ID
		case expansion.Slot, expansion.ColecoSlot:
			return false
		}
	}
	mapping, ok := expansion.SlotMap(bus, 1)
	if !ok {
		return false
	}
	sockets := map[int]bool{}
	for _, socket := range expansion.SlotSockets(bus, mapping) {
		sockets[socket] = true
	}
	for _, e := range c.Expansions {
		if !sockets[e.Slot] {
			return false
		}
	}
	return true
}

func validActivePackage(active Protocol2ActivePackage, capabilities Protocol2Capabilities) bool {
	if (active.Descriptor.Format == 5) != (active.StaticComposition != nil) {
		return false
	}
	if c := active.StaticComposition; c != nil {
		if active.Composition != nil || active.SlotComposition != nil || active.PartsComposition != nil || active.ROMLink != nil || active.ROMLinks != nil || active.PersistenceMode != "volatile" || !validStaticComposition(*c, active.PackageID, &active.Descriptor) {
			return false
		}
	}
	if protocol.ComputerABI(active.Descriptor.ABI.ID, active.Descriptor.ABI.Major, active.Descriptor.ABI.Minor) && active.PersistenceMode == "persistent" {
		status := protocol2MediaStatus(active, capabilities)
		if !protocol.PersistentMediaStatusValid(status) || !descriptorPersistentMedia(active.Descriptor, capabilities.MediaUnits) {
			return false
		}
	}
	for _, i := range active.Descriptor.Interfaces {
		if i.ID == expansion.NativeVideoSlot && active.PartsComposition == nil {
			return false
		}
	}
	if c := active.PartsComposition; c != nil {
		if active.Composition != nil || active.SlotComposition != nil || (active.PersistenceMode != "volatile" && !descriptorMediaWrite(active.Descriptor)) || !validPartsComposition(*c, active.PackageID) || c.ShellSHA256 != active.Descriptor.Payload.SHA256 {
			return false
		}
		if shell, err := corepackage.PartsShell(corepackage.Inspection{PackageID: active.PackageID, Descriptor: active.Descriptor}, nil); err != nil || shell.Layout != c.Layout {
			return false
		}
	}
	if c := active.SlotComposition; c != nil && (active.Composition != nil || !validSlotComposition(*c, active)) {
		return false
	}
	if active.Descriptor.Format == 4 {
		if capabilities.ROMLinking != 1 || active.ROMLinks == nil || !active.ROMLinks.ValidFor(active.Descriptor) || active.ROMLink != nil {
			return false
		}
	} else if active.ROMLinks != nil {
		return false
	}
	if active.Descriptor.ROM != nil {
		if !romDeliverySupported(capabilities, active.Descriptor) || active.ROMLink == nil || !active.ROMLink.ValidFor(active.Descriptor) {
			return false
		}
	} else if active.ROMLink != nil {
		return false
	}
	if c := active.Composition; c != nil {
		if !validComposition(*c, active.PackageID) || c.ShellSHA256 != active.Descriptor.Payload.SHA256 || active.PersistenceMode == "persistent" {
			return false
		}
		var socketID string
		switch active.Descriptor.ABI.ID {
		case "fes.simple-computer":
			if active.Descriptor.ABI.Major != 1 || active.Descriptor.ABI.Minor != 0 {
				return false
			}
			socketID = "fes.expansion.zx81-bus"
		case "fes.computer":
			if !protocol.ComputerMediaABI(active.Descriptor.ABI.ID, active.Descriptor.ABI.Major, active.Descriptor.ABI.Minor) {
				return false
			}
			socketID = "fes.expansion.zx81-bus"
		case "fes.application":
			if active.Descriptor.ABI.Major != 1 || active.Descriptor.ABI.Minor != 0 {
				return false
			}
			socketID = "fes.expansion.coleco-bus"
		default:
			return false
		}
		socket := false
		for _, i := range active.Descriptor.Interfaces {
			if (i.ID == "fes.expansion.zx81-bus" || i.ID == "fes.expansion.coleco-bus") && i.ID != socketID {
				return false
			}
			if i.ID == socketID && i.Minor == 0 && !i.Required &&
				(i.Major == 1 || i.Major == 2) {
				socket = true
			}
		}
		if !socket {
			return false
		}
	}
	if !protocol2Hex64.MatchString(active.PackageID) || !validDescriptor(active.Descriptor) {
		return false
	}
	abi := matchingABI(capabilities, active.Descriptor)
	if abi == nil || !containsString(capabilities.ProgrammingProfiles,
		active.Descriptor.Target.ProgrammingProfile) ||
		!requiredInterfacesSupported(active.Descriptor, *abi) ||
		!equalInterfaces(capabilities.ActiveInterfaces,
			interfaceIntersection(active.Descriptor, *abi)) {
		return false
	}
	if (active.Observed.ABI == nil) != (active.Observed.BuildID == nil) {
		return false
	}
	switch active.Descriptor.Target.ProgrammingProfile {
	case "fes-gp-v1":
		if active.Observed.ABI == nil {
			return false
		}
	case "de25-hps-first-v1", "de25-linux-menu-v1", "de25-linux-computer-v1", "de25-linux-static-computer-v1":
		// The same exact per-machine contract package admission enforces.
		wantABI := corepackage.DE25ComputerABI(active.Descriptor.Core.ID)
		if active.Descriptor.Target.ProgrammingProfile == "de25-linux-menu-v1" {
			wantABI = corepackage.Contract{ID: "fes.application", Major: 1, Minor: 0}
		}
		if active.Observed.ABI == nil || active.Descriptor.Target.Platform != "de25_nano" ||
			active.Descriptor.ABI != wantABI {
			return false
		}
	default:
		return false
	}
	if active.Observed.ABI == nil {
		return true
	}
	return active.Observed.ABI.ID == active.Descriptor.ABI.ID &&
		int64(active.Observed.ABI.Major) == active.Descriptor.ABI.Major &&
		int64(active.Observed.ABI.Minor) == active.Descriptor.ABI.Minor &&
		*active.Observed.BuildID == active.Descriptor.Build.ID
}

func validInspection(inspection Protocol2Inspection, capabilities Protocol2Capabilities) bool {
	if layout := inspection.PersistenceLayout; layout != nil {
		found := false
		for _, i := range inspection.Descriptor.Interfaces {
			if i.Required && i.ID == layout.ID && i.Major == int64(layout.Major) && i.Minor == int64(layout.Minor) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if !protocol2Hex64.MatchString(inspection.PackageID) || !validDescriptor(inspection.Descriptor) {
		return false
	}
	if inspection.Compatible {
		if (inspection.Descriptor.Format == 4 && capabilities.ROMLinking != 1) ||
			(inspection.Descriptor.ROM != nil && !romDeliverySupported(capabilities, inspection.Descriptor)) {
			return false
		}
		abi := matchingABI(capabilities, inspection.Descriptor)
		return inspection.CompatibilityError == nil && abi != nil &&
			containsString(capabilities.ProgrammingProfiles,
				inspection.Descriptor.Target.ProgrammingProfile) &&
			requiredInterfacesSupported(inspection.Descriptor, *abi)
	}
	return inspection.CompatibilityError != nil && validProtocol2Error(*inspection.CompatibilityError)
}

func matchingABI(capabilities Protocol2Capabilities, descriptor corepackage.Descriptor) *Protocol2ABI {
	for index := range capabilities.ABIs {
		candidate := &capabilities.ABIs[index]
		if descriptor.ABI.ID == protocol.ComputerABIID {
			if protocol.ComputerMediaABI(descriptor.ABI.ID, descriptor.ABI.Major, descriptor.ABI.Minor) &&
				protocol.ComputerMediaABI(candidate.ID, int64(candidate.Major), int64(candidate.Minor)) &&
				int64(candidate.Minor) >= descriptor.ABI.Minor {
				return candidate
			}
			continue
		}
		if candidate.ID == descriptor.ABI.ID && int64(candidate.Major) == descriptor.ABI.Major &&
			int64(candidate.Minor) == descriptor.ABI.Minor {
			return candidate
		}
	}
	return nil
}

func requiredInterfacesSupported(descriptor corepackage.Descriptor, abi Protocol2ABI) bool {
	for _, declared := range descriptor.Interfaces {
		if !declared.Required {
			continue
		}
		found := false
		for _, supported := range abi.Interfaces {
			if supported.ID == declared.ID && int64(supported.Major) == declared.Major &&
				int64(supported.Minor) == declared.Minor {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func interfaceIntersection(descriptor corepackage.Descriptor, abi Protocol2ABI) []Protocol2Interface {
	var result []Protocol2Interface
	for _, declared := range descriptor.Interfaces {
		for _, supported := range abi.Interfaces {
			if supported.ID == declared.ID && int64(supported.Major) == declared.Major &&
				int64(supported.Minor) == declared.Minor {
				result = append(result, supported)
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func equalInterfaces(left, right []Protocol2Interface) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func validDescriptor(descriptor corepackage.Descriptor) bool {
	return corepackage.ValidateDescriptor(descriptor) == nil
}

func validContract(id string, major, minor uint16) bool {
	return protocol2Identifier.MatchString(id) && major > 0
}

func sortedUniqueStrings(values []string) bool {
	if !sort.StringsAreSorted(values) {
		return false
	}
	for index, value := range values {
		if !protocol2Identifier.MatchString(value) || (index > 0 && values[index-1] == value) {
			return false
		}
	}
	return true
}

func positive(value *uint64) bool { return value != nil && *value != 0 }

func protocol2MutationAttempted(err error) bool {
	var attempted protocol2MutationError
	if errors.As(err, &attempted) {
		return attempted.attempted
	}
	// Alternate controls cannot prove that a transport error happened before
	// dispatch, so their ordinary errors remain sent-or-ambiguous.
	return true
}

func protocol2Identity(system, core *string) identityShape {
	if system == nil && core == nil {
		return identityNone
	}
	if system == nil && core != nil && *core != "" {
		return identityCore
	}
	if system == nil || core == nil || *system == "" || *core == "" {
		return identityInvalid
	}
	return identityPair
}

func validComposition(c expansion.Composition, packageID string) bool {
	id, err := expansion.CompositionID(c.PackageID, c.ExpansionID, c.PayloadSHA256)
	return err == nil && id == c.ID && c.PackageID == packageID && protocol2Hex64.MatchString(c.ShellSHA256) && c.PayloadSize >= 40408 && c.PayloadSize <= corepackage.MaxPayloadSize
}

type identityShape uint8

const (
	identityNone identityShape = iota
	identityPair
	identityCore
	identityInvalid
)

func descriptorMediaWrite(d corepackage.Descriptor) bool {
	return protocol.DescriptorMediaWrite(d, protocol.AtariStFloppyInterface()) || protocol.DescriptorMediaWrite(d, protocol.PcxtFloppyInterface())
}

func descriptorPersistentMedia(d corepackage.Descriptor, units []protocol.MediaUnitStatus) bool {
	// Every bound unit must be one the descriptor makes durable: pc386 can
	// bind its hard disk and both floppy drives at once.
	bound := false
	for _, unit := range units {
		if unit.Persistence == nil {
			continue
		}
		bound = true
		switch unit.Interface {
		case protocol.AtariStFloppyInterface(), protocol.PcxtFloppyInterface():
			if !protocol.DescriptorMediaWrite(d, unit.Interface) {
				return false
			}
		case protocol.AtaDiskInterface():
			if !protocol.DeclaresAtaDisk(d) {
				return false
			}
		case protocol.PcFloppyInterface():
			if !protocol.DeclaresPcFloppy(d) {
				return false
			}
		default:
			return false
		}
	}
	if bound {
		return true
	}
	for _, unit := range units {
		if unit.Interface == protocol.AtaDiskInterface() {
			return unit.Persistence == nil && (unit.State == protocol.MediaUnitEmpty || unit.State == protocol.MediaUnitLoading) &&
				protocol.DeclaresAtaDisk(d)
		}
	}
	if protocol.DeclaresPcxtMachine(d) && !protocol.DeclaresAtaDisk(d) {
		if !protocol.DeclaresPcxtFloppy(d) {
			return true
		}
		for _, unit := range units {
			if unit.Unit == protocol.PcxtFloppyUnit && unit.Interface == protocol.PcxtFloppyInterface() && unit.Persistence == nil {
				return true
			}
		}
		return false
	}
	// With no disk, a pc386 machine record alone is persistent.
	return protocol.DeclaresPc386Machine(d) && !protocol.DeclaresAtaDisk(d)
}

func protocol2MediaStatus(active Protocol2ActivePackage, capabilities Protocol2Capabilities) *protocol.CorePackageStatus {
	interfaces := make([]protocol.RuntimeInterface, len(capabilities.ActiveInterfaces))
	for index, value := range capabilities.ActiveInterfaces {
		interfaces[index] = protocol.RuntimeInterface{ID: value.ID, Major: value.Major, Minor: value.Minor}
	}
	return &protocol.CorePackageStatus{
		ABI:              protocol.RuntimeContract{ID: active.Descriptor.ABI.ID, Major: uint16(active.Descriptor.ABI.Major), Minor: uint16(active.Descriptor.ABI.Minor)},
		ActiveInterfaces: interfaces, MediaUnits: capabilities.MediaUnits, PersistenceMode: active.PersistenceMode,
	}
}
