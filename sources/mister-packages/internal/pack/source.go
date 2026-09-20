package pack

import (
	"fmt"
	"io"
	"path"
	"strings"
)

func (s *CoreSourceFile) Validate() error {
	if !validIdentifier(s.ID) {
		return fmt.Errorf("invalid core_source identifier %q", s.ID)
	}
	if err := validGitURL(s.Repository); err != nil {
		return fmt.Errorf("repository: %w", err)
	}
	if err := validGitCommit(s.Commit); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if err := validTreePath(s.RBFPath); err != nil {
		return fmt.Errorf("rbf_path: %w", err)
	}
	if !strings.HasSuffix(strings.ToLower(s.RBFPath), ".rbf") {
		return fmt.Errorf("rbf_path %q does not end in .rbf", s.RBFPath)
	}
	if err := validSHA256(s.RBFSHA256); err != nil {
		return err
	}
	if s.RBFSize == 0 {
		return fmt.Errorf("rbf_size is required")
	}
	if err := validTreePath(s.Project); err != nil {
		return fmt.Errorf("project: %w", err)
	}
	if !strings.HasSuffix(strings.ToLower(s.Project), ".qpf") {
		return fmt.Errorf("project %q does not end in .qpf", s.Project)
	}
	seen := map[string]bool{}
	for i, pin := range s.Submodules {
		if err := validTreePath(pin.Path); err != nil {
			return fmt.Errorf("submodules[%d].path: %w", i, err)
		}
		if seen[pin.Path] {
			return fmt.Errorf("duplicate submodule path %q", pin.Path)
		}
		seen[pin.Path] = true
		if err := validGitURL(pin.Repository); err != nil {
			return fmt.Errorf("submodules[%d].repository: %w", i, err)
		}
		if err := validGitCommit(pin.Commit); err != nil {
			return fmt.Errorf("submodules[%d].commit: %w", i, err)
		}
	}
	return nil
}

func (s *CoreSourceFile) Report(w io.Writer) error {
	fmt.Fprintf(w, "core_source %s\n", s.ID)
	fmt.Fprintf(w, "repository  %s\n", s.Repository)
	fmt.Fprintf(w, "commit      %s\n", s.Commit)
	fmt.Fprintf(w, "rbf_path    %s\n", s.RBFPath)
	fmt.Fprintf(w, "rbf_sha256  %s\n", s.RBFSHA256)
	fmt.Fprintf(w, "rbf_size    %d\n", s.RBFSize)
	fmt.Fprintf(w, "project     %s\n", s.Project)
	return nil
}

func DiffCoreSourceOracle(src *CoreSourceFile, oracle *CoreSourceOracleFile) []string {
	var problems []string
	eq := func(field, got, want string) {
		if got != want {
			problems = append(problems, fmt.Sprintf("%s got %q want %q", field, got, want))
		}
	}
	want := oracle.Pin
	eq("repository", src.Repository, want.Repository)
	eq("commit", src.Commit, want.Commit)
	eq("rbf_path", src.RBFPath, want.RBFPath)
	eq("rbf_sha256", src.RBFSHA256, want.RBFSHA256)
	if src.RBFSize != want.RBFSize {
		problems = append(problems, fmt.Sprintf("rbf_size got %d want %d", src.RBFSize, want.RBFSize))
	}
	eq("project", src.Project, want.Project)
	return problems
}

func validGitURL(value string) error {
	if !strings.HasPrefix(value, "https://") || strings.ContainsAny(value, " \t\n") {
		return fmt.Errorf("invalid git url %q", value)
	}
	return nil
}

func validGitCommit(value string) error {
	if len(value) != 40 {
		return fmt.Errorf("commit %q is not 40 hex characters", value)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("commit %q is not 40 lowercase hex characters", value)
		}
	}
	return nil
}

func validSHA256(value string) error {
	if len(value) != 64 {
		return fmt.Errorf("rbf_sha256 %q is not 64 hex characters", value)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("rbf_sha256 is not lowercase hex")
		}
	}
	return nil
}

func validTreePath(value string) error {
	if value == "" || path.IsAbs(value) || strings.Contains(value, "\\") {
		return fmt.Errorf("invalid path %q", value)
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("invalid path %q", value)
	}
	return nil
}
