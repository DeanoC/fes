package fogcast

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

// MeshSkipCatalogUnavailable is the whole-library skip when the local
// catalog cannot be read. GET /api/v1/library/titles returns 500 for it
// and does not answer an empty title list.
const MeshSkipCatalogUnavailable = "local catalog unavailable"

// MeshSkipMalformedContentID is the per-title skip when a built slot id
// is not canonical sha256 text. A stored digest that FromSHA256 rejects,
// and a canonical sha256: value sitting in a stored-digest field, keep
// the slot-specific "digest is not a stored sha256" reason. The title
// stays on the skip list either way. ParseContentID does not coerce it.
const MeshSkipMalformedContentID = "content id is malformed"

// LibraryNodeMovedReason is the node reason when a configured hostname's
// fresh lookup no longer contains the IP this process pinned. No bearer
// is sent. A restart re-pins. The pin is not persisted (#396).
const LibraryNodeMovedReason = "node moved; re-pair or confirm the new address"

// MeshBackendRow groups the existing one-execute catalog entries by game id,
// with rows that share an exact ROM sha256 and system linked under one
// canonical id (see linkMeshTitles). Each option's Entry.TitleID stays the
// source catalog game id.
// This is a host projection, not a session selection. The wire view is
// MeshLibraryTitles, served at GET /api/v1/library/titles.
type MeshBackendRow struct {
	TitleID        string
	System         string
	ContentIDs     []meshcontent.ContentID
	ContentSources []MeshContentSource
	Options        []MeshBackendOption
}

// MeshContentSource is one slot content-id and the nodes that can supply
// it. NodeIDs stays empty until #396. This phase does not read content_ids
// off the node document and does not send a bearer to fill them.
type MeshContentSource struct {
	ContentID meshcontent.ContentID
	NodeIDs   []string
}

// MeshBackendOption keeps each composition distinct. A HostLocal option is
// usable on this host only when Reason is empty. Nodes are capability
// candidates; their presence never makes this option Ready for a session.
// CoreID is the catalog core id on a package-backed option (fes.sms).
type MeshBackendOption struct {
	Entry     meshcontent.Entry
	Nodes     []MeshBackendNode
	HostLocal bool
	Reason    string
	CoreID    string
}

// Available is the library wire flag. A host-local option is available
// when its reason is empty, including when a nested remote node is not.
// Any other option is available when one of its nodes is. This is not
// session Ready.
func (o MeshBackendOption) Available() bool {
	if o.HostLocal {
		return o.Reason == ""
	}
	for _, node := range o.Nodes {
		if node.Available {
			return true
		}
	}
	return false
}

type MeshBackendNode struct {
	NodeID    string
	Available bool
	Reason    string
}

// MeshBackendLibrary projects the existing local catalog and observed node
// inventory. GET /api/v1/library/titles is the wire view of this same
// projection. It does not discover titles, select a session backend, or
// report Ready. Content-source node ids stay empty until #396. A
// native_emu inventory node is not dialed: remote eligibility waits on
// #298 and the runner prerequisites, and this method sends no bearer.
// FPGA package facts come from a read of the configured [[targets]]
// address, cached on this path. A configured hostname is resolved once
// per process and later reads dial only that pinned IP. If a fresh
// lookup drops that IP, the node is unavailable with
// LibraryNodeMovedReason and no bearer is sent. A miss leaves the node
// unavailable. The read does not use a discovered or reconciled address.
func (s *Service) MeshBackendLibrary(ctx context.Context) ([]MeshBackendRow, []MeshSkip) {
	if s == nil || s.catalog == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	games, err := s.catalog.Games(ctx)
	if err != nil {
		return nil, []MeshSkip{{Reason: MeshSkipCatalogUnavailable}}
	}
	lib := MeshLibrary{}
	var skipped []MeshSkip
	for _, game := range games {
		if game.Kind == catalog.SourceKindCorePackage {
			title, firmware, ok := s.meshCatalogTitle(ctx, game.ID)
			if !ok {
				skipped = append(skipped, MeshSkip{TitleID: game.ID, Reason: "core package projection unavailable"})
				continue
			}
			if firmware.MediaID != "" {
				lib.Firmware = firmware
			}
			lib.Titles = append(lib.Titles, title)
			continue
		}
		execute, err := s.resolveExecution(ctx, game)
		if err != nil {
			skipped = append(skipped, MeshSkip{TitleID: game.ID, Reason: "local execution unavailable"})
			continue
		}
		lib.Titles = append(lib.Titles, MeshTitle{Game: game, Launchable: true, Execute: execute})
	}
	// Copy the rows and the retained flag together so one browse result
	// cannot pair with another's retained state.
	s.meshMu.Lock()
	nodes := append([]MeshNode(nil), s.meshNodes...)
	retained := s.meshNodesRetained
	s.meshMu.Unlock()
	packages := make(map[string][]string)
	abis := make(map[string][]meshcontent.EligibleABI)
	var blocked map[string]string
	for _, node := range nodes {
		fpga := false
		for _, execute := range node.Capabilities.Execute {
			fpga = fpga || execute.Kind == meshcontent.ExecuteFPGANative
		}
		// Package facts are read at the enrolled origin, or reused from
		// this path's cache. The dial never uses a discovered or
		// reconciled address. A hostname is dialed only at the IP this
		// process pinned. A native_emu node is not a kit content read.
		if fpga {
			var reason string
			abis[node.NodeID], packages[node.NodeID], reason = s.libraryNodeFacts(ctx, node.NodeID)
			if reason != "" {
				if blocked == nil {
					blocked = map[string]string{}
				}
				blocked[node.NodeID] = reason
			}
		}
	}
	rows, projectedSkipped := ProjectMeshBackendLibrary(lib, nodes, retained, packages, abis)
	applyLibraryNodeReasons(rows, blocked)
	return rows, append(skipped, projectedSkipped...)
}

// applyLibraryNodeReasons marks nodes a library read refused. The node
// stays unavailable with that reason even when an older package list
// would have passed. Host-local availability is not cleared.
func applyLibraryNodeReasons(rows []MeshBackendRow, reasons map[string]string) {
	if len(reasons) == 0 {
		return
	}
	for i := range rows {
		for j := range rows[i].Options {
			opt := &rows[i].Options[j]
			for k := range opt.Nodes {
				reason, ok := reasons[opt.Nodes[k].NodeID]
				if !ok {
					continue
				}
				opt.Nodes[k].Available = false
				opt.Nodes[k].Reason = reason
			}
			if opt.HostLocal {
				continue
			}
			available := false
			for _, node := range opt.Nodes {
				available = available || node.Available
			}
			if !available && opt.Reason == "" {
				opt.Reason = "no compatible executor in inventory"
			}
		}
	}
}

// ProjectMeshBackendLibrary uses the same catalog projection and observed
// inventory as launch and placement. An absent/retained or conflicting node
// cannot be selected. FPGA package evidence is supplied by the authenticated
// node document; discovery alone cannot assert that a package is installed.
// Remote emulator compatibility stays unverified until #298 and the runner
// admission and provisioning prerequisites. This function does not read
// software_backends and does not send a bearer.
func ProjectMeshBackendLibrary(lib MeshLibrary, nodes []MeshNode, retained bool, kitPackages map[string][]string, kitABIs map[string][]meshcontent.EligibleABI) (rows []MeshBackendRow, skipped []MeshSkip) {
	counts := map[string]int{}
	for _, node := range nodes {
		counts[node.NodeID]++
	}
	var projected []projectedMeshTitle
	for _, title := range lib.Titles {
		entry, skip, ok := projectMeshTitle(lib.Firmware.MediaID, title)
		if !ok {
			skipped = append(skipped, skip)
			continue
		}
		projected = append(projected, projectedMeshTitle{title: title, entry: entry})
	}
	canonical := linkMeshTitles(projected)
	projected = appendRemoteNativeEmuOptions(projected, nodes)
	byID := map[string]int{}
	for _, item := range projected {
		title, entry := item.title, item.entry
		hostLocal := title.Execute == ExecutionHostOnly
		rowID := canonical[entry.TitleID]
		index, found := byID[rowID]
		if !found {
			index = len(rows)
			byID[rowID] = index
			rows = append(rows, MeshBackendRow{TitleID: rowID, System: entry.System})
		} else if rows[index].System != entry.System {
			skipped = append(skipped, MeshSkip{TitleID: entry.TitleID, Reason: "title has conflicting browse systems"})
			continue
		}
		row := &rows[index]
		duplicate := false
		for _, old := range row.Options {
			if old.HostLocal == hostLocal && reflect.DeepEqual(old.Entry, entry) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		for _, id := range entry.ContentIDs() {
			present := false
			for _, old := range row.ContentIDs {
				if old == id {
					present = true
					break
				}
			}
			if !present {
				row.ContentIDs = append(row.ContentIDs, id)
			}
		}
		option := MeshBackendOption{Entry: entry, HostLocal: hostLocal}
		if title.Core != nil {
			if _, pkg := meshEntryPackage(entry); pkg {
				option.CoreID = title.Core.CoreID
			}
		}
		// Mirror the launch check: an unavailable or offline local source is
		// not a usable host-local option.
		if hostLocal && (title.Game.State != catalog.SourceStateAvailable || !title.Game.RootOnline) {
			option.Reason = "local source unavailable"
		}
		for _, node := range nodes {
			kind := ""
			if len(entry.Execute) == 1 {
				kind = entry.Execute[0].Kind
			}
			advertised := false
			for _, execute := range node.Capabilities.Execute {
				if execute.Kind == kind {
					advertised = true
				}
			}
			if !advertised {
				continue
			}
			candidate := MeshBackendNode{NodeID: node.NodeID}
			switch {
			case retained:
				candidate.Reason = "inventory retained after browse error"
			case counts[node.NodeID] != 1 || node.AddressConflict || node.Address == "" || node.NodeID == "" || node.NodeID != node.TargetID:
				candidate.Reason = "node identity or address is ambiguous"
			case !discovery.MeshMajorCompatible(node.Mesh):
				candidate.Reason = "mesh protocol major mismatch"
			case kind == meshcontent.ExecuteNativeEmu:
				// Pin, system, and software_backends are not consulted.
				// #298 and runner admission and provisioning are open, so
				// a weaker check must not mark the node available, and no
				// bearer is sent to learn them.
				candidate.Reason = "remote emulator system and version unverified"
			case kind == meshcontent.ExecuteFPGANative:
				pkg, ok := meshEntryPackage(entry)
				if !ok || !containsString(kitPackages[node.NodeID], pkg.PackageID) {
					candidate.Reason = "package unavailable on node"
				} else if !meshcontent.ABIMatches(pkg, kitABIs[node.NodeID]) {
					candidate.Reason = "package ABI incompatible on node"
				} else {
					candidate.Available = true
				}
			default:
				candidate.Reason = "executor unsupported"
			}
			option.Nodes = append(option.Nodes, candidate)
		}
		if !option.HostLocal {
			available := false
			for _, candidate := range option.Nodes {
				available = available || candidate.Available
			}
			switch {
			case len(option.Nodes) == 0:
				option.Reason = "no advertised executor in inventory"
			case !available:
				option.Reason = "no compatible executor in inventory"
			}
		}
		row.Options = append(row.Options, option)
	}
	for i := range rows {
		orderMeshBackendRow(&rows[i])
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TitleID < rows[j].TitleID })
	return rows, skipped
}

type projectedMeshTitle struct {
	title MeshTitle
	entry meshcontent.Entry
}

// linkMeshTitles maps each projected catalog game id to its library row id.
// Rows whose primary-media content id (the ROM sha256) and browse system both
// match exactly are one title. The link is derived at read time; catalog rows
// are not rewritten. The canonical id is a package-backed row's game id when
// one is present, otherwise the lowest game id; among several package rows the
// lowest game id wins. A different hash (for example a patched ROM) or a
// different system never links.
func linkMeshTitles(projected []projectedMeshTitle) map[string]string {
	type member struct {
		id  string
		pkg bool
	}
	groups := map[string][]member{}
	canonical := map[string]string{}
	for _, item := range projected {
		canonical[item.entry.TitleID] = item.entry.TitleID
		media, ok := meshEntryPrimaryMedia(item.entry)
		if !ok {
			continue
		}
		_, pkg := meshEntryPackage(item.entry)
		key := item.entry.System + "\x00" + media.String()
		groups[key] = append(groups[key], member{id: item.entry.TitleID, pkg: pkg})
	}
	for _, members := range groups {
		sort.Slice(members, func(i, j int) bool {
			if members[i].pkg != members[j].pkg {
				return members[i].pkg
			}
			return members[i].id < members[j].id
		})
		for _, m := range members {
			canonical[m.id] = members[0].id
		}
	}
	return canonical
}

// appendRemoteNativeEmuOptions adds the phase-2 remote option. Each
// host-local emulator title gains one synthetic copy with the same game,
// system, and primary-media digest and with Execute set to native_emu.
// The existing loop keeps it because host_local differs. The option is
// emitted only when inventory advertises native_emu, so a kit-only
// library stays the FPGA option plus the host-local option. Nodes on the
// synthetic option stay unavailable; this does not read a runner and does
// not send a bearer.
func appendRemoteNativeEmuOptions(projected []projectedMeshTitle, nodes []MeshNode) []projectedMeshTitle {
	if !inventoryAdvertises(nodes, meshcontent.ExecuteNativeEmu) {
		return projected
	}
	extra := make([]projectedMeshTitle, 0)
	for _, item := range projected {
		if item.title.Execute != ExecutionHostOnly {
			continue
		}
		if _, ok := meshEntryPrimaryMedia(item.entry); !ok {
			continue
		}
		if len(item.entry.Execute) != 1 || item.entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu {
			continue
		}
		synth := item
		synth.title.Execute = meshcontent.ExecuteNativeEmu
		extra = append(extra, synth)
	}
	if len(extra) == 0 {
		return projected
	}
	return append(projected, extra...)
}

func inventoryAdvertises(nodes []MeshNode, kind string) bool {
	for _, node := range nodes {
		for _, execute := range node.Capabilities.Execute {
			if execute.Kind == kind {
				return true
			}
		}
	}
	return false
}

// orderMeshBackendRow makes a row independent of catalog input order:
// package-backed options first, then by source game id, host-local before
// sourced; content ids follow that option order.
func orderMeshBackendRow(row *MeshBackendRow) {
	sort.SliceStable(row.Options, func(i, j int) bool {
		a, b := row.Options[i], row.Options[j]
		_, ap := meshEntryPackage(a.Entry)
		_, bp := meshEntryPackage(b.Entry)
		if ap != bp {
			return ap
		}
		if a.Entry.TitleID != b.Entry.TitleID {
			return a.Entry.TitleID < b.Entry.TitleID
		}
		return a.HostLocal && !b.HostLocal
	})
	row.ContentIDs = row.ContentIDs[:0]
	for _, option := range row.Options {
		for _, id := range option.Entry.ContentIDs() {
			present := false
			for _, old := range row.ContentIDs {
				if old == id {
					present = true
					break
				}
			}
			if !present {
				row.ContentIDs = append(row.ContentIDs, id)
			}
		}
	}
	// One source row per slot id, inventory order, deduped. Node ids stay
	// empty until #396. Filling them would require reading the node document.
	row.ContentSources = make([]MeshContentSource, 0, len(row.ContentIDs))
	for _, id := range row.ContentIDs {
		row.ContentSources = append(row.ContentSources, MeshContentSource{ContentID: id, NodeIDs: []string{}})
	}
}

func meshEntryPrimaryMedia(entry meshcontent.Entry) (meshcontent.ContentID, bool) {
	for _, slot := range entry.Slots {
		if slot.Kind == meshcontent.SlotPrimaryMedia && slot.Content != nil {
			return *slot.Content, true
		}
	}
	return meshcontent.ContentID{}, false
}

func meshEntryPackage(entry meshcontent.Entry) (meshcontent.PackageABI, bool) {
	for _, slot := range entry.Slots {
		if slot.Kind == meshcontent.SlotPackageABI && slot.Package != nil {
			return *slot.Package, true
		}
	}
	return meshcontent.PackageABI{}, false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// MeshLibrary is the host catalog view Slice 2 can already see.
// Firmware.MediaID is the household BIOS core-media id when that slot
// is filled. Digests are stored SHA-256 strings. This view has no paths
// and no file bytes.
type MeshLibrary struct {
	Firmware catalog.CoreFirmware
	Titles   []MeshTitle
}

// MeshTitle is one library row. Game is the catalog identity. Core is
// set for a package-backed title. ABI is that package's described
// contract; ABI.Major is the package ABI major, not the mesh protocol
// major. Execute is today's session label (fpga_native or host_only).
// Expansions carry the name and digest the catalog already stored.
type MeshTitle struct {
	Game       catalog.Game
	Launchable bool
	Execute    string
	Core       *catalog.CoreEntry
	ABI        corepackage.Contract
	Expansions []MeshExpansion
}

// MeshExpansion is one named expansion slot.
// Digest is the slot-bytes digest: SHA-256 of that slot's own bytes,
// the cart payload (expansion.Manifest.CartSHA256). It is not
// expansion.Asset.ID, not the archive media_id of the stored tar, and
// not a post-link ProgrammedSHA256. Ensure asks the bound executor to
// link these bytes. The host does not pre-link them into primary media.
type MeshExpansion struct {
	Name   string
	Digest string
}

// MeshSkip is why one title was left out of the projection. The title
// is not replaced with a guessed entry.
type MeshSkip struct {
	TitleID string
	Reason  string
}

// ProjectMeshLibrary fills mesh catalog entries from the host library.
// Stored digests go through meshcontent.FromSHA256. Primary media uses
// PrimarySourceID (the format-3 source MediaID / SourceSHA256, never
// ProgrammedSHA256). Each expansion uses ExpansionSlotBytesID
// (MeshExpansion.Digest, the slot-bytes digest). The function does
// not open files and does not hash bytes. A title that cannot be named
// is omitted; the skip result carries the reason. This projection does
// not call ReadyHere. There is no cross-node pull and no host route.
func ProjectMeshLibrary(lib MeshLibrary) (entries []meshcontent.Entry, skipped []MeshSkip) {
	for _, title := range lib.Titles {
		entry, skip, ok := projectMeshTitle(lib.Firmware.MediaID, title)
		if !ok {
			skipped = append(skipped, skip)
			continue
		}
		entries = append(entries, entry)
	}
	return entries, skipped
}

func projectMeshTitle(firmwareDigest string, title MeshTitle) (meshcontent.Entry, MeshSkip, bool) {
	id := title.Game.ID
	if err := protocol.ValidateGameID(id); err != nil {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: "title id is not a catalog game id"}, false
	}
	if title.Core != nil && title.Core.GameID != "" && title.Core.GameID != id {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: "core entry is for a different title"}, false
	}
	kind, packageBacked, ok := meshExecuteKind(title.Execute, title.Launchable)
	if !ok {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: "execution is not a mesh catalog kind"}, false
	}
	system := meshBrowseSystem(title)
	slots, reason, ok := meshSlots(firmwareDigest, title, packageBacked)
	if !ok {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: reason}, false
	}
	entry := meshcontent.Entry{
		TitleID:    id,
		System:     system,
		Slots:      slots,
		Launchable: title.Launchable,
	}
	if kind != "" {
		entry.Execute = []meshcontent.Execute{{Kind: kind}}
	}
	if err := entry.Validate(); err != nil {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: err.Error()}, false
	}
	return entry, MeshSkip{}, true
}

func meshExecuteKind(execute string, launchable bool) (kind string, packageBacked bool, ok bool) {
	switch strings.TrimSpace(execute) {
	case ExecutionFPGANative:
		return meshcontent.ExecuteFPGANative, true, true
	case ExecutionHostOnly, meshcontent.ExecuteNativeEmu:
		return meshcontent.ExecuteNativeEmu, false, true
	case "":
		if launchable {
			return "", false, false
		}
		return "", false, true
	default:
		return "", false, false
	}
}

// meshBrowseSystem uses the machine name the closed package cores already
// have. Catalog rows for those cores are stored on the fpga platform.
// A described Core.System on the title's ABI is not available here; the
// core id is the name the library stores.
func meshBrowseSystem(title MeshTitle) string {
	if title.Core != nil {
		switch title.Core.CoreID {
		case "fes.coleco", "fes.zx81", "fes.pong", "fes.sms", "fes.sg1000":
			return strings.TrimPrefix(title.Core.CoreID, "fes.")
		}
	}
	return string(title.Game.System)
}

func meshSlots(firmwareDigest string, title MeshTitle, packageBacked bool) ([]meshcontent.Slot, string, bool) {
	var slots []meshcontent.Slot
	if packageBacked {
		pkg, reason, ok := meshPackage(title)
		if !ok {
			return nil, reason, false
		}
		slots = append(slots, meshcontent.PackageSlot(pkg))
	}
	firmwareRequired := title.Core != nil && title.Core.FirmwareRequired
	if firmwareRequired {
		next, reason, ok := appendStoredDigest(slots, firmwareDigest, true, "household firmware", meshcontent.FromSHA256, meshcontent.BIOSSlot)
		if !ok {
			return nil, reason, false
		}
		slots = next
	}
	primaryRequired := !packageBacked && title.Launchable
	next, reason, ok := appendStoredDigest(slots, storedPrimaryDigest(title), primaryRequired, "primary media", PrimarySourceID, meshcontent.PrimaryMediaSlot)
	if !ok {
		return nil, reason, false
	}
	slots = next
	for _, expansion := range title.Expansions {
		if strings.TrimSpace(expansion.Name) == "" {
			return nil, "expansion name is missing", false
		}
		var built []meshcontent.Slot
		built, reason, ok = appendStoredDigest(nil, expansion.Digest, true, "expansion "+expansion.Name, ExpansionSlotBytesID, func(id meshcontent.ContentID) meshcontent.Slot {
			return meshcontent.ExpansionSlot(expansion.Name, id)
		})
		if !ok {
			return nil, reason, false
		}
		slots = append(slots, built...)
	}
	return slots, "", true
}

// PrimarySourceID names primary media by the format-3 source digest.
// That digest is the catalog MediaID, which the executor records as
// ROMLink.SourceSHA256. ProgrammedSHA256 is the post-link image and
// is not this id.
func PrimarySourceID(sourceSHA256 string) (meshcontent.ContentID, error) {
	return meshcontent.FromSHA256(sourceSHA256)
}

// ExpansionSlotBytesID names an expansion by MeshExpansion.Digest, the
// slot-bytes digest (SHA-256 of the cart payload). Asset.ID, the
// archive media_id, and ProgrammedSHA256 are not this id. The host
// does not link the bytes; the bound executor does.
func ExpansionSlotBytesID(slotBytesDigest string) (meshcontent.ContentID, error) {
	return meshcontent.FromSHA256(slotBytesDigest)
}

func meshPackage(title MeshTitle) (meshcontent.PackageABI, string, bool) {
	if title.Core == nil || title.ABI.Major < 1 || title.ABI.Major > 65535 {
		return meshcontent.PackageABI{}, "package abi is required", false
	}
	pkg := meshcontent.PackageABI{
		PackageID: title.Core.PackageID,
		ABI:       title.ABI.ID,
		Major:     int(title.ABI.Major),
	}
	if err := pkg.Validate(); err != nil {
		return meshcontent.PackageABI{}, "package abi is required", false
	}
	return pkg, "", true
}

// storedPrimaryDigest is the format-3 source identity. Core.MediaID is
// the source SHA-256 (ROMLink.SourceSHA256 on the executor). A native
// title uses the catalog content SHA-256 in that same role. Neither
// value is ROMLink.ProgrammedSHA256.
func storedPrimaryDigest(title MeshTitle) string {
	if title.Core != nil && title.Core.MediaID != "" {
		return title.Core.MediaID
	}
	if title.Game.Content != nil {
		return title.Game.Content.SHA256
	}
	return ""
}

func appendStoredDigest(slots []meshcontent.Slot, digest string, required bool, slot string, parse func(string) (meshcontent.ContentID, error), build func(meshcontent.ContentID) meshcontent.Slot) ([]meshcontent.Slot, string, bool) {
	if digest == "" {
		if required {
			return nil, slot + " digest is required", false
		}
		return slots, "", true
	}
	// Stored fields are bare digests. ParseContentID accepts only the
	// canonical wire text and does not trim, downcase, or add a prefix.
	// A wire id in this field is not coerced into a slot. A bare digest
	// FromSHA256 cannot build, and a built id ParseContentID rejects,
	// are skips. They are not dropped.
	if _, err := meshcontent.ParseContentID(digest); err == nil {
		return nil, slot + " digest is not a stored sha256", false
	}
	id, err := parse(digest)
	if err != nil {
		return nil, slot + " digest is not a stored sha256", false
	}
	locked, err := meshcontent.ParseContentID(id.String())
	if err != nil || locked != id {
		return nil, slot + " " + MeshSkipMalformedContentID, false
	}
	return append(slots, build(locked)), "", true
}
