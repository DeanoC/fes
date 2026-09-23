// Package meshcontent names Phase 2 slot identities.
//
// A catalog entry can carry a package / ABI identity and the BIOS,
// primary-media, and expansion content-ids a later slice will pull.
// This package stores presence of those ids. It does not transfer
// bytes, and rooms Ready does not call it.
//
// The content-id algorithm is an unsigned strawman: sha256. Deano has
// not locked it.
package meshcontent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	// AlgorithmSHA256 is the unsigned Phase 2 strawman. Rejecting every
	// other name keeps a later lock from having to interpret garbage.
	AlgorithmSHA256 = "sha256"

	SlotPackageABI   = "package_abi"
	SlotBIOS         = "bios"
	SlotPrimaryMedia = "primary_media"
	SlotExpansion    = "expansion"
)

var (
	// ErrAlgorithm reports a content-id whose algorithm is not the
	// unsigned sha256 strawman.
	ErrAlgorithm = errors.New("content-id algorithm is not the unsigned sha256 strawman")
	// ErrContentID reports a content-id that is not canonical.
	ErrContentID = errors.New("content-id is invalid")
	// ErrEntry reports a catalog entry that cannot name its slots.
	ErrEntry = errors.New("catalog entry is invalid")
)

// ContentID names bytes for one BIOS, primary-media, or expansion slot.
// It is not a title id and it is not a package id.
type ContentID struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}

func (id ContentID) String() string {
	return id.Algorithm + ":" + id.Digest
}

// Validate accepts only the sha256 strawman: 64 lowercase hex digits.
func (id ContentID) Validate() error {
	if id.Algorithm != AlgorithmSHA256 {
		return fmt.Errorf("%w: %q", ErrAlgorithm, id.Algorithm)
	}
	if !hex64(id.Digest) {
		return fmt.Errorf("%w: digest", ErrContentID)
	}
	return nil
}

// ParseContentID reads the canonical text form sha256:<64 hex>.
func ParseContentID(text string) (ContentID, error) {
	algorithm, digest, ok := strings.Cut(text, ":")
	if !ok || strings.Contains(digest, ":") {
		return ContentID{}, fmt.Errorf("%w: text", ErrContentID)
	}
	id := ContentID{Algorithm: algorithm, Digest: digest}
	if err := id.Validate(); err != nil {
		return ContentID{}, err
	}
	return id, nil
}

// FromSHA256 adapts a digest the host already stores (catalog content,
// core-media id, firmware bytes). It does not hash a package archive
// and it does not accept a file path.
func FromSHA256(digest string) (ContentID, error) {
	id := ContentID{Algorithm: AlgorithmSHA256, Digest: digest}
	if err := id.Validate(); err != nil {
		return ContentID{}, err
	}
	return id, nil
}

// SumSHA256 hashes one slot's bytes with the unsigned strawman.
// Package / ABI identity does not use this function.
func SumSHA256(slot []byte) ContentID {
	sum := sha256.Sum256(slot)
	return ContentID{Algorithm: AlgorithmSHA256, Digest: hex.EncodeToString(sum[:])}
}

// PackageABI is the described package and the ABI it runs. PackageID is
// the existing 64-hex package identity, not a content-id of a raw RBF.
type PackageABI struct {
	PackageID string `json:"package_id"`
	ABI       string `json:"abi"`
	Major     int    `json:"major"`
}

func (p PackageABI) Validate() error {
	if !hex64(p.PackageID) {
		return fmt.Errorf("%w: package id", ErrEntry)
	}
	if !token(p.ABI, true) {
		return fmt.Errorf("%w: abi", ErrEntry)
	}
	if p.Major < 1 || p.Major > 65535 {
		return fmt.Errorf("%w: abi major", ErrEntry)
	}
	return nil
}

// Slot is one required composition identity.
// Package is set for package_abi. Content is set for the other kinds.
type Slot struct {
	Kind    string      `json:"kind"`
	Name    string      `json:"name,omitempty"`
	Package *PackageABI `json:"package,omitempty"`
	Content *ContentID  `json:"content,omitempty"`
}

// PackageSlot builds the package / ABI slot.
func PackageSlot(pkg PackageABI) Slot {
	return Slot{Kind: SlotPackageABI, Package: &pkg}
}

// BIOSSlot builds the firmware content-id slot.
func BIOSSlot(id ContentID) Slot {
	return Slot{Kind: SlotBIOS, Content: &id}
}

// PrimaryMediaSlot builds the cart, ROM, or other primary-medium slot.
func PrimaryMediaSlot(id ContentID) Slot {
	return Slot{Kind: SlotPrimaryMedia, Content: &id}
}

// ExpansionSlot builds one named expansion content-id.
func ExpansionSlot(name string, id ContentID) Slot {
	return Slot{Kind: SlotExpansion, Name: name, Content: &id}
}

// Execute is the capability kind this entry requires. One kind. This is
// not a discovered node.
type Execute struct {
	Kind string `json:"kind"`
}

// Entry is a catalog row a shell may show. Paths are not fields.
// TitleID is the game id, distinct from every content-id.
type Entry struct {
	TitleID    string    `json:"title_id"`
	System     string    `json:"system"`
	Slots      []Slot    `json:"slots"`
	Execute    []Execute `json:"execute,omitempty"`
	Launchable bool      `json:"launchable"`
}

// ContentIDs returns BIOS, primary-media, and expansion ids in slot
// order. The package / ABI slot is omitted. Callers still Validate.
func (e Entry) ContentIDs() []ContentID {
	out := make([]ContentID, 0, len(e.Slots))
	for _, slot := range e.Slots {
		if slot.Content == nil || slot.Kind == SlotPackageABI {
			continue
		}
		out = append(out, *slot.Content)
	}
	return out
}

// Validate checks the catalog shape. It does not look at a node, a
// lease, or a cache.
func (e Entry) Validate() error {
	if err := validTitle(e.TitleID); err != nil {
		return err
	}
	if !token(e.System, false) {
		return fmt.Errorf("%w: system", ErrEntry)
	}
	var packages, bios, primary int
	expansions := map[string]struct{}{}
	for _, slot := range e.Slots {
		switch slot.Kind {
		case SlotPackageABI:
			packages++
			if slot.Content != nil || slot.Name != "" || slot.Package == nil {
				return fmt.Errorf("%w: package slot", ErrEntry)
			}
			if err := slot.Package.Validate(); err != nil {
				return err
			}
			if e.TitleID == slot.Package.PackageID {
				return fmt.Errorf("%w: title is a package id", ErrEntry)
			}
		case SlotBIOS, SlotPrimaryMedia:
			if slot.Kind == SlotBIOS {
				bios++
			} else {
				primary++
			}
			if err := validContentSlot(e.TitleID, slot); err != nil {
				return err
			}
		case SlotExpansion:
			if slot.Name == "" || !token(slot.Name, true) {
				return fmt.Errorf("%w: expansion name", ErrEntry)
			}
			if _, dup := expansions[slot.Name]; dup {
				return fmt.Errorf("%w: expansion %s", ErrEntry, slot.Name)
			}
			expansions[slot.Name] = struct{}{}
			if err := validContentSlot(e.TitleID, slot); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: slot kind", ErrEntry)
		}
	}
	if packages > 1 || bios > 1 || primary > 1 {
		return fmt.Errorf("%w: repeated slot", ErrEntry)
	}
	if !e.Launchable {
		for _, execute := range e.Execute {
			if !token(execute.Kind, false) {
				return fmt.Errorf("%w: execute", ErrEntry)
			}
		}
		return nil
	}
	if len(e.Execute) != 1 || !token(e.Execute[0].Kind, false) {
		return fmt.Errorf("%w: execute", ErrEntry)
	}
	if packages != 1 {
		return fmt.Errorf("%w: package slot", ErrEntry)
	}
	return nil
}

func validContentSlot(title string, slot Slot) error {
	if slot.Package != nil || slot.Content == nil {
		return fmt.Errorf("%w: %s slot", ErrEntry, slot.Kind)
	}
	if err := slot.Content.Validate(); err != nil {
		return err
	}
	if title == slot.Content.String() {
		return fmt.Errorf("%w: title is a content-id", ErrEntry)
	}
	return nil
}

func validTitle(title string) error {
	if title == "" || len(title) > 128 || strings.ContainsAny(title, "/\\ \t") {
		return fmt.Errorf("%w: title", ErrEntry)
	}
	if _, err := ParseContentID(title); err == nil {
		return fmt.Errorf("%w: title is a content-id", ErrEntry)
	}
	return nil
}

// Cache is the set of content-ids one executor already holds.
// Hold records the id only. There is no byte store and no peer.
type Cache struct {
	mu   sync.Mutex
	held map[string]struct{}
}

// NewCache returns an empty executor-local presence set.
func NewCache() *Cache {
	return &Cache{held: map[string]struct{}{}}
}

// Hold records id on this executor. Invalid ids are rejected.
func (c *Cache) Hold(id ContentID) error {
	if c == nil {
		return errors.New("meshcontent: nil cache")
	}
	if err := id.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held[id.String()] = struct{}{}
	return nil
}

// Holds reports whether this executor already has id.
func (c *Cache) Holds(id ContentID) bool {
	if c == nil {
		return false
	}
	if err := id.Validate(); err != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.held[id.String()]
	return ok
}

// Block is why ReadyHere is false. Values are codes, not sofa copy.
type Block string

const (
	BlockNone           Block = ""
	BlockInvalid        Block = "invalid"
	BlockBrowseOnly     Block = "browse_only"
	BlockVersionSkew    Block = "version_skew"
	BlockLeaseHeld      Block = "lease_held"
	BlockNoExecutor     Block = "no_capable_executor"
	BlockContentMissing Block = "content_missing"
	BlockDistant        Block = "distant"
	BlockEnsureProgress Block = "ensure_in_progress"
)

// Bound is what this session already knows about one executor.
// Execute means a binding this shell can use, not another node's
// advertisement. Packages are described package ids on that executor.
// Local, Distant, and Checking are content-id presence. ReadyHere does
// not discover nodes and does not pull bytes.
type Bound struct {
	Execute     bool
	LeaseFree   bool
	MeshMajorOK bool
	Local       *Cache
	Distant     *Cache
	Checking    []ContentID
	Packages    []string
}

// ReadyHere is the Phase 2 rule: this session can play here.
// Rooms, session launch, and discovery.ReadyForBoundExecutor do not
// call it. A true result is not Phase 0 or Phase 1 Ready.
//
// A required content slot that is not on the executor, not distant, and
// not mid-pull is content missing. Distant-only is not Ready. Mid-pull
// is Checking. A fully local id wins over distant and checking.
func ReadyHere(entry Entry, bound Bound) (bool, Block) {
	if err := entry.Validate(); err != nil {
		return false, BlockInvalid
	}
	if !entry.Launchable {
		return false, BlockBrowseOnly
	}
	if !bound.MeshMajorOK {
		return false, BlockVersionSkew
	}
	if !bound.LeaseFree {
		return false, BlockLeaseHeld
	}
	if !bound.Execute || !packageHeld(entry, bound.Packages) {
		return false, BlockNoExecutor
	}
	switch worstContent(entry, bound) {
	case presenceMissing:
		return false, BlockContentMissing
	case presenceDistant:
		return false, BlockDistant
	case presenceChecking:
		return false, BlockEnsureProgress
	default:
		return true, BlockNone
	}
}

type presence int

const (
	presenceEnsured presence = iota
	presenceChecking
	presenceDistant
	presenceMissing
)

func packageHeld(entry Entry, packages []string) bool {
	var id string
	for _, slot := range entry.Slots {
		if slot.Kind == SlotPackageABI && slot.Package != nil {
			id = slot.Package.PackageID
			break
		}
	}
	for _, held := range packages {
		if held == id {
			return true
		}
	}
	return false
}

func worstContent(entry Entry, bound Bound) presence {
	worst := presenceEnsured
	for _, id := range entry.ContentIDs() {
		state := presenceOf(id, bound)
		if state > worst {
			worst = state
		}
	}
	return worst
}

func presenceOf(id ContentID, bound Bound) presence {
	if bound.Local.Holds(id) {
		return presenceEnsured
	}
	if checking(bound.Checking, id) {
		return presenceChecking
	}
	if bound.Distant.Holds(id) {
		return presenceDistant
	}
	return presenceMissing
}

func checking(ids []ContentID, want ContentID) bool {
	text := want.String()
	for _, id := range ids {
		if id.String() == text && id.Validate() == nil {
			return true
		}
	}
	return false
}

func hex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func token(value string, dotted bool) bool {
	if value == "" || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-':
		case dotted && (c == '.'):
		default:
			return false
		}
	}
	return true
}
