package stagea0

import (
	"context"
	"fmt"
)

type Code string

const (
	CodeBootstrapSchemaInvalid   Code = "BOOTSTRAP_SCHEMA_INVALID"
	CodeVDateRecipeMismatch      Code = "VDATE_RECIPE_MISMATCH"
	CodeVDateInputInvalid        Code = "VDATE_INPUT_INVALID"
	CodeRepositoryPolicyMismatch Code = "REPOSITORY_POLICY_MISMATCH"
	CodeCommandFailed            Code = "COMMAND_FAILED"
)

const (
	ExitOK         = 0
	ExitUsage      = 2
	ExitBootstrap  = 3
	ExitRecipe     = 4
	ExitRepository = 5
	ExitCommand    = 6
)

const (
	BootstrapFormatV1       = 1
	BootstrapSchemaV1       = "fogcast.stage-a0-bootstrap"
	VDateRecipeV1           = "vdate-recipe-v1"
	VDateExpressionV1       = "%y%m%d"
	VDateFormatV1           = "YYMMDD"
	VDateTimezoneV1         = "UTC"
	VDateDigitsV1           = 6
	DisabledPushURL         = "disabled://stage-a0/upstream"
	RecipeV1ValidationBlock = "ifneq ($(origin VDATE),command line)\n$(error VDATE must be supplied as six ASCII YYMMDD digits)\nendif\noverride stage_a0_shell_quote = '$(subst ','\"'\"',$(1))'\noverride STAGE_A0_VDATE := $(value VDATE)\noverride VDATE_VALID := $(shell LC_ALL=C; export LC_ALL; VDATE=$(call stage_a0_shell_quote,$(STAGE_A0_VDATE)); export VDATE; [[ \"$$VDATE\" =~ ^[0-9]{6}$$ ]] && printf valid)\nifneq ($(VDATE_VALID),valid)\n$(error VDATE must be supplied as six ASCII YYMMDD digits)\nendif\noverride VDATE := $(STAGE_A0_VDATE)\n"
	RecipeV1DateValue       = "$(VDATE)"
)

type Failure struct {
	Code   Code
	Op     string
	Detail string
	Err    error
}

func (f *Failure) Error() string {
	return fmt.Sprintf("%s: %s", f.Code, f.Detail)
}

func (f *Failure) Unwrap() error { return f.Err }

type Bootstrap struct {
	Format              int          `toml:"format"`
	Schema              string       `toml:"schema"`
	FogCastBaseRevision string       `toml:"fogcast_base_revision"`
	MainUpstream        MainUpstream `toml:"main_upstream"`
	Branch              Branch       `toml:"branch"`
	VDate               VDateSpec    `toml:"vdate"`
	Patch               PatchSpec    `toml:"patch"`
	InitialCommit       CommitSpec   `toml:"initial_commit"`
}

type MainUpstream struct {
	RepositoryID string `toml:"repository_id"`
	FetchURL     string `toml:"fetch_url"`
	Commit       string `toml:"commit"`
	Tree         string `toml:"tree"`
}

type Branch struct {
	Name         string `toml:"name"`
	ParentCommit string `toml:"parent_commit"`
}

type VDateSpec struct {
	SourcePath           string `toml:"source_path"`
	SourceEvidenceSHA256 string `toml:"source_evidence_sha256"`
	OfficialExpression   string `toml:"official_expression"`
	Format               string `toml:"format"`
	Timezone             string `toml:"timezone"`
	ASCIIDigits          int    `toml:"ascii_digits"`
}

type PatchSpec struct {
	RecipeVersion string `toml:"recipe_version"`
}

type CommitSpec struct {
	AuthorName         string `toml:"author_name"`
	AuthorEmail        string `toml:"author_email"`
	CommitterName      string `toml:"committer_name"`
	CommitterEmail     string `toml:"committer_email"`
	AuthorTimestamp    int64  `toml:"author_timestamp"`
	CommitterTimestamp int64  `toml:"committer_timestamp"`
	CommitMessage      string `toml:"commit_message"`
	Signing            bool   `toml:"signing"`
}

type Command struct {
	Path  string
	Args  []string
	Env   []string
	Dir   string
	Stdin []byte
}

type CommandResult struct {
	Stdout, Stderr []byte
	ExitCode       int
}

type Runner interface {
	Run(ctx context.Context, command Command) (CommandResult, error)
}

type ExecRunner struct{}

type InitRequest struct {
	Bootstrap   Bootstrap
	Destination string
	FogCastRoot string
}

type ForkIdentity struct {
	UpstreamCommit string
	UpstreamTree   string
	Branch         string
	PatchCommit    string
	PatchTree      string
}

func failure(code Code, op, detail string) error {
	return &Failure{Code: code, Op: op, Detail: detail}
}
