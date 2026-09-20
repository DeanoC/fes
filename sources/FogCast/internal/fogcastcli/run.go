// Package fogcastcli implements the FogCast host command-line interface.
package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
)

const usageText = "usage: fogcast [--config path] [--api origin] [--json] {scan|games|search <text>|launch <game-id>|favorite <game-id>|unfavorite <game-id>|recents|media-scan|facets-sync|health|status|stop|core-inspect <path>|core-load <path>|core-media <path>|core-install <path>|core-media-install <path>|core-media-capabilities <package-id>|core-media-select <game-id> <expected-package-id> <expected-media-id-or-none> <media-id-or-none>|core-firmware-select <media-id-or-none>|core-list|core-check <package-id>|core-entry <title> <package-id> [<role> <media-id>] [firmware]|core-select <game-id> <expected-package-id> <package-id>|core-settings <game-id>|core-settings-set <game-id> <expected-package-id> <expected-revision> <speed>|core-progress <game-id>}\n       fogcast --version [--json]\n"

const maxPublicGameIDBytes = 128

// Service is the catalog and agent-health surface used by local CLI commands.
// Launch, status and stop use the persistent host session API instead.
type Service interface {
	Scan(context.Context) (catalog.ScanReport, error)
	Games(context.Context) ([]catalog.Game, error)
	Search(context.Context, string) ([]catalog.Game, error)
	Health(context.Context) (protocol.Health, error)
	Close() error
}

// OpenService opens a service using the selected host paths.
type OpenService func(context.Context, fogcast.Paths) (Service, error)

type rootResult struct {
	RootID    string          `json:"root_id"`
	System    protocol.System `json:"system"`
	Added     int             `json:"added"`
	Updated   int             `json:"updated"`
	Unchanged int             `json:"unchanged"`
	Invalid   int             `json:"invalid"`
	Missing   int             `json:"missing"`
	Offline   bool            `json:"offline"`
}

type scanResult struct {
	Roots []rootResult `json:"roots"`
}

type gameResult struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	System        protocol.System     `json:"system"`
	LibraryID     string              `json:"library_id"`
	State         catalog.SourceState `json:"state"`
	RootOnline    bool                `json:"root_online"`
	ContentCached bool                `json:"content_cached"`
}

type gamesResult struct {
	Games []gameResult `json:"games"`
}

type statusResult struct {
	State       protocol.State              `json:"state"`
	GameID      *string                     `json:"game_id,omitempty"`
	System      *protocol.System            `json:"system,omitempty"`
	Core        *string                     `json:"core,omitempty"`
	Error       *commandError               `json:"error,omitempty"`
	CorePackage *protocol.CorePackageStatus `json:"core_package,omitempty"`
}

type coreInspectionResult struct {
	PackageID string               `json:"package_id"`
	Core      corepackage.Core     `json:"core"`
	ABI       corepackage.Contract `json:"abi"`
	Build     corepackage.Build    `json:"build"`
}

type versionResult struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

type commandError struct {
	Code    protocol.ErrorCode `json:"code"`
	Message string             `json:"message"`
	Phase   string             `json:"phase,omitempty"`
}

type errorResult struct {
	Error commandError `json:"error"`
}

type commandResult struct {
	jsonValue any
	human     func(io.Writer) error
	exit      int
	err       error
}

// Run executes one FogCast command and returns its process exit status.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, open OpenService) int {
	flags := flag.NewFlagSet("fogcast", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "host configuration path")
	apiOrigin := flags.String("api", "", "running FogCast host API origin")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	versionOutput := flags.Bool("version", false, "emit version metadata")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, err := io.WriteString(stdout, usageText); err != nil {
				writeHumanError(stderr, safeCommandError(err))
				return 1
			}
			return 0
		}
		_, _ = io.WriteString(stderr, usageText)
		return 2
	}
	commandArgs := flags.Args()
	if *versionOutput {
		if len(commandArgs) != 0 {
			_, _ = io.WriteString(stderr, usageText)
			return 2
		}
		result := versionResult{Version: version.Version, Revision: version.Revision}
		if *jsonOutput {
			if err := json.NewEncoder(stdout).Encode(result); err != nil {
				writeHumanError(stderr, safeCommandError(err))
				return 1
			}
			return 0
		}
		if _, err := fmt.Fprintf(stdout, "fogcast version=%s revision=%s\n", result.Version, result.Revision); err != nil {
			writeHumanError(stderr, safeCommandError(err))
			return 1
		}
		return 0
	}
	if !validCommand(commandArgs) {
		_, _ = io.WriteString(stderr, usageText)
		return 2
	}
	if err := ctx.Err(); err != nil {
		return writeFailure(*jsonOutput, stdout, stderr, err)
	}
	if commandArgs[0] == "core-inspect" {
		return writeResult(*jsonOutput, stdout, stderr, inspectCore(commandArgs[1]))
	}
	if commandArgs[0] == "core-load" || commandArgs[0] == "core-media" {
		origin, err := coreAPIOrigin(*apiOrigin)
		if err != nil {
			return writeFailure(*jsonOutput, stdout, stderr, err)
		}
		if commandArgs[0] == "core-media" {
			return writeResult(*jsonOutput, stdout, stderr, loadMediaThroughHostAPI(ctx, origin, commandArgs[1]))
		}
		return writeResult(*jsonOutput, stdout, stderr, loadCoreThroughHostAPI(ctx, origin, commandArgs[1]))
	}

	if coreLibraryCommand(commandArgs[0]) {
		origin, err := coreAPIOrigin(*apiOrigin)
		if err != nil {
			return writeFailure(*jsonOutput, stdout, stderr, err)
		}
		return writeResult(*jsonOutput, stdout, stderr, runCoreLibraryCommand(ctx, origin, commandArgs))
	}
	if commandArgs[0] == "launch" || commandArgs[0] == "status" || commandArgs[0] == "stop" {
		origin, err := coreAPIOrigin(*apiOrigin)
		if err != nil {
			return writeFailure(*jsonOutput, stdout, stderr, err)
		}
		return writeResult(*jsonOutput, stdout, stderr, runHostSessionCommand(ctx, origin, commandArgs))
	}
	paths, err := fogcast.DefaultPaths()
	if err != nil {
		return writeFailure(*jsonOutput, stdout, stderr, err)
	}
	configSpecified := false
	flags.Visit(func(selected *flag.Flag) {
		configSpecified = configSpecified || selected.Name == "config"
	})
	if configSpecified {
		paths.Config = *configPath
	}
	if open == nil {
		return writeFailure(*jsonOutput, stdout, stderr, errors.New("FogCast service opener is unavailable"))
	}
	service, err := open(ctx, paths)
	if err != nil || service == nil {
		if err == nil {
			err = errors.New("FogCast service opener returned no service")
		}
		return writeFailure(*jsonOutput, stdout, stderr, err)
	}

	result := execute(ctx, commandArgs, service)
	if ctx.Err() != nil {
		result = commandResult{err: ctx.Err(), exit: 1}
	}
	if closeErr := service.Close(); result.err == nil && closeErr != nil {
		result = commandResult{err: closeErr, exit: 1}
	}
	if result.err != nil {
		return writeFailure(*jsonOutput, stdout, stderr, result.err)
	}
	if *jsonOutput {
		if err := json.NewEncoder(stdout).Encode(result.jsonValue); err != nil {
			writeHumanError(stderr, safeCommandError(err))
			return 1
		}
		return result.exit
	}
	if result.human != nil {
		if err := result.human(stdout); err != nil {
			writeHumanError(stderr, safeCommandError(err))
			return 1
		}
	}
	return result.exit
}

func validCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "core-settings-set", "core-media-select":
		return len(args) == 5
	case "core-settings", "core-progress", "core-firmware-select":
		return len(args) == 2
	case "core-entry":
		return len(args) == 3 || len(args) == 4 || len(args) == 5 || len(args) == 6
	case "core-select":
		return len(args) == 4
	case "core-list", "scan", "games", "health", "status", "stop", "recents", "media-scan", "facets-sync":
		return len(args) == 1
	case "core-media-capabilities", "core-media-install", "core-install", "core-check", "search", "launch", "favorite", "unfavorite", "core-inspect", "core-load", "core-media":
		return len(args) == 2
	default:
		return false
	}
}

func execute(ctx context.Context, args []string, service Service) commandResult {
	switch args[0] {
	case "scan":
		report, err := service.Scan(ctx)
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		result := makeScanResult(report)
		return commandResult{jsonValue: result, human: func(output io.Writer) error { return writeHumanScan(output, result) }}
	case "games":
		games, err := service.Games(ctx)
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		result := makeGamesResult(games)
		return commandResult{jsonValue: result, human: func(output io.Writer) error { return writeHumanGames(output, result) }}
	case "search":
		games, err := service.Search(ctx, args[1])
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		result := makeGamesResult(games)
		return commandResult{jsonValue: result, human: func(output io.Writer) error { return writeHumanGames(output, result) }}
	case "favorite", "unfavorite":
		fav, ok := service.(interface {
			SetFavorite(context.Context, string, bool) error
		})
		if !ok {
			return commandResult{err: errors.New("user library is unavailable"), exit: 1}
		}
		if err := fav.SetFavorite(ctx, args[1], args[0] == "favorite"); err != nil {
			return commandResult{err: err, exit: 1}
		}
		return commandResult{jsonValue: map[string]any{"id": args[1], "favorite": args[0] == "favorite"}}
	case "recents":
		lister, ok := service.(interface {
			QueryGames(context.Context, catalog.Query) (catalog.Page, error)
		})
		if !ok {
			return commandResult{err: errors.New("user library is unavailable"), exit: 1}
		}
		page, err := lister.QueryGames(ctx, catalog.Query{Collection: "recents", Limit: 50})
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		result := makeGamesResult(page.Games)
		return commandResult{jsonValue: result, human: func(output io.Writer) error { return writeHumanGames(output, result) }}
	case "media-scan":
		scanner, ok := service.(interface {
			ScanMedia(context.Context) error
		})
		if !ok {
			return commandResult{err: errors.New("library media is unavailable"), exit: 1}
		}
		if err := scanner.ScanMedia(ctx); err != nil {
			return commandResult{err: err, exit: 1}
		}
		return commandResult{jsonValue: map[string]any{"result": "ok"}}
	case "facets-sync":
		syncer, ok := service.(interface {
			SyncFacets(context.Context) (int, error)
		})
		if !ok {
			return commandResult{err: errors.New("catalog facets are unavailable"), exit: 1}
		}
		updated, err := syncer.SyncFacets(ctx)
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		return commandResult{jsonValue: map[string]any{"updated": updated}, human: func(output io.Writer) error {
			_, err := fmt.Fprintf(output, "updated %d\n", updated)
			return err
		}}
	case "health":
		health, err := service.Health(ctx)
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		exit := 0
		if !health.Ready {
			exit = 1
		}
		return commandResult{jsonValue: health, exit: exit, human: func(output io.Writer) error {
			if health.Ready {
				_, err := io.WriteString(output, "ready\n")
				return err
			}
			_, err := io.WriteString(output, "not ready\n")
			return err
		}}
	default:
		return commandResult{err: errors.New("invalid command"), exit: 2}
	}
}

func inspectCore(path string) commandResult {
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}, exit: 1}
	}
	result := coreInspectionResult{PackageID: inspection.PackageID, Core: inspection.Descriptor.Core, ABI: inspection.Descriptor.ABI, Build: inspection.Descriptor.Build}
	return commandResult{jsonValue: result, human: func(output io.Writer) error {
		_, err := fmt.Fprintf(output, "package=%s core=%s version=%s abi=%s/%d.%d build=%s repository=%s revision=%s\n",
			result.PackageID, result.Core.ID, result.Core.Version, result.ABI.ID, result.ABI.Major, result.ABI.Minor,
			result.Build.ID, result.Build.Repository, result.Build.Revision)
		return err
	}}
}

type coreArchiveSnapshot struct {
	inspection corepackage.Inspection
	data       []byte
}

func snapshotCoreArchive(path string) (coreArchiveSnapshot, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() < 1 || before.Size() > corepackage.MaxArchiveSize {
		return coreArchiveSnapshot{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package archive is invalid", Phase: "admission"}
	}
	file, err := os.Open(path)
	if err != nil {
		return coreArchiveSnapshot{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package archive is unavailable", Phase: "admission"}
	}
	defer file.Close()
	return snapshotOpenedCoreArchive(file, before)
}

func snapshotOpenedCoreArchive(file *os.File, before os.FileInfo) (coreArchiveSnapshot, error) {
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return coreArchiveSnapshot{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package archive changed while opening", Phase: "admission"}
	}
	data, err := io.ReadAll(io.LimitReader(file, corepackage.MaxArchiveSize+1))
	if err != nil || int64(len(data)) != opened.Size() || len(data) == 0 || int64(len(data)) > corepackage.MaxArchiveSize {
		return coreArchiveSnapshot{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package archive changed while reading", Phase: "admission"}
	}
	temporary, err := os.CreateTemp("", "fogcast-core-snapshot-*.fcore")
	if err != nil {
		return coreArchiveSnapshot{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "core package snapshot could not be created", Phase: "admission"}
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	closeErr := temporary.Close()
	if err != nil || closeErr != nil {
		return coreArchiveSnapshot{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "core package snapshot could not be written", Phase: "admission"}
	}
	inspection, err := corepackage.InspectPackage(temporaryPath)
	if err != nil {
		return coreArchiveSnapshot{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "core package is invalid", Phase: "admission"}
	}
	return coreArchiveSnapshot{inspection: inspection, data: data}, nil
}

func coreAPIOrigin(flagValue string) (string, error) {
	raw := flagValue
	if raw == "" {
		raw = os.Getenv("FOGCAST_API")
	}
	if raw == "" {
		raw = "http://127.0.0.1:8787"
	}
	return validateCoreAPIOrigin(raw)
}

func validateCoreAPIOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", &protocol.APIError{Code: protocol.CodeBadRequest, Message: "FogCast API origin is invalid", Phase: "request"}
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

type developmentCoreSession struct {
	State       protocol.State              `json:"state"`
	Execution   string                      `json:"execution"`
	CorePackage *protocol.CorePackageStatus `json:"core_package"`
}

func loadCoreThroughHostAPI(ctx context.Context, origin, path string) commandResult {
	snapshot, err := snapshotCoreArchive(path)
	if err != nil {
		return commandResult{err: err, exit: 1}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/v1/session/development-core", bytes.NewReader(snapshot.data))
	if err != nil {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "FogCast API request is invalid", Phase: "request"}, exit: 1}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not accepted")
	}}
	response, err := client.Do(request)
	if err != nil {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable", Phase: "request"}, exit: 1}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "FogCast host API response is unavailable", Phase: "request"}, exit: 1}
	}
	if response.StatusCode != http.StatusOK {
		var envelope struct {
			Error commandError `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return commandResult{err: &protocol.APIError{Code: envelope.Error.Code, Message: envelope.Error.Message, Phase: envelope.Error.Phase}, exit: 1}
		}
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "FogCast host session request failed", Phase: "request"}, exit: 1}
	}
	var session developmentCoreSession
	if json.Unmarshal(body, &session) != nil || session.State != protocol.StateActive || session.Execution != fogcast.ExecutionFPGADevelopment ||
		!corePackageMatchesInspection(session.CorePackage, snapshot.inspection) {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Message: "FogCast host returned an invalid core package session", Phase: "recovery"}, exit: 1}
	}
	result := statusResult{State: session.State, CorePackage: session.CorePackage}
	return commandResult{jsonValue: result, human: func(output io.Writer) error { return writeHumanStatusResult(output, result) }}
}

func corePackageMatchesInspection(status *protocol.CorePackageStatus, inspection corepackage.Inspection) bool {
	descriptor := inspection.Descriptor
	if status == nil || status.PackageID != inspection.PackageID || status.Generation == 0 ||
		status.ABI.ID != descriptor.ABI.ID || int64(status.ABI.Major) != descriptor.ABI.Major ||
		int64(status.ABI.Minor) != descriptor.ABI.Minor || status.BuildID != descriptor.Build.ID {
		return false
	}
	descriptorInterfaces := make(map[string]corepackage.Interface, len(descriptor.Interfaces))
	for _, contract := range descriptor.Interfaces {
		descriptorInterfaces[contract.ID] = contract
	}
	active := make(map[string]bool, len(status.ActiveInterfaces))
	gamepad := false
	for index, contract := range status.ActiveInterfaces {
		declared, ok := descriptorInterfaces[contract.ID]
		if !ok || int64(contract.Major) != declared.Major || int64(contract.Minor) != declared.Minor ||
			(index > 0 && status.ActiveInterfaces[index-1].ID >= contract.ID) {
			return false
		}
		active[contract.ID] = true
		if (contract.ID == "fes.gamepad" || contract.ID == "fes.gamepad.ports") && contract.Major == 1 && contract.Minor == 0 {
			gamepad = true
		}
	}
	for _, contract := range descriptor.Interfaces {
		if contract.Required && !active[contract.ID] {
			return false
		}
	}
	return status.Gamepad == gamepad
}

func writeResult(jsonOutput bool, stdout, stderr io.Writer, result commandResult) int {
	if result.err != nil {
		return writeFailure(jsonOutput, stdout, stderr, result.err)
	}
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(result.jsonValue); err != nil {
			writeHumanError(stderr, safeCommandError(err))
			return 1
		}
	} else if result.human != nil {
		if err := result.human(stdout); err != nil {
			writeHumanError(stderr, safeCommandError(err))
			return 1
		}
	}
	return result.exit
}

func makeScanResult(report catalog.ScanReport) scanResult {
	result := scanResult{Roots: make([]rootResult, 0, len(report.Roots))}
	for _, root := range report.Roots {
		result.Roots = append(result.Roots, rootResult{
			RootID: root.RootID, System: root.System, Added: root.Added, Updated: root.Updated,
			Unchanged: root.Unchanged, Invalid: root.Invalid, Missing: root.Missing, Offline: root.Offline,
		})
	}
	return result
}

func makeGamesResult(games []catalog.Game) gamesResult {
	games = slices.Clone(games)
	slices.SortFunc(games, func(left, right catalog.Game) int {
		if compared := strings.Compare(strings.ToLower(left.Title), strings.ToLower(right.Title)); compared != 0 {
			return compared
		}
		return strings.Compare(left.ID, right.ID)
	})
	result := gamesResult{Games: make([]gameResult, 0, len(games))}
	for _, game := range games {
		result.Games = append(result.Games, gameResult{
			ID: game.ID, Title: game.Title, System: game.System, LibraryID: game.LibraryID,
			State: game.State, RootOnline: game.RootOnline, ContentCached: game.Content != nil,
		})
	}
	return result
}

func writeHumanScan(output io.Writer, result scanResult) error {
	writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	for _, root := range result.Roots {
		availability := "online"
		if root.Offline {
			availability = "offline"
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\tadded=%d\tupdated=%d\tunchanged=%d\tinvalid=%d\tmissing=%d\t%s\n",
			root.RootID, systemLabel(root.System), root.Added, root.Updated, root.Unchanged, root.Invalid, root.Missing, availability); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func writeHumanGames(output io.Writer, result gamesResult) error {
	writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	for _, game := range result.Games {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", game.ID, systemLabel(game.System), game.State, game.Title); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func systemLabel(system protocol.System) string {
	if system == protocol.SystemMegaDrive {
		return "Mega Drive"
	}
	if system == protocol.SystemSNES {
		return "SNES"
	}
	return string(system)
}

func publicCore(value *string) *string {
	if value == nil {
		return nil
	}
	if *value != "MENU" {
		if !core.DefaultRegistry().RecognizesObserved(*value) {
			return nil
		}
	}
	copy := *value
	return &copy
}

func writeHumanStatusResult(output io.Writer, status statusResult) error {
	if status.State == protocol.StateIdle {
		_, err := io.WriteString(output, "idle\n")
		return err
	}
	if _, err := io.WriteString(output, string(status.State)); err != nil {
		return err
	}
	if status.GameID != nil {
		if _, err := fmt.Fprintf(output, ": %s", *status.GameID); err != nil {
			return err
		}
		if status.Core != nil {
			if _, err := fmt.Fprintf(output, " (%s)", *status.Core); err != nil {
				return err
			}
		}
	} else if status.Core != nil {
		if _, err := fmt.Fprintf(output, " core=%s", *status.Core); err != nil {
			return err
		}
	}
	if status.Error != nil {
		if _, err := fmt.Fprintf(output, " error=%s", status.Error.Code); err != nil {
			return err
		}
	}
	if status.CorePackage != nil {
		if _, err := fmt.Fprintf(output, " package=%s abi=%s/%d.%d build=%s generation=%d", status.CorePackage.PackageID,
			status.CorePackage.ABI.ID, status.CorePackage.ABI.Major, status.CorePackage.ABI.Minor, status.CorePackage.BuildID, status.CorePackage.Generation); err != nil {
			return err
		}
	}
	_, err := io.WriteString(output, "\n")
	return err
}

func writeFailure(jsonOutput bool, stdout, stderr io.Writer, err error) int {
	safe := safeCommandError(err)
	if jsonOutput {
		if encodeErr := json.NewEncoder(stdout).Encode(errorResult{Error: safe}); encodeErr != nil {
			writeHumanError(stderr, safeCommandError(encodeErr))
		}
		return 1
	}
	writeHumanError(stderr, safe)
	return 1
}

func safeCommandError(err error) commandError {
	switch {
	case errors.Is(err, context.Canceled):
		return commandError{Code: "CANCELED", Message: "FogCast operation was canceled"}
	case errors.Is(err, context.DeadlineExceeded):
		return commandError{Code: "DEADLINE_EXCEEDED", Message: "FogCast operation timed out"}
	}
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		result := publicAPIError(apiErr.Code)
		result.Phase = apiErr.Phase
		return result
	}
	return commandError{Code: protocol.CodeInternal, Message: "FogCast operation failed internally"}
}

func publicAPIError(code protocol.ErrorCode) commandError {
	messages := map[protocol.ErrorCode]string{
		protocol.CodeBadRequest:           "FogCast request is invalid",
		protocol.CodeUnauthorized:         "target authentication failed",
		protocol.CodeROMNotFound:          "catalog game was not found",
		protocol.CodeBusy:                 "another launch or stop transition is running",
		protocol.CodeStaleRevision:        "core package selection changed; refresh before retrying",
		protocol.CodeUnsupportedSystem:    "game system is unsupported",
		protocol.CodeUnsupportedOperation: "requested operation is unsupported",
		protocol.CodeInvalidROMPath:       "target ROM path is invalid",
		protocol.CodeMiSTerUnavailable:    "MiSTer is unavailable",
		protocol.CodeCoreTimeout:          "core transition timed out",
		protocol.CodeInternal:             "FogCast operation failed internally",
		protocol.CodeUnrecognizedCore:     "active core is unrecognized",
		protocol.CodeSourceUnavailable:    "game source is unavailable",
		protocol.CodeInvalidArchive:       "game archive is invalid",
		protocol.CodeTransferFailed:       "content transfer failed",
		protocol.CodeDigestMismatch:       "content digest does not match its identity",
		protocol.CodeContentNotCached:     "content is not present in the verified target cache",
		protocol.CodeCacheFull:            "target cache has insufficient safe capacity",
	}
	message, ok := messages[code]
	if !ok {
		return commandError{Code: protocol.CodeInternal, Message: messages[protocol.CodeInternal]}
	}
	return commandError{Code: code, Message: message}
}

func writeHumanError(output io.Writer, err commandError) {
	if err.Phase != "" {
		_, _ = fmt.Fprintf(output, "%s[%s]: %s\n", err.Code, err.Phase, err.Message)
		return
	}
	_, _ = fmt.Fprintf(output, "%s: %s\n", err.Code, err.Message)
}
