// f3live: live pc386 floppy insert and eject through FogCast's target client
// (F3 #2101 kit smoke). The client applies the host's own status checks, so a
// result the host would refuse fails here too.
//
//	F3_ADDR=http://kit:port F3_TOKEN=<agent token> F3_LEASE=<kit lease> f3live insert UNIT IMAGE GAME [wp]
//	F3_ADDR=... F3_TOKEN=... F3_LEASE=... f3live eject UNIT
//
// It binds the kit's active package generation, prints the returned unit
// states and persistence mode as one JSON line, and exits 1 on any error.
// F3_LEASE is the kit lease the smoke driver holds; the agent guards these
// routes with it. Build: copy adopt_lease_shim.go.txt into
// sources/FogCast/targetclient as a .go file and this file into a scratch
// package there, go build, then remove both.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "f3live: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 3 {
		fail("usage: f3live insert UNIT IMAGE GAME [wp] | eject UNIT")
	}
	base, err := url.Parse(os.Getenv("F3_ADDR"))
	if err != nil || base.Host == "" || os.Getenv("F3_TOKEN") == "" || os.Getenv("F3_LEASE") == "" {
		fail("F3_ADDR, F3_TOKEN and F3_LEASE are required")
	}
	client := targetclient.F3AdoptLease(targetclient.NewClient(base, os.Getenv("F3_TOKEN"), nil), os.Getenv("F3_LEASE"))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil || status.CorePackage == nil {
		fail("status: %v", err)
	}
	unit, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fail("unit: %v", err)
	}
	b := protocol.MediaUnitBinding{PackageID: status.CorePackage.PackageID, Generation: status.CorePackage.Generation, Unit: uint8(unit)}
	switch os.Args[1] {
	case "insert":
		if len(os.Args) < 5 {
			fail("insert needs UNIT IMAGE GAME")
		}
		data, readErr := os.ReadFile(os.Args[3])
		if readErr != nil {
			fail("image: %v", readErr)
		}
		sum := sha256.Sum256(data)
		library := protocol.LibraryMediaBinding{MediaUnitBinding: b, GameID: os.Args[4], BaseMediaID: hex.EncodeToString(sum[:]),
			WriteProtected: len(os.Args) > 5 && os.Args[5] == "wp"}
		status, err = client.InsertLibraryMedia(ctx, int64(len(data)), bytes.NewReader(data), library)
	case "eject":
		status, err = client.EjectMedia(ctx, b)
	default:
		fail("unknown operation %q", os.Args[1])
	}
	if err != nil || status.CorePackage == nil {
		fail("%s: %v", os.Args[1], err)
	}
	type unitOut struct {
		Unit        uint8                     `json:"unit"`
		Interface   string                    `json:"interface"`
		State       string                    `json:"state"`
		Persistence *protocol.MediaDataStatus `json:"persistence,omitempty"`
	}
	out := struct {
		Op              string    `json:"op"`
		PersistenceMode string    `json:"persistence_mode"`
		Units           []unitOut `json:"units"`
	}{Op: os.Args[1], PersistenceMode: status.CorePackage.PersistenceMode}
	for _, u := range status.CorePackage.MediaUnits {
		out.Units = append(out.Units, unitOut{u.Unit, u.Interface.ID, u.State, u.Persistence})
	}
	line, _ := json.Marshal(out)
	fmt.Println(string(line))
}
