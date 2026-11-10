package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/catalogcheckpoint"
)

func TestKitCatalogOfflineCommands(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	if code := kitMain(ctx, []string{"catalog", "schema"}, &stdout, &stderr, nil); code != 0 || stdout.String() != "{\"schema\":22}\n" {
		t.Fatalf("schema: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	root := t.TempDir()
	database := filepath.Join(root, "library.sqlite3")
	snapshot := filepath.Join(root, "snapshot")
	retained := filepath.Join(root, "retained")
	for _, args := range [][]string{
		{"catalog", "inspect", "--database", database},
		{"catalog", "checkpoint", "--database", database, "--snapshot", snapshot},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := kitMain(ctx, args, &stdout, &stderr, nil); code != 0 {
			t.Fatalf("%v code=%d stderr=%q", args, code, stderr.String())
		}
		var result catalogcheckpoint.Metadata
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Present {
			t.Fatalf("absent result %q: %v", stdout.String(), err)
		}
	}
	store, err := catalog.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	stdout.Reset()
	stderr.Reset()
	if code := kitMain(ctx, []string{"catalog", "restore", "--database", database, "--snapshot", snapshot, "--retain", retained, "--expected-current-schema", "22"}, &stdout, &stderr, nil); code != 0 {
		t.Fatalf("restore code=%d stderr=%q", code, stderr.String())
	}
	for _, args := range [][]string{{"catalog"}, {"catalog", "schema", "--database", database}, {"catalog", "inspect"}, {"catalog", "restore", "--database", database}, {"catalog", "inspect", "--database", database, "extra"}} {
		stdout.Reset()
		stderr.Reset()
		if code := kitMain(ctx, args, &stdout, &stderr, nil); code != 2 || stdout.Len() != 0 {
			t.Fatalf("invalid invocation %v: code=%d stdout=%q", args, code, stdout.String())
		}
	}
}
