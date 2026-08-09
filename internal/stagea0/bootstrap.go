package stagea0

import (
	"bytes"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

type bootstrapWire struct {
	Format              *int             `toml:"format"`
	Schema              *string          `toml:"schema"`
	FogCastBaseRevision *string          `toml:"fogcast_base_revision"`
	MainUpstream        mainUpstreamWire `toml:"main_upstream"`
	Branch              branchWire       `toml:"branch"`
	VDate               vdateWire        `toml:"vdate"`
	Patch               patchWire        `toml:"patch"`
	InitialCommit       commitWire       `toml:"initial_commit"`
}

type mainUpstreamWire struct {
	RepositoryID *string `toml:"repository_id"`
	FetchURL     *string `toml:"fetch_url"`
	Commit       *string `toml:"commit"`
	Tree         *string `toml:"tree"`
}

type branchWire struct {
	Name         *string `toml:"name"`
	ParentCommit *string `toml:"parent_commit"`
}

type vdateWire struct {
	SourcePath           *string `toml:"source_path"`
	SourceEvidenceSHA256 *string `toml:"source_evidence_sha256"`
	OfficialExpression   *string `toml:"official_expression"`
	Format               *string `toml:"format"`
	Timezone             *string `toml:"timezone"`
	ASCIIDigits          *int    `toml:"ascii_digits"`
}

type patchWire struct {
	RecipeVersion *string `toml:"recipe_version"`
}

type commitWire struct {
	AuthorName         *string `toml:"author_name"`
	AuthorEmail        *string `toml:"author_email"`
	CommitterName      *string `toml:"committer_name"`
	CommitterEmail     *string `toml:"committer_email"`
	AuthorTimestamp    *int64  `toml:"author_timestamp"`
	CommitterTimestamp *int64  `toml:"committer_timestamp"`
	CommitMessage      *string `toml:"commit_message"`
	Signing            *bool   `toml:"signing"`
}

func ParseBootstrap(raw []byte) (Bootstrap, error) {
	if !validBootstrapBytes(raw) {
		return Bootstrap{}, failure(CodeBootstrapSchemaInvalid, "bootstrap", "bootstrap must be UTF-8 and LF-only")
	}
	decoder := toml.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire bootstrapWire
	if err := decoder.Decode(&wire); err != nil {
		return Bootstrap{}, failure(CodeBootstrapSchemaInvalid, "bootstrap", "bootstrap TOML is invalid")
	}
	value, ok := wire.bootstrap()
	if !ok {
		return Bootstrap{}, failure(CodeBootstrapSchemaInvalid, "bootstrap", "bootstrap has a missing required field")
	}
	if err := ValidateBootstrap(value); err != nil {
		return Bootstrap{}, err
	}
	return value, nil
}

func (w bootstrapWire) bootstrap() (Bootstrap, bool) {
	if w.Format == nil || w.Schema == nil || w.FogCastBaseRevision == nil ||
		w.MainUpstream.RepositoryID == nil || w.MainUpstream.FetchURL == nil || w.MainUpstream.Commit == nil || w.MainUpstream.Tree == nil ||
		w.Branch.Name == nil || w.Branch.ParentCommit == nil ||
		w.VDate.SourcePath == nil || w.VDate.SourceEvidenceSHA256 == nil || w.VDate.OfficialExpression == nil || w.VDate.Format == nil || w.VDate.Timezone == nil || w.VDate.ASCIIDigits == nil ||
		w.Patch.RecipeVersion == nil ||
		w.InitialCommit.AuthorName == nil || w.InitialCommit.AuthorEmail == nil || w.InitialCommit.CommitterName == nil || w.InitialCommit.CommitterEmail == nil || w.InitialCommit.AuthorTimestamp == nil || w.InitialCommit.CommitterTimestamp == nil || w.InitialCommit.CommitMessage == nil || w.InitialCommit.Signing == nil {
		return Bootstrap{}, false
	}
	return Bootstrap{
		Format: *w.Format, Schema: *w.Schema, FogCastBaseRevision: *w.FogCastBaseRevision,
		MainUpstream:  MainUpstream{RepositoryID: *w.MainUpstream.RepositoryID, FetchURL: *w.MainUpstream.FetchURL, Commit: *w.MainUpstream.Commit, Tree: *w.MainUpstream.Tree},
		Branch:        Branch{Name: *w.Branch.Name, ParentCommit: *w.Branch.ParentCommit},
		VDate:         VDateSpec{SourcePath: *w.VDate.SourcePath, SourceEvidenceSHA256: *w.VDate.SourceEvidenceSHA256, OfficialExpression: *w.VDate.OfficialExpression, Format: *w.VDate.Format, Timezone: *w.VDate.Timezone, ASCIIDigits: *w.VDate.ASCIIDigits},
		Patch:         PatchSpec{RecipeVersion: *w.Patch.RecipeVersion},
		InitialCommit: CommitSpec{AuthorName: *w.InitialCommit.AuthorName, AuthorEmail: *w.InitialCommit.AuthorEmail, CommitterName: *w.InitialCommit.CommitterName, CommitterEmail: *w.InitialCommit.CommitterEmail, AuthorTimestamp: *w.InitialCommit.AuthorTimestamp, CommitterTimestamp: *w.InitialCommit.CommitterTimestamp, CommitMessage: *w.InitialCommit.CommitMessage, Signing: *w.InitialCommit.Signing},
	}, true
}

func ValidateBootstrap(value Bootstrap) error {
	invalid := func(detail string) error { return failure(CodeBootstrapSchemaInvalid, "bootstrap", detail) }
	if value.Format != BootstrapFormatV1 || value.Schema != BootstrapSchemaV1 || !lowerHex(value.FogCastBaseRevision, 40) {
		return invalid("bootstrap format, schema, or FogCast revision is invalid")
	}
	if value.MainUpstream.RepositoryID != "mister-devel-main-mister" || !normalizedHTTPSURL(value.MainUpstream.FetchURL) || !lowerHex(value.MainUpstream.Commit, 40) || !lowerHex(value.MainUpstream.Tree, 40) {
		return invalid("main upstream is invalid")
	}
	if value.Branch.Name != "fogcast/stage-a-baseline" || value.Branch.ParentCommit != value.MainUpstream.Commit {
		return invalid("branch is invalid")
	}
	if !safeSourcePath(value.VDate.SourcePath) || !lowerHex(value.VDate.SourceEvidenceSHA256, 64) ||
		value.VDate.OfficialExpression != VDateExpressionV1 || value.VDate.Format != VDateFormatV1 || value.VDate.Timezone != VDateTimezoneV1 || value.VDate.ASCIIDigits != VDateDigitsV1 {
		return invalid("VDATE evidence is invalid")
	}
	if value.Patch.RecipeVersion != VDateRecipeV1 {
		return invalid("patch recipe is invalid")
	}
	if !safeIdentity(value.InitialCommit.AuthorName) || !safeIdentity(value.InitialCommit.AuthorEmail) || !safeIdentity(value.InitialCommit.CommitterName) || !safeIdentity(value.InitialCommit.CommitterEmail) ||
		value.InitialCommit.AuthorTimestamp < 0 || value.InitialCommit.CommitterTimestamp < 0 || value.InitialCommit.Signing || !validCommitMessage(value.InitialCommit.CommitMessage) {
		return invalid("initial commit metadata is invalid")
	}
	return nil
}

func RenderVDate(timestamp int64) (string, error) {
	value := time.Unix(timestamp, 0).UTC().Format("060102")
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return "", failure(CodeVDateInputInvalid, "vdate", "rendered VDATE is not ASCII digits")
		}
	}
	return value, nil
}

func validBootstrapBytes(raw []byte) bool {
	if !utf8.Valid(raw) || len(raw) == 0 {
		return false
	}
	for _, b := range raw {
		if b == '\r' || (b < 0x20 && b != '\n') || b == 0x7f {
			return false
		}
	}
	return true
}

func lowerHex(value string, width int) bool {
	if len(value) != width {
		return false
	}
	for i := range value {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return true
}

func normalizedHTTPSURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Host != strings.ToLower(u.Host) || u.Hostname() != strings.ToLower(u.Hostname()) || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path == "" || strings.HasSuffix(u.Path, "/") || u.String() != value {
		return false
	}
	return true
}

func safeSourcePath(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." || !safeIdentity(component) {
			return false
		}
	}
	return true
}

func safeIdentity(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validCommitMessage(value string) bool {
	if !utf8.ValidString(value) || !strings.HasSuffix(value, "\n") {
		return false
	}
	for _, r := range value {
		if (r < 0x20 && r != '\n') || r == 0x7f {
			return false
		}
	}
	return true
}
