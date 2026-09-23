package discovery

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// MeshProtocol is the major.minor this build advertises. A different
	// major fails closed for a session that needs the mesh contract. Minor
	// additions may add optional TXT fields. Phase 0 peers omit mesh and
	// stay directly bindable.
	MeshProtocol      = "1.0"
	meshProtocolMajor = 1

	nodeIDKey = "node_id"
	meshKey   = "mesh"
	capKey    = "cap"
	ttlKey    = "ttl"

	// ExecuteFPGANative is the kit's execute kind. It is not a claim that
	// any RBF runs, and it does not name a display title or a file path.
	ExecuteFPGANative = "fpga_native"

	capDisplaySink = "display_sink"
	capInputSource = "input_source"
	capExecute     = "execute"
)

// MeshVersion is a mesh-protocol major.minor pair.
type MeshVersion struct {
	Major int
	Minor int
}

func (v MeshVersion) String() string {
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor)
}

// ABI is one package family an Execute kind can run, when the node knows it.
type ABI struct {
	ID    string
	Major int
}

// Execute is one advertised execute kind and, when known, its ABI families.
type Execute struct {
	Kind string
	ABIs []ABI
}

// Capabilities is the node's advertised bag. Absent entries are not claims.
// DisplaySink means the node can present. It does not mean the picture is up.
type Capabilities struct {
	Execute     []Execute
	DisplaySink bool
	InputSource bool
}

// Advertisement is a parsed DNS-SD TXT record. Phase 0 records omit mesh
// fields. Unknown keys are ignored. Credentials, titles, and lease secrets
// are not fields of this value.
type Advertisement struct {
	TargetID             string
	NodeID               string
	DiscoveryProtocol    string
	Mesh                 *MeshVersion
	Capabilities         Capabilities
	TTLSeconds           *int
	NodeIDConflict       bool
	MeshUnusable         bool
	CapabilitiesUnusable bool
	TTLUnusable          bool
}

// PictureUp reports session picture liveness. Discovery never sets it.
// HDMI and ADV health are not this advertisement.
func (Advertisement) PictureUp() bool { return false }

// SilenceReleasesLease reports whether advertisement silence frees a kit
// lease. It is always false. Silence past a parsed TTL is absence for a
// future placement choice only. Lease release stays claim, renew, and expiry.
func (Advertisement) SilenceReleasesLease() bool { return false }

// DirectBindable reports a Phase 0 bind: discovery protocol and target id.
// A missing mesh version, a mismatched mesh major, and a parse-only TTL do
// not remove that bind.
func (a Advertisement) DirectBindable() bool {
	return a.DiscoveryProtocol == protocolVersion && ValidID(a.TargetID)
}

// MeshSessionCompatible reports a session that needs this build's mesh
// contract. Phase 0 omission is directly bindable and is not mesh-compatible.
// A major mismatch fails closed here and still leaves DirectBindable set.
func (a Advertisement) MeshSessionCompatible() bool {
	return a.DirectBindable() &&
		!a.NodeIDConflict &&
		!a.MeshUnusable &&
		!a.CapabilitiesUnusable &&
		a.Mesh != nil &&
		a.Mesh.Major == meshProtocolMajor
}

// ReadyForBoundExecutor keeps Phase 0 composition as the only Ready signal.
// foreign is another node's advertisement. Its Execute capability cannot
// grant Ready.
func ReadyForBoundExecutor(compositionReady bool, _ Advertisement) bool {
	return compositionReady
}

// KitCapabilities is what the target agent can advertise honestly.
// Execute is fpga_native without ABI families: advertisement starts before
// this process has a package inventory. An empty family list is not "any RBF".
// DisplaySink means the kit can present, not that HDMI or ADV is live.
// InputSource is the kit's local pad path for one play session.
// Catalog, Content, Shell, and Coordinator are omitted.
func KitCapabilities() Capabilities {
	return Capabilities{
		Execute:     []Execute{{Kind: ExecuteFPGANative}},
		DisplaySink: true,
		InputSource: true,
	}
}

// EncodeKitTXT builds the kit announcement. node_id is id. This does not
// mint a second identifier.
func EncodeKitTXT(id string) ([]string, error) {
	return EncodeTXT(id, KitCapabilities())
}

// EncodeTXT builds additive DNS-SD text. protocol and target_id stay the
// Phase 0 keys. node_id repeats id. mesh and cap follow.
func EncodeTXT(id string, caps Capabilities) ([]string, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("target ID is invalid")
	}
	cap, err := encodeCapabilities(caps)
	if err != nil {
		return nil, err
	}
	text := []string{
		"protocol=" + protocolVersion,
		"target_id=" + id,
		nodeIDKey + "=" + id,
		meshKey + "=" + MeshProtocol,
	}
	if cap != "" {
		text = append(text, capKey+"="+cap)
	}
	return text, nil
}

// ParseTXT reads discovery text. Omitted mesh fields leave a Phase 0
// advertisement. A ttl value is stored and does not arm a lease clock.
func ParseTXT(text map[string]string) Advertisement {
	ad := Advertisement{
		TargetID:          text["target_id"],
		NodeID:            text["target_id"],
		DiscoveryProtocol: text["protocol"],
	}
	if node, ok := text[nodeIDKey]; ok {
		switch {
		case node == ad.TargetID && ValidID(node):
			ad.NodeID = node
		default:
			// Keep the stable target id. Do not adopt a second identifier.
			ad.NodeID = ad.TargetID
			ad.NodeIDConflict = true
		}
	}
	if raw, ok := text[meshKey]; ok {
		version, ok := parseMeshVersion(raw)
		if !ok {
			ad.MeshUnusable = true
		} else {
			ad.Mesh = &version
		}
	}
	if raw, ok := text[capKey]; ok {
		caps, ok := parseCapabilities(raw)
		if !ok {
			ad.CapabilitiesUnusable = true
		} else {
			ad.Capabilities = caps
		}
	}
	if raw, ok := text[ttlKey]; ok {
		seconds, ok := parseTTL(raw)
		if !ok {
			ad.TTLUnusable = true
		} else {
			ad.TTLSeconds = &seconds
		}
	}
	return ad
}

func encodeCapabilities(caps Capabilities) (string, error) {
	tokens := make([]string, 0, 2+len(caps.Execute))
	if caps.DisplaySink {
		tokens = append(tokens, capDisplaySink)
	}
	if caps.InputSource {
		tokens = append(tokens, capInputSource)
	}
	executes := append([]Execute(nil), caps.Execute...)
	sort.Slice(executes, func(i, j int) bool { return executes[i].Kind < executes[j].Kind })
	seenKind := map[string]struct{}{}
	for _, execute := range executes {
		if !validToken(execute.Kind) {
			return "", fmt.Errorf("execute kind %q is invalid", execute.Kind)
		}
		if _, dup := seenKind[execute.Kind]; dup {
			return "", fmt.Errorf("execute kind %q is repeated", execute.Kind)
		}
		seenKind[execute.Kind] = struct{}{}
		token := capExecute + ":" + execute.Kind
		abis := append([]ABI(nil), execute.ABIs...)
		sort.Slice(abis, func(i, j int) bool {
			if abis[i].ID != abis[j].ID {
				return abis[i].ID < abis[j].ID
			}
			return abis[i].Major < abis[j].Major
		})
		seenABI := map[string]struct{}{}
		for _, abi := range abis {
			if !validABIID(abi.ID) || abi.Major < 0 || abi.Major > 65535 {
				return "", fmt.Errorf("execute abi is invalid")
			}
			key := abi.ID + "/" + strconv.Itoa(abi.Major)
			if _, dup := seenABI[key]; dup {
				continue
			}
			seenABI[key] = struct{}{}
			token += ":" + key
		}
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	return strings.Join(tokens, ","), nil
}

func parseMeshVersion(raw string) (MeshVersion, bool) {
	majorText, minorText, ok := strings.Cut(raw, ".")
	if !ok || strings.Contains(minorText, ".") {
		return MeshVersion{}, false
	}
	major, ok := parseSmallInt(majorText)
	if !ok {
		return MeshVersion{}, false
	}
	minor, ok := parseSmallInt(minorText)
	if !ok {
		return MeshVersion{}, false
	}
	return MeshVersion{Major: major, Minor: minor}, true
}

func parseCapabilities(raw string) (Capabilities, bool) {
	if raw == "" || len(raw) > 255 || strings.Contains(raw, " ") {
		return Capabilities{}, false
	}
	var caps Capabilities
	executes := map[string]*Execute{}
	for _, token := range strings.Split(raw, ",") {
		if token == "" {
			return Capabilities{}, false
		}
		switch token {
		case capDisplaySink:
			caps.DisplaySink = true
		case capInputSource:
			caps.InputSource = true
		default:
			kind, abis, ok := parseExecuteToken(token)
			if !ok {
				// Unknown tokens are optional minor fields. A broken execute
				// token makes the bag unusable rather than half-trusted.
				if strings.HasPrefix(token, capExecute+":") || strings.Contains(token, " ") {
					return Capabilities{}, false
				}
				continue
			}
			current := executes[kind]
			if current == nil {
				current = &Execute{Kind: kind}
				executes[kind] = current
			}
			current.ABIs = append(current.ABIs, abis...)
		}
	}
	if len(executes) > 0 {
		kinds := make([]string, 0, len(executes))
		for kind := range executes {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		caps.Execute = make([]Execute, 0, len(kinds))
		for _, kind := range kinds {
			execute := executes[kind]
			execute.ABIs = dedupABIs(execute.ABIs)
			caps.Execute = append(caps.Execute, *execute)
		}
	}
	return caps, true
}

func parseExecuteToken(token string) (string, []ABI, bool) {
	parts := strings.Split(token, ":")
	if len(parts) < 2 || parts[0] != capExecute || !validToken(parts[1]) {
		return "", nil, false
	}
	abis := make([]ABI, 0, len(parts)-2)
	for _, spec := range parts[2:] {
		id, majorText, ok := strings.Cut(spec, "/")
		if !ok || !validABIID(id) {
			return "", nil, false
		}
		major, ok := parseSmallInt(majorText)
		if !ok {
			return "", nil, false
		}
		abis = append(abis, ABI{ID: id, Major: major})
	}
	return parts[1], abis, true
}

func dedupABIs(abis []ABI) []ABI {
	sort.Slice(abis, func(i, j int) bool {
		if abis[i].ID != abis[j].ID {
			return abis[i].ID < abis[j].ID
		}
		return abis[i].Major < abis[j].Major
	})
	out := abis[:0]
	var prev string
	for _, abi := range abis {
		key := abi.ID + "/" + strconv.Itoa(abi.Major)
		if key == prev {
			continue
		}
		prev = key
		out = append(out, abi)
	}
	return out
}

func parseTTL(raw string) (int, bool) {
	if len(raw) > 8 {
		return 0, false
	}
	seconds, ok := parseSmallInt(raw)
	if !ok || seconds < 1 {
		return 0, false
	}
	return seconds, true
}

func parseSmallInt(raw string) (int, bool) {
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 65535 {
		return 0, false
	}
	return n, true
}

func validToken(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func validABIID(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}
