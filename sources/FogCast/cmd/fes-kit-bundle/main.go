// fes-kit-bundle produces and inspects immutable kit software archives.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast/appliance/software"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: fes-kit-bundle pack|inspect|verify-directory [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	manifest := flags.String("manifest", "", "release metadata JSON (pack fills files)")
	root := flags.String("root", "", "payload directory containing bin/ and share/")
	output := flags.String("output", "", "output tar path")
	bundle := flags.String("bundle", "", "archive path to inspect")
	base := flags.String("base", "", "base compatibility JSON for verify-directory")
	if e := flags.Parse(args[1:]); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	switch args[0] {
	case "verify-directory":
		if *root == "" || *base == "" {
			return errors.New("verify-directory requires --root and --base")
		}
		r, e := software.VerifyDirectory(context.Background(), *root)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(*base)
		if e != nil {
			return e
		}
		var c software.Compatibility
		if e = json.Unmarshal(b, &c); e != nil {
			return e
		}
		if e = r.Manifest.Compatible(c); e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(r)
	case "pack":
		if *manifest == "" || *root == "" || *output == "" {
			return errors.New("pack requires --manifest, --root and --output")
		}
		f, e := os.Open(*manifest)
		if e != nil {
			return e
		}
		d := json.NewDecoder(io.LimitReader(f, software.MaxManifestSize+1))
		d.DisallowUnknownFields()
		var m software.Manifest
		e = d.Decode(&m)
		if e == nil {
			var extra any
			if d.Decode(&extra) != io.EOF {
				e = errors.New("trailing metadata")
			}
		}
		f.Close()
		if e != nil {
			return e
		}
		m, e = software.PopulateFiles(m, os.DirFS(*root))
		if e != nil {
			return e
		}
		// Publish only a complete validated archive; keep an existing output intact
		// when hashing, reading or writing the replacement fails.
		dst, e := os.CreateTemp(filepath.Dir(*output), ".kit-bundle-")
		if e != nil {
			return e
		}
		tmp := dst.Name()
		defer os.Remove(tmp)
		e = software.WriteBundle(dst, m, os.DirFS(*root))
		if e == nil {
			e = dst.Sync()
		}
		ce := dst.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
		if e = os.Rename(tmp, *output); e != nil {
			return e
		}
		id, _ := m.ID()
		return json.NewEncoder(out).Encode(software.Release{ID: id, Manifest: m})
	case "inspect":
		if *bundle == "" {
			return errors.New("inspect requires --bundle")
		}
		f, e := os.Open(*bundle)
		if e != nil {
			return e
		}
		defer f.Close()
		r, e := software.InspectBundle(f)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(r)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
