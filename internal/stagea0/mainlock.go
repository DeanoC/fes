package stagea0

import (
	"bytes"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

const (
	mainLockFormatV1     = 1
	mainLockSchemaV1     = "fogcast.stage-a0-main-lock"
	mainLockMaxBytes     = 8 << 20
	mainLockMaxSPDXBytes = 4096
	mainLockMaxSPDXDepth = 64
)

type mainLockWire struct {
	Format              *int                `toml:"format"`
	Schema              *string             `toml:"schema"`
	FogCastBaseRevision *string             `toml:"fogcast_base_revision"`
	SourceDateEpoch     *int64              `toml:"source_date_epoch"`
	Environment         environmentWire     `toml:"environment"`
	Main                mainWire            `toml:"main"`
	Build               buildWire           `toml:"build"`
	Materials           *[]materialWire     `toml:"materials"`
	Toolchains          *[]toolchainWire    `toml:"toolchains"`
	BuildUtilities      *[]buildUtilityWire `toml:"build_utilities"`
	Configs             *[]buildConfigWire  `toml:"configs"`
	Policies            *[]policyWire       `toml:"policies"`
	Licenses            *[]licenseWire      `toml:"licenses"`
}
type environmentWire struct {
	Locale              *string   `toml:"locale"`
	Timezone            *string   `toml:"timezone"`
	Umask               *string   `toml:"umask"`
	JobCount            *int      `toml:"job_count"`
	Network             *string   `toml:"network"`
	PathPolicy          *[]string `toml:"path_policy"`
	ContainerMaterialID *string   `toml:"container_material_id"`
}
type mainWire struct {
	UpstreamMaterialID         *string   `toml:"upstream_material_id"`
	ForkMaterialID             *string   `toml:"fork_material_id"`
	UpstreamCommit             *string   `toml:"upstream_commit"`
	UpstreamTree               *string   `toml:"upstream_tree"`
	ForkCommit                 *string   `toml:"fork_commit"`
	ForkTree                   *string   `toml:"fork_tree"`
	ForkParentCommit           *string   `toml:"fork_parent_commit"`
	PatchCommits               *[]string `toml:"patch_commits"`
	PublicationStatus          *string   `toml:"publication_status"`
	DurableRetrievalMaterialID *string   `toml:"durable_retrieval_material_id"`
}
type buildWire struct {
	Entrypoint            *[]string `toml:"entrypoint"`
	WorkingDirectory      *string   `toml:"working_directory"`
	VDateFormat           *string   `toml:"vdate_format"`
	VDateExpression       *string   `toml:"vdate_expression"`
	AllowedFinalArtifacts *[]string `toml:"allowed_final_artifacts"`
}
type materialWire struct {
	ID           *string           `toml:"id"`
	Role         *string           `toml:"role"`
	Kind         *string           `toml:"kind"`
	LicenseIDs   *[]string         `toml:"license_ids"`
	Purpose      *string           `toml:"purpose"`
	GitLocal     *gitLocalWire     `toml:"git_local"`
	GitHTTPS     *gitHTTPSWire     `toml:"git_https"`
	ArchiveHTTPS *archiveHTTPSWire `toml:"archive_https"`
	OCI          *ociWire          `toml:"oci"`
	GitSubtree   *gitSubtreeWire   `toml:"git_subtree"`
	MaterialFile *materialFileWire `toml:"material_file"`
}
type gitLocalWire struct {
	RepositoryID *string `toml:"repository_id"`
	Commit       *string `toml:"commit"`
	Tree         *string `toml:"tree"`
}
type gitHTTPSWire struct {
	RepositoryID *string `toml:"repository_id"`
	URL          *string `toml:"url"`
	Commit       *string `toml:"commit"`
	Tree         *string `toml:"tree"`
}
type archiveHTTPSWire struct {
	URL    *string `toml:"url"`
	Size   *int64  `toml:"size"`
	SHA256 *string `toml:"sha256"`
}
type ociWire struct {
	Reference      *string `toml:"reference"`
	ManifestDigest *string `toml:"manifest_digest"`
	ConfigDigest   *string `toml:"config_digest"`
	OS             *string `toml:"os"`
	Architecture   *string `toml:"architecture"`
}
type gitSubtreeWire struct {
	ParentID *string `toml:"parent_id"`
	Path     *string `toml:"path"`
	Tree     *string `toml:"tree"`
}
type materialFileWire struct {
	ParentID *string `toml:"parent_id"`
	Path     *string `toml:"path"`
	Size     *int64  `toml:"size"`
	SHA256   *string `toml:"sha256"`
}
type toolchainWire struct {
	ID           *string                   `toml:"id"`
	TargetTriple *string                   `toml:"target_triple"`
	Components   *[]toolchainComponentWire `toml:"components"`
}
type toolchainComponentWire struct {
	Role             *string `toml:"role"`
	MaterialID       *string `toml:"material_id"`
	LogicalPath      *string `toml:"logical_path"`
	ExecutableSHA256 *string `toml:"executable_sha256"`
	Version          *string `toml:"version"`
}
type buildUtilityWire struct {
	Role             *string `toml:"role"`
	MaterialID       *string `toml:"material_id"`
	LogicalPath      *string `toml:"logical_path"`
	ExecutableSHA256 *string `toml:"executable_sha256"`
	Version          *string `toml:"version"`
}
type buildConfigWire struct {
	ID         *string `toml:"id"`
	MaterialID *string `toml:"material_id"`
	Path       *string `toml:"path"`
	SHA256     *string `toml:"sha256"`
	Purpose    *string `toml:"purpose"`
}
type policyWire struct {
	ID         *string `toml:"id"`
	MaterialID *string `toml:"material_id"`
	Path       *string `toml:"path"`
	SHA256     *string `toml:"sha256"`
	Kind       *string `toml:"kind"`
}
type licenseWire struct {
	ID                         *string `toml:"id"`
	MaterialID                 *string `toml:"material_id"`
	SPDXExpression             *string `toml:"spdx_expression"`
	NoticeLocator              *string `toml:"notice_locator"`
	CorrespondingSourceLocator *string `toml:"corresponding_source_locator"`
	RedistributionStatus       *string `toml:"redistribution_status"`
}

func ParseMainLock(raw []byte) (MainLock, error) {
	if !validMainLockBytes(raw) {
		return MainLock{}, mainLockInvalid("lock bytes are not canonical")
	}
	d := toml.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var w mainLockWire
	if err := d.Decode(&w); err != nil {
		return MainLock{}, mainLockInvalid("lock TOML is invalid")
	}
	v, ok := w.value()
	if !ok {
		return MainLock{}, mainLockInvalid("lock has a missing required field")
	}
	if err := ValidateMainLock(v); err != nil {
		return MainLock{}, err
	}
	return v, nil
}

func (w mainLockWire) value() (MainLock, bool) {
	if w.Format == nil || w.Schema == nil || w.FogCastBaseRevision == nil || w.SourceDateEpoch == nil || !w.Environment.present() || !w.Main.present() || !w.Build.present() || w.Materials == nil || w.Toolchains == nil || w.BuildUtilities == nil || w.Configs == nil || w.Policies == nil || w.Licenses == nil {
		return MainLock{}, false
	}
	if (*w.Main.PublicationStatus == "local-only" && w.Main.DurableRetrievalMaterialID != nil) || (*w.Main.PublicationStatus == "durably-retrievable" && w.Main.DurableRetrievalMaterialID == nil) {
		return MainLock{}, false
	}
	v := MainLock{Format: *w.Format, Schema: *w.Schema, FogCastBaseRevision: *w.FogCastBaseRevision, SourceDateEpoch: *w.SourceDateEpoch, Environment: w.Environment.value(), Main: w.Main.value(), Build: w.Build.value()}
	for _, x := range *w.Materials {
		if x.Purpose != nil && *x.Purpose == "" {
			return MainLock{}, false
		}
		y, ok := x.value()
		if !ok {
			return MainLock{}, false
		}
		v.Materials = append(v.Materials, y)
	}
	for _, x := range *w.Toolchains {
		y, ok := x.value()
		if !ok {
			return MainLock{}, false
		}
		v.Toolchains = append(v.Toolchains, y)
	}
	for _, x := range *w.BuildUtilities {
		y, ok := x.value()
		if !ok {
			return MainLock{}, false
		}
		v.BuildUtilities = append(v.BuildUtilities, y)
	}
	for _, x := range *w.Configs {
		y, ok := x.value()
		if !ok {
			return MainLock{}, false
		}
		v.Configs = append(v.Configs, y)
	}
	for _, x := range *w.Policies {
		y, ok := x.value()
		if !ok {
			return MainLock{}, false
		}
		v.Policies = append(v.Policies, y)
	}
	for _, x := range *w.Licenses {
		y, ok := x.value()
		if !ok {
			return MainLock{}, false
		}
		v.Licenses = append(v.Licenses, y)
	}
	return v, true
}
func (w environmentWire) present() bool {
	return w.Locale != nil && w.Timezone != nil && w.Umask != nil && w.JobCount != nil && w.Network != nil && w.PathPolicy != nil && w.ContainerMaterialID != nil
}
func (w environmentWire) value() MainLockEnvironment {
	return MainLockEnvironment{*w.Locale, *w.Timezone, *w.Umask, *w.Network, *w.ContainerMaterialID, *w.JobCount, append([]string(nil), (*w.PathPolicy)...)}
}
func (w mainWire) present() bool {
	return w.UpstreamMaterialID != nil && w.ForkMaterialID != nil && w.UpstreamCommit != nil && w.UpstreamTree != nil && w.ForkCommit != nil && w.ForkTree != nil && w.ForkParentCommit != nil && w.PatchCommits != nil && w.PublicationStatus != nil
}
func (w mainWire) value() MainLockMain {
	d := ""
	if w.DurableRetrievalMaterialID != nil {
		d = *w.DurableRetrievalMaterialID
	}
	return MainLockMain{*w.UpstreamMaterialID, *w.ForkMaterialID, *w.UpstreamCommit, *w.UpstreamTree, *w.ForkCommit, *w.ForkTree, *w.ForkParentCommit, append([]string(nil), (*w.PatchCommits)...), *w.PublicationStatus, d}
}
func (w buildWire) present() bool {
	return w.Entrypoint != nil && w.WorkingDirectory != nil && w.VDateFormat != nil && w.VDateExpression != nil && w.AllowedFinalArtifacts != nil
}
func (w buildWire) value() MainLockBuild {
	return MainLockBuild{append([]string(nil), (*w.Entrypoint)...), *w.WorkingDirectory, *w.VDateFormat, *w.VDateExpression, append([]string(nil), (*w.AllowedFinalArtifacts)...)}
}
func (w materialWire) value() (Material, bool) {
	if w.ID == nil || w.Role == nil || w.Kind == nil || w.LicenseIDs == nil {
		return Material{}, false
	}
	m := Material{ID: *w.ID, Role: *w.Role, Kind: *w.Kind, LicenseIDs: append([]string(nil), (*w.LicenseIDs)...)}
	if w.Purpose != nil {
		m.Purpose = *w.Purpose
	}
	n := 0
	if w.GitLocal != nil {
		n++
		if w.GitLocal.RepositoryID == nil || w.GitLocal.Commit == nil || w.GitLocal.Tree == nil {
			return Material{}, false
		}
		m.GitLocal = &GitLocalMaterial{*w.GitLocal.RepositoryID, *w.GitLocal.Commit, *w.GitLocal.Tree}
	}
	if w.GitHTTPS != nil {
		n++
		if w.GitHTTPS.RepositoryID == nil || w.GitHTTPS.URL == nil || w.GitHTTPS.Commit == nil || w.GitHTTPS.Tree == nil {
			return Material{}, false
		}
		m.GitHTTPS = &GitHTTPSMaterial{*w.GitHTTPS.RepositoryID, *w.GitHTTPS.URL, *w.GitHTTPS.Commit, *w.GitHTTPS.Tree}
	}
	if w.ArchiveHTTPS != nil {
		n++
		if w.ArchiveHTTPS.URL == nil || w.ArchiveHTTPS.Size == nil || w.ArchiveHTTPS.SHA256 == nil {
			return Material{}, false
		}
		m.ArchiveHTTPS = &ArchiveHTTPSMaterial{*w.ArchiveHTTPS.URL, *w.ArchiveHTTPS.Size, *w.ArchiveHTTPS.SHA256}
	}
	if w.OCI != nil {
		n++
		if w.OCI.Reference == nil || w.OCI.ManifestDigest == nil || w.OCI.ConfigDigest == nil || w.OCI.OS == nil || w.OCI.Architecture == nil {
			return Material{}, false
		}
		m.OCI = &OCIMaterial{*w.OCI.Reference, *w.OCI.ManifestDigest, *w.OCI.ConfigDigest, *w.OCI.OS, *w.OCI.Architecture}
	}
	if w.GitSubtree != nil {
		n++
		if w.GitSubtree.ParentID == nil || w.GitSubtree.Path == nil || w.GitSubtree.Tree == nil {
			return Material{}, false
		}
		m.GitSubtree = &GitSubtreeMaterial{*w.GitSubtree.ParentID, *w.GitSubtree.Path, *w.GitSubtree.Tree}
	}
	if w.MaterialFile != nil {
		n++
		if w.MaterialFile.ParentID == nil || w.MaterialFile.Path == nil || w.MaterialFile.Size == nil || w.MaterialFile.SHA256 == nil {
			return Material{}, false
		}
		m.MaterialFile = &MaterialFile{*w.MaterialFile.ParentID, *w.MaterialFile.Path, *w.MaterialFile.Size, *w.MaterialFile.SHA256}
	}
	return m, n == 1
}
func (w toolchainWire) value() (Toolchain, bool) {
	if w.ID == nil || w.TargetTriple == nil || w.Components == nil {
		return Toolchain{}, false
	}
	v := Toolchain{ID: *w.ID, TargetTriple: *w.TargetTriple}
	for _, x := range *w.Components {
		if x.Role == nil || x.MaterialID == nil || x.LogicalPath == nil {
			return Toolchain{}, false
		}
		y := ToolchainComponent{Role: *x.Role, MaterialID: *x.MaterialID, LogicalPath: *x.LogicalPath}
		if x.ExecutableSHA256 != nil {
			value := *x.ExecutableSHA256
			y.ExecutableSHA256 = &value
		}
		if x.Version != nil {
			value := *x.Version
			y.Version = &value
		}
		v.Components = append(v.Components, y)
	}
	return v, true
}
func (w buildUtilityWire) value() (BuildUtility, bool) {
	if w.Role == nil || w.MaterialID == nil || w.LogicalPath == nil || w.ExecutableSHA256 == nil || w.Version == nil {
		return BuildUtility{}, false
	}
	return BuildUtility{*w.Role, *w.MaterialID, *w.LogicalPath, *w.ExecutableSHA256, *w.Version}, true
}
func (w buildConfigWire) value() (BuildConfig, bool) {
	if w.ID == nil || w.MaterialID == nil || w.Path == nil || w.SHA256 == nil || w.Purpose == nil {
		return BuildConfig{}, false
	}
	return BuildConfig{*w.ID, *w.MaterialID, *w.Path, *w.SHA256, *w.Purpose}, true
}
func (w policyWire) value() (Policy, bool) {
	if w.ID == nil || w.MaterialID == nil || w.Path == nil || w.SHA256 == nil || w.Kind == nil {
		return Policy{}, false
	}
	return Policy{*w.ID, *w.MaterialID, *w.Path, *w.SHA256, *w.Kind}, true
}
func (w licenseWire) value() (License, bool) {
	if w.ID == nil || w.MaterialID == nil || w.SPDXExpression == nil || w.NoticeLocator == nil || w.CorrespondingSourceLocator == nil || w.RedistributionStatus == nil {
		return License{}, false
	}
	return License{*w.ID, *w.MaterialID, *w.SPDXExpression, *w.NoticeLocator, *w.CorrespondingSourceLocator, *w.RedistributionStatus}, true
}

func ValidateMainLock(v MainLock) error {
	bad := mainLockInvalid
	if v.Format != mainLockFormatV1 || v.Schema != mainLockSchemaV1 || !lowerHex(v.FogCastBaseRevision, 40) || v.SourceDateEpoch < 0 {
		return bad("format, schema, or source epoch is invalid")
	}
	e := v.Environment
	if e.Locale != "C" || e.Timezone != "UTC" || e.Umask != "022" || e.JobCount <= 0 || e.Network != "disabled-during-build" || !sameStrings(e.PathPolicy, []string{"/stage-a0/build-utils/bin", "/stage-a0/toolchain/bin"}) {
		return bad("environment is invalid")
	}
	if err := validateBuild(v.Build); err != nil {
		return err
	}
	mats, err := validateMaterials(v.Materials)
	if err != nil {
		return err
	}
	if _, ok := mats[e.ContainerMaterialID]; !ok {
		return bad("container material is unresolved")
	}
	if mats[e.ContainerMaterialID].OCI == nil || mats[e.ContainerMaterialID].Role != "consumed-build-input" {
		return bad("container material is invalid")
	}
	if err := validateMain(v.Main, mats); err != nil {
		return err
	}
	if err := validateToolchains(v.Toolchains, mats); err != nil {
		return err
	}
	if err := validateUtilities(v.BuildUtilities, mats); err != nil {
		return err
	}
	if err := validateConfigs(v.Configs, mats); err != nil {
		return err
	}
	if err := validatePolicies(v.Policies, mats); err != nil {
		return err
	}
	return validateLicenses(v.Licenses, mats)
}
func validateBuild(v MainLockBuild) error {
	if len(v.Entrypoint) == 0 || !logicalBelow(v.Entrypoint[0], "/stage-a0/build-utils/bin") || !safeAbs(v.WorkingDirectory) || v.WorkingDirectory != "/stage-a0/src" || v.VDateFormat != "YYMMDD" || v.VDateExpression != "%y%m%d" || !sameStrings(v.AllowedFinalArtifacts, []string{"bin/MiSTer", "bin/MiSTer.elf"}) {
		return mainLockInvalid("build is invalid")
	}
	for _, a := range v.Entrypoint {
		if !validMainLockString(a) || (strings.HasPrefix(a, "/") && !safeAbs(a)) {
			return mainLockInvalid("entrypoint is invalid")
		}
	}
	return nil
}
func validateMaterials(xs []Material) (map[string]Material, error) {
	if len(xs) == 0 {
		return nil, mainLockInvalid("materials are empty")
	}
	out := map[string]Material{}
	last := ""
	for _, m := range xs {
		if m.ID == "" || m.ID <= last || !safeID(m.ID) || !oneOf(m.Role, "consumed-build-input", "context-only", "future-overlord-input", "historical-comparator") || len(m.LicenseIDs) == 0 || !sortedUnique(m.LicenseIDs) || (m.Purpose != "" && !validMainLockString(m.Purpose)) {
			return nil, mainLockInvalid("material identity or ordering is invalid")
		}
		last = m.ID
		if _, ok := out[m.ID]; ok {
			return nil, mainLockInvalid("duplicate material")
		}
		if err := validateMaterial(m, out); err != nil {
			return nil, err
		}
		out[m.ID] = m
	}
	return out, nil
}
func validateMaterial(m Material, prior map[string]Material) error {
	n := 0
	if m.GitLocal != nil {
		n++
		if m.Kind != "git-local" || !safeID(m.GitLocal.RepositoryID) || !lowerHex(m.GitLocal.Commit, 40) || !lowerHex(m.GitLocal.Tree, 40) {
			return mainLockInvalid("git-local material is invalid")
		}
	}
	if m.GitHTTPS != nil {
		n++
		if m.Kind != "git-https" || !safeID(m.GitHTTPS.RepositoryID) || !lowerHex(m.GitHTTPS.Commit, 40) || !lowerHex(m.GitHTTPS.Tree, 40) {
			return mainLockInvalid("git HTTPS material is invalid")
		}
		if err := immutableURL(m.GitHTTPS.URL); err != nil {
			return err
		}
	}
	if m.ArchiveHTTPS != nil {
		n++
		if m.Kind != "archive-https" || m.ArchiveHTTPS.Size < 0 || !lowerHex(m.ArchiveHTTPS.SHA256, 64) {
			return mainLockInvalid("archive material is invalid")
		}
		if err := immutableURL(m.ArchiveHTTPS.URL); err != nil {
			return err
		}
	}
	if m.OCI != nil {
		n++
		o := m.OCI
		if m.Kind != "oci" {
			return mainLockInvalid("OCI material is invalid")
		}
		if err := validateOCI(o.Reference, o.ManifestDigest, o.ConfigDigest, o.OS, o.Architecture); err != nil {
			return err
		}
	}
	if m.GitSubtree != nil {
		n++
		x := m.GitSubtree
		if m.Kind != "git-subtree" || !safeRelative(x.Path) || !lowerHex(x.Tree, 40) || !priorMaterial(prior, x.ParentID) {
			return mainLockInvalid("git subtree material is invalid")
		}
	}
	if m.MaterialFile != nil {
		n++
		x := m.MaterialFile
		if m.Kind != "material-file" || !safeRelative(x.Path) || x.Size < 0 || !lowerHex(x.SHA256, 64) || !priorMaterial(prior, x.ParentID) {
			return mainLockInvalid("material file is invalid")
		}
	}
	if n != 1 {
		return mainLockInvalid("material union is invalid")
	}
	return nil
}
func validateMain(m MainLockMain, mats map[string]Material) error {
	if !safeID(m.UpstreamMaterialID) || !safeID(m.ForkMaterialID) || !validMainLockString(m.PublicationStatus) || (m.DurableRetrievalMaterialID != "" && !safeID(m.DurableRetrievalMaterialID)) {
		return mainLockInvalid("main identity is invalid")
	}
	if !lowerHex(m.UpstreamCommit, 40) || !lowerHex(m.UpstreamTree, 40) || !lowerHex(m.ForkCommit, 40) || !lowerHex(m.ForkTree, 40) || m.ForkParentCommit != m.UpstreamCommit || len(m.PatchCommits) == 0 || !uniqueHex(m.PatchCommits, 40) || m.PatchCommits[len(m.PatchCommits)-1] != m.ForkCommit {
		return mainLockInvalid("main commits are invalid")
	}
	u, uok := mats[m.UpstreamMaterialID]
	f, fok := mats[m.ForkMaterialID]
	if !uok || !fok || u.Role != "consumed-build-input" || u.GitHTTPS == nil || u.GitHTTPS.Commit != m.UpstreamCommit || u.GitHTTPS.Tree != m.UpstreamTree || f.Role != "consumed-build-input" || ((f.GitLocal == nil) && (f.GitHTTPS == nil)) {
		return mainLockInvalid("main materials are invalid")
	}
	if f.GitLocal != nil && (f.GitLocal.Commit != m.ForkCommit || f.GitLocal.Tree != m.ForkTree) {
		return mainLockInvalid("fork material mismatch")
	}
	if f.GitHTTPS != nil && (f.GitHTTPS.Commit != m.ForkCommit || f.GitHTTPS.Tree != m.ForkTree) {
		return mainLockInvalid("fork material mismatch")
	}
	switch m.PublicationStatus {
	case "local-only":
		if m.DurableRetrievalMaterialID != "" {
			return mainLockInvalid("local publication has durable material")
		}
	case "durably-retrievable":
		d, ok := mats[m.DurableRetrievalMaterialID]
		if !ok || d.Role != "consumed-build-input" || d.GitHTTPS == nil || d.GitHTTPS.RepositoryID != forkRepo(f) || d.GitHTTPS.Commit != m.ForkCommit || d.GitHTTPS.Tree != m.ForkTree {
			return mainLockInvalid("durable retrieval is invalid")
		}
	default:
		return mainLockInvalid("publication status is invalid")
	}
	return nil
}
func forkRepo(m Material) string {
	if m.GitLocal != nil {
		return m.GitLocal.RepositoryID
	}
	return m.GitHTTPS.RepositoryID
}
func validateToolchains(xs []Toolchain, mats map[string]Material) error {
	if len(xs) == 0 {
		return mainLockInvalid("toolchains are empty")
	}
	last := ""
	seen := map[string]bool{}
	for _, x := range xs {
		if !safeID(x.ID) || x.ID <= last || seen[x.ID] || !validMainLockString(x.TargetTriple) {
			return mainLockInvalid("toolchain identity is invalid")
		}
		seen[x.ID] = true
		last = x.ID
		if err := validateComponents(x.Components, mats); err != nil {
			return err
		}
	}
	return nil
}

var baselineExecutable = []string{"compiler", "linker", "assembler", "archiver", "objcopy", "objdump", "strip", "readelf"}
var baselineRoots = []string{"binutils", "libc", "sysroot"}
var baselineUtilities = []string{"bash", "make", "git", "sed", "cp", "mkdir", "rm", "nproc-shim"}

func validateComponents(xs []ToolchainComponent, mats map[string]Material) error {
	if len(xs) == 0 {
		return mainLockInvalid("components are empty")
	}
	seen := map[string]bool{}
	last := ""
	for _, x := range xs {
		k := x.Role + "\x00" + x.MaterialID + "\x00" + x.LogicalPath
		if k <= last || seen[x.Role] || !safeKebab(x.Role) || !consumed(mats, x.MaterialID) {
			return mainLockInvalid("component ordering or reference is invalid")
		}
		seen[x.Role] = true
		last = k
		root := oneOf(x.Role, baselineRoots...)
		if root {
			if x.ExecutableSHA256 != nil || x.Version != nil {
				return mainLockInvalid("root component has executable fields")
			}
			if (x.Role == "binutils" && x.LogicalPath != "/stage-a0/toolchain") || ((x.Role == "libc" || x.Role == "sysroot") && x.LogicalPath != "/stage-a0/sysroot") {
				return mainLockInvalid("root component path is invalid")
			}
		} else {
			if x.ExecutableSHA256 == nil || x.Version == nil || !lowerHex(*x.ExecutableSHA256, 64) || !validMainLockString(*x.Version) || !logicalBelow(x.LogicalPath, "/stage-a0/toolchain/bin") {
				return mainLockInvalid("executable component is invalid")
			}
		}
	}
	for _, r := range append(append([]string{}, baselineExecutable...), baselineRoots...) {
		if !seen[r] {
			return mainLockInvalid("baseline component is missing")
		}
	}
	return nil
}
func validateUtilities(xs []BuildUtility, mats map[string]Material) error {
	if len(xs) == 0 {
		return mainLockInvalid("build utilities are empty")
	}
	seen := map[string]bool{}
	last := ""
	for _, x := range xs {
		k := x.Role + "\x00" + x.MaterialID + "\x00" + x.LogicalPath
		if k <= last || seen[x.Role] || !safeKebab(x.Role) || !consumed(mats, x.MaterialID) || !logicalBelow(x.LogicalPath, "/stage-a0/build-utils/bin") || !lowerHex(x.ExecutableSHA256, 64) || !validMainLockString(x.Version) {
			return mainLockInvalid("build utility is invalid")
		}
		seen[x.Role] = true
		last = k
	}
	for _, r := range baselineUtilities {
		if !seen[r] {
			return mainLockInvalid("baseline utility is missing")
		}
	}
	return nil
}
func validateConfigs(xs []BuildConfig, mats map[string]Material) error {
	if len(xs) == 0 {
		return mainLockInvalid("configs are empty")
	}
	last := ""
	for _, x := range xs {
		if !safeID(x.ID) || x.ID <= last || !consumed(mats, x.MaterialID) || !safeRelative(x.Path) || !lowerHex(x.SHA256, 64) || !validMainLockString(x.Purpose) {
			return mainLockInvalid("config is invalid")
		}
		last = x.ID
	}
	return nil
}
func validatePolicies(xs []Policy, mats map[string]Material) error {
	if len(xs) != 6 {
		return mainLockInvalid("policy cardinality is invalid")
	}
	want := map[string]bool{"source-set": false, "compile-link": false, "elf-dependency": false, "generated-input": false, "intermediate-path": false, "upstream-fork-delta": false}
	last := ""
	for _, x := range xs {
		m, ok := mats[x.MaterialID]
		if !safeID(x.ID) || x.ID <= last || !ok || m.Role != "consumed-build-input" || m.MaterialFile == nil || x.Path != m.MaterialFile.Path || x.SHA256 != m.MaterialFile.SHA256 || !lowerHex(x.SHA256, 64) {
			return mainLockInvalid("policy material is invalid")
		}
		v, ok := want[x.Kind]
		if !ok || v {
			return mainLockInvalid("policy kind is invalid")
		}
		want[x.Kind] = true
		last = x.ID
	}
	for _, v := range want {
		if !v {
			return mainLockInvalid("policy kind is missing")
		}
	}
	return nil
}
func validateLicenses(xs []License, mats map[string]Material) error {
	if len(xs) == 0 {
		return mainLockInvalid("licenses are empty")
	}
	by := map[string]map[string]bool{}
	last := ""
	for _, x := range xs {
		m, ok := mats[x.MaterialID]
		if !safeID(x.ID) || x.ID <= last || !ok {
			return mainLockInvalid("license identity is invalid")
		}
		if !validSPDX(x.SPDXExpression) || !validLicenseLocator(x.NoticeLocator) || !validLicenseLocator(x.CorrespondingSourceLocator) || !oneOf(x.RedistributionStatus, "redistributable", "redistributable-with-corresponding-source", "local-use-only", "review-required") || !containsString(m.LicenseIDs, x.ID) {
			return mainLockLicense("license is incomplete")
		}
		if by[x.MaterialID] == nil {
			by[x.MaterialID] = map[string]bool{}
		}
		if by[x.MaterialID][x.ID] {
			return mainLockInvalid("duplicate license")
		}
		by[x.MaterialID][x.ID] = true
		last = x.ID
	}
	for _, m := range mats {
		for _, id := range m.LicenseIDs {
			if !by[m.ID][id] {
				return mainLockLicense("material license backlink is unresolved")
			}
		}
	}
	return nil
}
func validMainLockBytes(raw []byte) bool {
	if len(raw) == 0 || len(raw) > mainLockMaxBytes || !utf8.Valid(raw) || raw[len(raw)-1] != '\n' || bytes.Contains(raw, []byte{0xef, 0xbb, 0xbf}) {
		return false
	}
	for _, b := range raw {
		if b == '\r' || b == 0 || b == 0x7f || (b < 0x20 && b != '\n' && b != '\t') {
			return false
		}
	}
	return true
}
func mainLockInvalid(d string) error { return failure(CodeLockSchemaInvalid, "main-lock", d) }
func mainLockMutable(d string) error { return failure(CodeLockMutableIdentity, "main-lock", d) }
func mainLockLicense(d string) error { return failure(CodeLicenseRecordIncomplete, "main-lock", d) }
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func sortedUnique(a []string) bool {
	for i, x := range a {
		if !safeID(x) || (i > 0 && a[i-1] >= x) {
			return false
		}
	}
	return true
}
func uniqueHex(a []string, n int) bool {
	s := map[string]bool{}
	for _, x := range a {
		if !lowerHex(x, n) || s[x] {
			return false
		}
		s[x] = true
	}
	return true
}
func oneOf(s string, vs ...string) bool {
	for _, v := range vs {
		if s == v {
			return true
		}
	}
	return false
}
func priorMaterial(m map[string]Material, id string) bool { _, ok := m[id]; return ok }
func consumed(m map[string]Material, id string) bool {
	x, ok := m[id]
	return ok && x.Role == "consumed-build-input"
}
func safeID(s string) bool { return validMainLockString(s) }
func validMainLockString(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '\u007f' || r == '\ufeff' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func safeKebab(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	lastDash := false
	for _, b := range []byte(s) {
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
			lastDash = false
			continue
		}
		if b == '-' && !lastDash {
			lastDash = true
			continue
		}
		return false
	}
	return true
}
func safeRelative(s string) bool {
	if s == "" || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.Contains(s, "//") {
		return false
	}
	for _, x := range strings.Split(s, "/") {
		if x == "" || x == "." || x == ".." {
			return false
		}
		for _, b := range []byte(x) {
			if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '.' || b == '_' || b == '-' || b == '~') {
				return false
			}
		}
	}
	return true
}

var logicalRoots = []string{"/stage-a0/src", "/stage-a0/build", "/stage-a0/build-utils", "/stage-a0/toolchain", "/stage-a0/sysroot"}

func safeAbs(s string) bool {
	for _, r := range logicalRoots {
		if s == r {
			return true
		}
		if strings.HasPrefix(s, r+"/") && safeRelative(strings.TrimPrefix(s, r+"/")) {
			return true
		}
	}
	return false
}
func logicalBelow(s, r string) bool {
	return strings.HasPrefix(s, r+"/") && safeRelative(strings.TrimPrefix(s, r+"/"))
}
func immutableURL(s string) error {
	if normalizedMainLockHTTPS(s) {
		return nil
	}
	u, e := url.Parse(s)
	if e == nil && (u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Host == "" || u.Port() != "") {
		return mainLockMutable("URL has mutable identity")
	}
	return mainLockInvalid("URL is invalid")
}
func normalizedMainLockHTTPS(s string) bool {
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.Host != strings.ToLower(u.Host) || u.Hostname() != u.Host || !validRegistry(u.Host) || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || strings.Contains(s, "%") || strings.Contains(s, "\\") || u.Path == "" || strings.HasSuffix(u.Path, "/") || !safeURLPath(u.Path) || u.String() != s {
		return false
	}
	return true
}
func safeURLPath(p string) bool { return safeRelative(strings.TrimPrefix(p, "/")) }
func validateOCI(ref, manifest, config, os, arch string) error {
	if !digest(manifest) || !digest(config) || os != "linux" || arch != "amd64" {
		return mainLockInvalid("OCI metadata is invalid")
	}
	at := strings.LastIndexByte(ref, '@')
	if at < 0 || !digest(ref[at+1:]) {
		return mainLockMutable("OCI reference is not digest-only")
	}
	if ref[at+1:] != manifest {
		return mainLockInvalid("OCI reference digest does not match manifest")
	}
	name := ref[:at]
	parts := strings.Split(name, "/")
	if len(parts) < 2 || !validRegistry(parts[0]) {
		return mainLockInvalid("OCI registry is invalid")
	}
	for _, p := range parts[1:] {
		if strings.Contains(p, ":") {
			return mainLockMutable("OCI reference contains a tag")
		}
		if !validOCIComponent(p) {
			return mainLockInvalid("OCI component is invalid")
		}
	}
	return nil
}
func digest(s string) bool {
	return strings.HasPrefix(s, "sha256:") && lowerHex(strings.TrimPrefix(s, "sha256:"), 64)
}
func validRegistry(s string) bool {
	if s == "localhost" {
		return true
	}
	for _, p := range strings.Split(s, ".") {
		if len(p) == 0 || len(p) > 63 || !asciiAlnum(p[0]) || !asciiAlnum(p[len(p)-1]) {
			return false
		}
		for _, b := range []byte(p) {
			if !asciiAlnum(b) && b != '-' {
				return false
			}
		}
	}
	return true
}
func validOCIComponent(s string) bool {
	if s == "" || !asciiAlnum(s[0]) || !asciiAlnum(s[len(s)-1]) {
		return false
	}
	sep := false
	for _, b := range []byte(s) {
		if asciiAlnum(b) {
			sep = false
			continue
		}
		if (b == '.' || b == '_' || b == '-') && !sep {
			sep = true
			continue
		}
		return false
	}
	return true
}
func asciiAlnum(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' }
func validLicenseLocator(s string) bool {
	if safeRelative(s) || normalizedMainLockHTTPS(s) {
		return true
	}
	return false
}

type spdxParser struct {
	s     string
	i     int
	depth int
}

func validSPDX(s string) bool {
	if len(s) == 0 || len(s) > mainLockMaxSPDXBytes {
		return false
	}
	p := &spdxParser{s: s}
	return p.or() && p.i == len(s)
}
func (p *spdxParser) or() bool {
	if !p.and() {
		return false
	}
	for p.take(" OR ") {
		if !p.and() {
			return false
		}
	}
	return true
}
func (p *spdxParser) and() bool {
	if !p.with() {
		return false
	}
	for p.take(" AND ") {
		if !p.with() {
			return false
		}
	}
	return true
}
func (p *spdxParser) with() bool {
	if strings.HasPrefix(p.s[p.i:], "(") {
		return p.primary()
	}
	if !p.licenseID() {
		return false
	}
	if p.take(" WITH ") {
		return p.id()
	}
	return true
}
func (p *spdxParser) primary() bool {
	if p.take("(") {
		p.depth++
		if p.depth > mainLockMaxSPDXDepth {
			return false
		}
		if !p.or() || !p.take(")") {
			return false
		}
		p.depth--
		return true
	}
	return p.licenseID()
}
func (p *spdxParser) licenseID() bool {
	atom := p.atom()
	if atom == "" {
		return false
	}
	if strings.HasPrefix(atom, "LicenseRef-") {
		return refID(strings.TrimPrefix(atom, "LicenseRef-"))
	}
	if strings.HasPrefix(atom, "DocumentRef-") {
		parts := strings.Split(atom, ":LicenseRef-")
		return len(parts) == 2 && refID(strings.TrimPrefix(parts[0], "DocumentRef-")) && refID(parts[1])
	}
	return spdxID(atom)
}
func (p *spdxParser) id() bool {
	return spdxID(p.atom())
}
func (p *spdxParser) atom() string {
	start := p.i
	for p.i < len(p.s) {
		b := p.s[p.i]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '.' || b == '-' || b == ':' {
			p.i++
			continue
		}
		break
	}
	return p.s[start:p.i]
}
func spdxID(s string) bool {
	if s == "" || !asciiAlphaNumAny(s[0]) {
		return false
	}
	for _, b := range []byte(s) {
		if !asciiAlphaNumAny(b) && b != '.' && b != '-' {
			return false
		}
	}
	return true
}
func refID(s string) bool { return spdxID(s) }
func asciiAlphaNumAny(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}
func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
func (p *spdxParser) take(x string) bool {
	if strings.HasPrefix(p.s[p.i:], x) {
		p.i += len(x)
		return true
	}
	return false
}
