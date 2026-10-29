//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

func run() error {
	if _, err := board.InitDevices(); err != nil {
		return err
	}
	disk, err := board.OpenStorage()
	if err != nil {
		return err
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		return err
	}
	data, err := volume.ReadFile("FES/BOOT.JSON")
	if err != nil {
		return err
	}
	var config struct {
		APIURL  string `json:"api_url"`
		APIHost string `json:"api_host"`
	}
	if err = json.Unmarshal(data, &config); err != nil {
		return err
	}
	api, err := url.Parse(config.APIURL)
	if err != nil {
		return err
	}
	address, err := netip.ParseAddr(api.Hostname())
	if err != nil || !address.Is4() || api.Scheme != "http" || api.User != nil || api.Path != "" || api.RawQuery != "" || api.Fragment != "" {
		return fmt.Errorf("network probe requires plain HTTP IPv4 origin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	iface, err := board.OpenNetwork(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("FES_NETWORK_READY board=%s address=%s\n", board.Name, iface.Address())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.APIURL+"/api/v1/games", nil)
	if err != nil {
		return err
	}
	if config.APIHost != "" {
		request.Host = config.APIHost
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 1<<20 || response.StatusCode != 200 {
		return fmt.Errorf("network probe HTTP status=%d bytes=%d", response.StatusCode, len(body))
	}
	var catalog struct {
		Games []struct {
			ID string `json:"id"`
		} `json:"games"`
	}
	if err = json.Unmarshal(body, &catalog); err != nil {
		return err
	}
	ids := make([]string, 0, len(catalog.Games))
	for _, game := range catalog.Games {
		ids = append(ids, game.ID)
	}
	encoded, _ := json.Marshal(ids)
	if err = iface.Err(); err != nil {
		return err
	}
	fmt.Printf("FES_NETWORK_PROBE PASS ids=%s stats=%s\n", encoded, iface.Stats())
	return nil
}
func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
