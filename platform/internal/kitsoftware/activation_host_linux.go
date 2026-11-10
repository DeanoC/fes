package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast/appliance/software"
)

// ActivationReady checks the host only while the engine accepts a software
// transition. An ordinary host exit still restarts that host alone.
func (c *ProcessController) ActivationReady(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != id || c.prepared == nil || c.prepared.id != id {
		return errors.New("activation host does not belong to the prepared release")
	}
	root, err := c.preparedPath(ctx, id)
	if err != nil {
		return err
	}
	executable := filepath.Join(root, hostExecutable)
	if c.host == nil || c.hostPlan == nil || c.host.role != roleHost || c.host.path != executable || c.hostPlan.path != executable {
		return errors.New("activation host is not the selected supervised executable")
	}
	before, err := processIdentity(c.host)
	if err != nil {
		// A host exit must not make the engine restart the whole group.
		return fmt.Errorf("activation host is unavailable: %v", err)
	}
	file, err := os.Open(filepath.Join(root, "manifest.json"))
	if err != nil {
		return err
	}
	manifest, readErr := software.DecodeManifest(file)
	err = errors.Join(readErr, file.Close())
	if err != nil {
		return err
	}
	revision := manifest.Provenance["fogcast"]
	if revision == "" {
		return errors.New("activation host revision is not recorded in the release")
	}
	if err := probeActivationHost(ctx, http.DefaultClient, "http://127.0.0.1:8787/api/v1/health", revision); err != nil {
		return err
	}
	after, err := processIdentity(c.host)
	if err != nil || !sameProcessIdentity(before, after) {
		return errors.New("activation host identity changed during readiness")
	}
	return nil
}

func probeActivationHost(ctx context.Context, client *http.Client, url, revision string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("activation host returned HTTP %d", response.StatusCode)
	}
	return validateActivationHostHealth(response.Body, revision)
}

func validateActivationHostHealth(reader io.Reader, revision string) error {
	var health struct {
		Host struct {
			Revision string `json:"revision"`
		} `json:"host"`
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 64<<10))
	if err := decoder.Decode(&health); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("activation host health has trailing data")
	}
	if revision == "" || health.Host.Revision != revision {
		return errors.New("activation host revision differs from the selected release")
	}
	// Target readiness can remain false until software maintenance is released.
	// A serving host proves its catalog opened; requiring target readiness here
	// would wait on the operation whose completion this probe decides.
	return nil
}
