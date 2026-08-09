package stagea0

// MainLock is the immutable V1 input to a Stage A0 build.  It deliberately
// contains values only: parser-only TOML presence information is private.
type MainLock struct {
	Format              int
	Schema              string
	FogCastBaseRevision string
	SourceDateEpoch     int64
	Environment         MainLockEnvironment
	Main                MainLockMain
	Build               MainLockBuild
	Materials           []Material
	Toolchains          []Toolchain
	BuildUtilities      []BuildUtility
	Configs             []BuildConfig
	Policies            []Policy
	Licenses            []License
}

type MainLockEnvironment struct {
	Locale, Timezone, Umask, Network, ContainerMaterialID string
	JobCount                                              int
	PathPolicy                                            []string
}
type MainLockMain struct {
	UpstreamMaterialID, ForkMaterialID, UpstreamCommit, UpstreamTree, ForkCommit, ForkTree, ForkParentCommit string
	PatchCommits                                                                                             []string
	PublicationStatus, DurableRetrievalMaterialID                                                            string
}
type MainLockBuild struct {
	Entrypoint                                     []string
	WorkingDirectory, VDateFormat, VDateExpression string
	AllowedFinalArtifacts                          []string
}

type Material struct {
	ID, Role, Kind string
	LicenseIDs     []string
	Purpose        string
	GitLocal       *GitLocalMaterial
	GitHTTPS       *GitHTTPSMaterial
	ArchiveHTTPS   *ArchiveHTTPSMaterial
	OCI            *OCIMaterial
	GitSubtree     *GitSubtreeMaterial
	MaterialFile   *MaterialFile
}
type GitLocalMaterial struct{ RepositoryID, Commit, Tree string }
type GitHTTPSMaterial struct{ RepositoryID, URL, Commit, Tree string }
type ArchiveHTTPSMaterial struct {
	URL    string
	Size   int64
	SHA256 string
}
type OCIMaterial struct{ Reference, ManifestDigest, ConfigDigest, OS, Architecture string }
type GitSubtreeMaterial struct{ ParentID, Path, Tree string }
type MaterialFile struct {
	ParentID, Path string
	Size           int64
	SHA256         string
}

type Toolchain struct {
	ID, TargetTriple string
	Components       []ToolchainComponent
}
type ToolchainComponent struct {
	Role, MaterialID, LogicalPath string
	ExecutableSHA256, Version     *string
}
type BuildUtility struct{ Role, MaterialID, LogicalPath, ExecutableSHA256, Version string }
type BuildConfig struct{ ID, MaterialID, Path, SHA256, Purpose string }
type Policy struct{ ID, MaterialID, Path, SHA256, Kind string }
type License struct{ ID, MaterialID, SPDXExpression, NoticeLocator, CorrespondingSourceLocator, RedistributionStatus string }
