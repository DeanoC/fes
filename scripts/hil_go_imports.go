// Report whether changed Go files can change the import graph. This helper
// uses only the Go standard library and never builds or executes PR code.
package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
)

type pair struct {
	Path   string  `json:"path"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

func signature(name, source string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	sig := []string{"package:" + f.Name.Name}
	usesC := false
	for _, imp := range f.Imports {
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		sig = append(sig, "import:"+alias+":"+imp.Path.Value)
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		usesC = usesC || path == "C"
	}
	for _, group := range f.Comments {
		for _, comment := range group.List {
			// Build constraints affect the selected files. C preambles may
			// change compilation and are conservatively treated as graph input.
			if usesC || strings.HasPrefix(comment.Text, "//go:build") || strings.HasPrefix(comment.Text, "// +build") {
				sig = append(sig, "comment:"+comment.Text)
			}
		}
	}
	// Embed/link directives can follow the import block. Scan lexical comments
	// throughout the file so string literals containing these words do not
	// masquerade as directives.
	set := token.NewFileSet()
	file := set.AddFile(name, set.Base(), len(source))
	var s scanner.Scanner
	s.Init(file, []byte(source), nil, scanner.ScanComments)
	for {
		_, kind, text := s.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.COMMENT && (strings.HasPrefix(text, "//go:embed") || strings.HasPrefix(text, "//go:linkname")) {
			sig = append(sig, "directive:"+text)
		}
	}
	if s.ErrorCount != 0 {
		return nil, fmt.Errorf("invalid Go source %s", name)
	}
	sort.Strings(sig)
	return sig, nil
}

func main() {
	var pairs []pair
	if err := json.NewDecoder(os.Stdin).Decode(&pairs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	changed := []string{}
	for _, p := range pairs {
		if p.Before == nil || p.After == nil {
			changed = append(changed, p.Path)
			continue
		}
		a, err := signature(p.Path, *p.Before)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		b, err := signature(p.Path, *p.After)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if strings.Join(a, "\x00") != strings.Join(b, "\x00") {
			changed = append(changed, p.Path)
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(changed); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
