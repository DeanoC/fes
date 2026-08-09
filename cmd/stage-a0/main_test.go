package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0"
)

// This catches an argument parser that accepts malformed command syntax.
func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	got := run([]string{"unknown"}, &stdout, &stderr, stagea0.ExecRunner{})
	if got != stagea0.ExitUsage {
		t.Fatalf("run() = %d, want %d", got, stagea0.ExitUsage)
	}
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestExitCodeMapsEveryStableFailure(t *testing.T) {
	for _, tc := range []struct {
		code stagea0.Code
		want int
	}{
		{stagea0.CodeBootstrapSchemaInvalid, stagea0.ExitBootstrap}, {stagea0.CodeVDateRecipeMismatch, stagea0.ExitRecipe}, {stagea0.CodeVDateInputInvalid, stagea0.ExitRecipe}, {stagea0.CodeRepositoryPolicyMismatch, stagea0.ExitRepository}, {stagea0.CodeCommandFailed, stagea0.ExitCommand},
	} {
		if got := exitCode(&stagea0.Failure{Code: tc.code}); got != tc.want {
			t.Fatalf("%s = %d", tc.code, got)
		}
	}
	if got := exitCode(errors.New("wrapped path")); got != stagea0.ExitCommand {
		t.Fatalf("unknown = %d", got)
	}
}

// This catches a CLI that prints a success token before the entire C0 policy
// has succeeded, or allows unrelated flags/order to alter the command syntax.
func TestRunBootstrapVerifySuccessAndExactSyntax(t *testing.T) {
	root, bootstrapPath := cliFixture(t)
	withWorkingDirectory(t, root)
	runner := &cliRunner{results: []stagea0.CommandResult{
		{Stdout: []byte("0123456789abcdef0123456789abcdef01234567\n")},
		{ExitCode: 1},
		{},
	}}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"bootstrap-verify", "--bootstrap", bootstrapPath}, &stdout, &stderr, runner); got != stagea0.ExitOK || stdout.String() != "valid\n" || stderr.String() != "" {
		t.Fatalf("successful bootstrap-verify = exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if len(runner.commands) != 3 {
		t.Fatalf("successful verification commands = %#v", runner.commands)
	}
	for _, args := range [][]string{
		{},
		{"bootstrap-verify"},
		{"bootstrap-verify", "--unknown", bootstrapPath},
		{"bootstrap-verify", "--bootstrap", bootstrapPath, "--destination", "x"},
		{"init-main", "--destination", "x", "--bootstrap", bootstrapPath},
		{"init-main", "--bootstrap", bootstrapPath},
		{"init-main", "--bootstrap", bootstrapPath, "--destination", "x", "--extra"},
	} {
		stdout.Reset()
		stderr.Reset()
		if got := run(args, &stdout, &stderr, stagea0.ExecRunner{}); got != stagea0.ExitUsage || stdout.String() != "" || stderr.String() != usageText {
			t.Fatalf("run(%#v) = exit=%d stdout=%q stderr=%q", args, got, stdout.String(), stderr.String())
		}
	}
}

// This catches error reporting that leaks the filesystem error or maps one of
// the five public failure codes to the wrong process status.
func TestRunUsesStableErrorLinesAndNeverLeaksBootstrapPath(t *testing.T) {
	root, bootstrapPath := cliFixture(t)
	withWorkingDirectory(t, root)
	missing := filepath.Join(root, "secret directory", "missing.toml")
	var stdout, stderr bytes.Buffer
	if got := run([]string{"bootstrap-verify", "--bootstrap", missing}, &stdout, &stderr, stagea0.ExecRunner{}); got != stagea0.ExitBootstrap || stdout.String() != "" || stderr.String() != "BOOTSTRAP_SCHEMA_INVALID: bootstrap file cannot be read\n" || strings.Contains(stderr.String(), root) {
		t.Fatalf("missing bootstrap = exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	for _, tc := range []struct {
		code stagea0.Code
		want int
	}{
		{stagea0.CodeBootstrapSchemaInvalid, stagea0.ExitBootstrap},
		{stagea0.CodeVDateRecipeMismatch, stagea0.ExitRecipe},
		{stagea0.CodeVDateInputInvalid, stagea0.ExitRecipe},
		{stagea0.CodeRepositoryPolicyMismatch, stagea0.ExitRepository},
		{stagea0.CodeCommandFailed, stagea0.ExitCommand},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			detail := "safe diagnostic"
			if got := report(&stderr, &stagea0.Failure{Code: tc.code, Detail: detail, Err: fmt.Errorf("wrapped %s", root)}); got != tc.want || stdout.String() != "" || stderr.String() != string(tc.code)+": "+detail+"\n" || strings.Contains(stderr.String(), root) {
				t.Fatalf("report(%s) = exit=%d stdout=%q stderr=%q", tc.code, got, stdout.String(), stderr.String())
			}
		})
	}
	// Keep a real valid bootstrap path in this test: an accidental reordered
	// parser that reads the file before syntax validation would otherwise pass.
	if _, err := os.Stat(bootstrapPath); err != nil {
		t.Fatal(err)
	}
}

// This exercises the command dispatcher end to end for every non-usage
// process status.  It catches a path that reports a correct Failure code but
// fails to turn it into the stable CLI line and exit status.
func TestRunMapsRepositoryRecipeAndCommandFailures(t *testing.T) {
	root, bootstrapPath := cliFixture(t)
	withWorkingDirectory(t, root)
	for _, tc := range []struct {
		name, wantLine string
		wantExit       int
		runner         stagea0.Runner
		bootstrap      string
	}{
		{
			name: "repository", wantExit: stagea0.ExitRepository, wantLine: "REPOSITORY_POLICY_MISMATCH: FogCast C0 revision does not match\n",
			runner: &cliRunner{results: []stagea0.CommandResult{{Stdout: []byte(strings.Repeat("f", 40) + "\n")}}}, bootstrap: bootstrapPath,
		},
		{
			name: "command", wantExit: stagea0.ExitCommand, wantLine: "COMMAND_FAILED: Git command failed\n",
			runner: &cliRunner{results: []stagea0.CommandResult{
				{Stdout: []byte("0123456789abcdef0123456789abcdef01234567\n")}, {ExitCode: 1}, {}, {ExitCode: 1},
			}}, bootstrap: bootstrapPath,
		},
		{
			name: "recipe", wantExit: stagea0.ExitRecipe, wantLine: "VDATE_RECIPE_MISMATCH: date command token is not unique\n",
			runner: &cliRecipeFailureRunner{}, bootstrap: cliRecipeFailureBootstrap(t, root),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := run([]string{"init-main", "--bootstrap", tc.bootstrap, "--destination", filepath.Join(root, tc.name+"-destination")}, &stdout, &stderr, tc.runner)
			if got != tc.wantExit || stdout.String() != "" || stderr.String() != tc.wantLine {
				t.Fatalf("run() = exit=%d stdout=%q stderr=%q", got, stdout.String(), stderr.String())
			}
		})
	}
}

const usageText = "usage: stage-a0 bootstrap-verify --bootstrap FILE | init-main --bootstrap FILE --destination DIR\n"

type cliRunner struct {
	commands []stagea0.Command
	results  []stagea0.CommandResult
}

type cliRecipeFailureRunner struct{}

func (r *cliRecipeFailureRunner) Run(_ context.Context, command stagea0.Command) (stagea0.CommandResult, error) {
	switch strings.Join(command.Args, "\x00") {
	case "rev-parse\x00--show-object-format":
		return stagea0.CommandResult{Stdout: []byte("sha1\n")}, nil
	case "hash-object\x00-w\x00-t\x00tree\x00--stdin":
		return stagea0.CommandResult{Stdout: []byte("4b825dc642cb6eb9a060e54bf8d69288fbee4904\n")}, nil
	case "cat-file\x00-e\x004b825dc642cb6eb9a060e54bf8d69288fbee4904^{tree}":
		return stagea0.CommandResult{}, nil
	case "cat-file\x00-t\x004b825dc642cb6eb9a060e54bf8d69288fbee4904":
		return stagea0.CommandResult{Stdout: []byte("tree\n")}, nil
	case "ls-tree\x00-z\x004b825dc642cb6eb9a060e54bf8d69288fbee4904":
		return stagea0.CommandResult{}, nil
	case "rev-parse\x00HEAD^{commit}":
		return stagea0.CommandResult{Stdout: []byte("0123456789abcdef0123456789abcdef01234567\n")}, nil
	case "symbolic-ref\x00-q\x00HEAD":
		return stagea0.CommandResult{ExitCode: 1}, nil
	case "status\x00--porcelain=v1\x00--untracked-files=all":
		return stagea0.CommandResult{}, nil
	}
	if len(command.Args) >= 2 && command.Args[0] == "rev-parse" && strings.HasSuffix(command.Args[1], "^{tree}") {
		return stagea0.CommandResult{Stdout: []byte("fedcba9876543210fedcba9876543210fedcba98\n")}, nil
	}
	if reflect.DeepEqual(command.Args, []string{"ls-tree", "fedcba9876543210fedcba9876543210fedcba98", "--", "Makefile"}) {
		return stagea0.CommandResult{Stdout: []byte("100644 blob 1111111111111111111111111111111111111111\tMakefile\n")}, nil
	}
	if reflect.DeepEqual(command.Args, []string{"cat-file", "blob", "1111111111111111111111111111111111111111"}) {
		return stagea0.CommandResult{Stdout: []byte("missing Recipe V1 token\n")}, nil
	}
	return stagea0.CommandResult{}, nil
}

func (r *cliRunner) Run(_ context.Context, command stagea0.Command) (stagea0.CommandResult, error) {
	r.commands = append(r.commands, command)
	if len(r.results) == 0 {
		return stagea0.CommandResult{}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

func cliFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]\nrepositoryformatversion = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bootstrap.toml")
	if err := os.WriteFile(path, validBootstrapText(), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, path
}

func withWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func validBootstrapText() []byte {
	return []byte(`format = 1
schema = "fogcast.stage-a0-bootstrap"
fogcast_base_revision = "0123456789abcdef0123456789abcdef01234567"

[main_upstream]
repository_id = "mister-devel-main-mister"
fetch_url = "https://github.com/MiSTer-devel/Main_MiSTer.git"
commit = "89abcdef0123456789abcdef0123456789abcdef"
tree = "fedcba9876543210fedcba9876543210fedcba98"

[branch]
name = "fogcast/stage-a-baseline"
parent_commit = "89abcdef0123456789abcdef0123456789abcdef"

[vdate]
source_path = "Makefile"
source_evidence_sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
official_expression = "%y%m%d"
format = "YYMMDD"
timezone = "UTC"
ascii_digits = 6

[patch]
recipe_version = "vdate-recipe-v1"

[initial_commit]
author_name = "FogCast"
author_email = "fogcast@example.invalid"
committer_name = "FogCast"
committer_email = "fogcast@example.invalid"
author_timestamp = 0
committer_timestamp = 1722470400
commit_message = "stage-a0: make VDATE reproducible\n"
signing = false
`)
}

func cliRecipeFailureBootstrap(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "recipe-failure.toml")
	bad := []byte("missing Recipe V1 token\n")
	digest := sha256.Sum256(bad)
	raw := strings.Replace(string(validBootstrapText()), strings.Repeat("a", 64), fmt.Sprintf("%x", digest), 1)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
