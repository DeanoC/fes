// Diagnostic loopback host; production modules and routes remain unchanged.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/DeanoC/FogCast/corecatalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/misteross/expansion"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func expectations(catalogPath, sgmPath string) (map[string]any, error) {
	c, err := corecatalog.Load(catalogPath)
	if err != nil {
		return nil, err
	}
	reader, entry, err := c.OpenPackage("fes.coleco")
	if err != nil {
		return nil, err
	}
	archive, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp("", "factory-video-expectations-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	staged, err := corepackage.Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer staged.Cleanup()
	shell, err := corepackage.CanonicalArchive(staged.Directory)
	if err != nil {
		return nil, err
	}
	parts, err := c.ReadVideoParts(context.Background(), entry, shell)
	if err != nil {
		return nil, err
	}
	compositions := map[string]expansion.PartsComposition{}
	sgmCompositions := map[string]expansion.PartsComposition{}
	var sgm expansion.Asset
	if sgmPath != "" {
		encoded, err := os.ReadFile(sgmPath)
		if err != nil {
			return nil, err
		}
		sgm, err = expansion.ReadAsset(bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
	}
	for _, part := range parts {
		composed, err := corepackage.ComposePartsArchive(context.Background(), shell, []expansion.Asset{part.Asset})
		if err != nil {
			return nil, err
		}
		compositions[part.Reference.Profile] = composed.Composition
		if sgmPath != "" {
			linked, err := corepackage.ComposePartsArchive(context.Background(), shell, []expansion.Asset{sgm, part.Asset})
			if err != nil {
				return nil, err
			}
			sgmCompositions[part.Reference.Profile] = linked.Composition
		}
	}
	if len(compositions) != 2 {
		return nil, fmt.Errorf("diagnostic requires the factory Direct/Scanlines pair")
	}
	return map[string]any{"catalog_sha256": c.SHA256, "source_id": c.SourceID, "entry": entry,
		"package_id": staged.PackageID, "build_id": staged.Descriptor.Build.ID, "abi": staged.Descriptor.ABI,
		"compositions": compositions, "sgm_compositions": sgmCompositions, "sgm_id": sgm.ID}, nil
}

func main() {
	state := flag.String("state", "", "root-supplied private state directory with config.toml")
	sgmPath := flag.String("sgm", "", "exact optional SGM expansion archive")
	catalogPath := flag.String("catalog", "", "exact configured factory catalog.json")
	targetID := flag.String("target-id", "", "operator-authorized Kit 2 target ID")
	listen := flag.String("listen", "127.0.0.1:18788", "loopback listener")
	offline := flag.Bool("offline", false, "only verify publication and emit expected composition tuples")
	flag.Parse()
	if *catalogPath == "" {
		panic("missing catalog")
	}
	expected, err := expectations(*catalogPath, *sgmPath)
	if err != nil {
		panic(err)
	}
	if *offline {
		if err := json.NewEncoder(os.Stdout).Encode(expected); err != nil {
			panic(err)
		}
		return
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || host != "127.0.0.1" {
		panic("diagnostic listener must use 127.0.0.1")
	}
	if *state == "" || !filepath.IsAbs(*state) || *targetID == "" {
		panic("missing private state or authorized target ID")
	}
	paths := fogcast.Paths{Config: filepath.Join(*state, "config.toml"), Index: filepath.Join(*state, "library.sqlite3"),
		CorePackages: filepath.Join(*state, "core-packages"), Staging: filepath.Join(*state, "staging"), MetadataRoot: filepath.Join(*state, "metadata"),
		UserLibrary: filepath.Join(*state, "library-user.sqlite3"), LibrarySettings: filepath.Join(*state, "library-settings.json"),
		MediaIndex: filepath.Join(*state, "library-media.sqlite3"), MediaCache: filepath.Join(*state, "library-media")}
	service, err := fogcast.Open(context.Background(), paths, nil)
	if err != nil {
		panic(err)
	}
	defer service.Close()
	selected := service.SelectedTargetConfig()
	if selected.TargetID != *targetID {
		panic("configured target differs from operator-authorized target")
	}
	handler := hostapi.New(service)
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, value any, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(503)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "diagnostic observation unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}
	mux.HandleFunc("GET /diagnostic/expectations", func(w http.ResponseWriter, r *http.Request) { write(w, expected, nil) })
	mux.HandleFunc("GET /diagnostic/health", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Health(fogcast.WithPairedTarget(r.Context(), *targetID))
		if err == nil && value.TargetID != *targetID {
			err = fmt.Errorf("target identity mismatch")
		}
		write(w, value, err)
	})
	mux.HandleFunc("GET /diagnostic/status", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Status(fogcast.WithPairedTarget(r.Context(), *targetID))
		write(w, value, err)
	})
	mux.HandleFunc("GET /diagnostic/lease", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.PairedTargetKitLeaseStatus(r.Context(), *targetID)
		write(w, value, err)
	})
	mux.Handle("/", handler)
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	fmt.Println("Factory-video diagnostic host ready on", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
