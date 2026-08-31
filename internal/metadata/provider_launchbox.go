package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	launchBoxAttributionLabel = "Data from LaunchBox Games Database"
	launchBoxCoverCacheLimit  = 64
)

// LaunchBoxCatalog is an in-memory index of FPGA-launchable system records from
// official XML.
type LaunchBoxCatalog struct {
	candidates map[protocol.System][]Candidate
	covers     map[string]string
}

type launchBoxXMLGame struct {
	DatabaseID  string `xml:"DatabaseID"`
	Name        string `xml:"Name"`
	Platform    string `xml:"Platform"`
	Overview    string `xml:"Overview"`
	ReleaseYear string `xml:"ReleaseYear"`
	ReleaseDate string `xml:"ReleaseDate"`
	Genres      string `xml:"Genres"`
	Developer   string `xml:"Developer"`
	Publisher   string `xml:"Publisher"`
	MaxPlayers  string `xml:"MaxPlayers"`
}

type launchBoxXMLAlias struct {
	DatabaseID    string `xml:"DatabaseID"`
	AlternateName string `xml:"AlternateName"`
}

type launchBoxXMLImage struct {
	DatabaseID string `xml:"DatabaseID"`
	FileName   string `xml:"FileName"`
	Type       string `xml:"Type"`
}

type launchBoxIndexedGame struct {
	record    launchBoxGameRecord
	aliases   []string
	cover     string
	coverType string
	system    protocol.System
}

type launchBoxCatalogBatch struct {
	games map[string]*launchBoxIndexedGame
}

// LoadLaunchBoxCatalog parses Metadata.xml and keeps only FogCast's
// FPGA-launchable systems.
func LoadLaunchBoxCatalog(reader io.Reader) (*LaunchBoxCatalog, error) {
	batch := &launchBoxCatalogBatch{games: make(map[string]*launchBoxIndexedGame)}
	decoder := xml.NewDecoder(reader)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, newOpError(ErrInvalidResponse, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "Game":
			var game launchBoxXMLGame
			if err := decoder.DecodeElement(&game, &start); err != nil {
				continue
			}
			year := strings.TrimSpace(game.ReleaseYear)
			if year == "" && len(strings.TrimSpace(game.ReleaseDate)) >= 4 {
				year = strings.TrimSpace(game.ReleaseDate)[:4]
			}
			_ = batch.putLaunchBoxRecord(launchBoxRecord{family: "Game", game: launchBoxGameRecord{
				databaseID:  strings.TrimSpace(game.DatabaseID),
				name:        strings.TrimSpace(game.Name),
				platform:    strings.TrimSpace(game.Platform),
				overview:    strings.TrimSpace(game.Overview),
				releaseYear: year,
				genres:      strings.TrimSpace(game.Genres),
				developer:   strings.TrimSpace(game.Developer),
				publisher:   strings.TrimSpace(game.Publisher),
				maxPlayers:  strings.TrimSpace(game.MaxPlayers),
			}})
		case "GameAlternateName":
			var alias launchBoxXMLAlias
			if err := decoder.DecodeElement(&alias, &start); err != nil {
				continue
			}
			_ = batch.putLaunchBoxRecord(launchBoxRecord{family: "GameAlternateName", alias: launchBoxAliasRecord{
				databaseID:    strings.TrimSpace(alias.DatabaseID),
				alternateName: strings.TrimSpace(alias.AlternateName),
			}})
		case "GameImage":
			var image launchBoxXMLImage
			if err := decoder.DecodeElement(&image, &start); err != nil {
				continue
			}
			_ = batch.putLaunchBoxRecord(launchBoxRecord{family: "GameImage", image: launchBoxImageRecord{
				databaseID: strings.TrimSpace(image.DatabaseID),
				fileName:   strings.TrimSpace(image.FileName),
				typeName:   strings.TrimSpace(image.Type),
			}})
		}
	}
	catalog := &LaunchBoxCatalog{
		candidates: map[protocol.System][]Candidate{
			protocol.SystemSNES:         nil,
			protocol.SystemMegaDrive:    nil,
			protocol.SystemNES:          nil,
			protocol.SystemSMS:          nil,
			protocol.SystemGameBoy:      nil,
			protocol.SystemGBA:          nil,
			protocol.SystemPCE:          nil,
			protocol.SystemGameGear:     nil,
			protocol.SystemGameBoyColor: nil,
			protocol.SystemAtari2600:    nil,
			protocol.SystemColecoVision: nil,
			protocol.SystemAtariLynx:    nil,
		},
		covers: make(map[string]string),
	}
	for _, game := range batch.games {
		if game == nil || game.system == "" {
			continue
		}
		candidate := Candidate{
			ProviderID:       game.record.databaseID,
			Name:             game.record.name,
			AlternativeNames: append([]string(nil), game.aliases...),
			PlatformIDs:      []string{string(game.system)},
			Summary:          game.record.overview,
			Studios:          launchBoxStudios(game.record),
			Players:          game.record.maxPlayers,
		}
		if year, err := parseLaunchBoxYear(game.record.releaseYear); err == nil {
			candidate.FirstReleaseYear = year
		}
		if game.record.genres != "" {
			candidate.Genres = strings.Split(game.record.genres, ";")
		}
		if game.cover != "" {
			handle := launchBoxArtworkHandle(game.cover)
			candidate.Artwork = []ArtworkRef{{Role: ArtworkCover, ID: handle}}
			catalog.covers[handle] = game.cover
		}
		catalog.candidates[game.system] = append(catalog.candidates[game.system], candidate)
	}
	return catalog, nil
}

func parseLaunchBoxYear(value string) (int, error) {
	if !validLaunchBoxYear(value) {
		return 0, errLaunchBoxSkip
	}
	year := 0
	for i := 0; i < 4; i++ {
		year = year*10 + int(value[i]-'0')
	}
	return year, nil
}

var errLaunchBoxSkip = errLaunchBox("skip")

type errLaunchBox string

func (err errLaunchBox) Error() string { return string(err) }

func launchBoxStudios(record launchBoxGameRecord) []string {
	if record.developer != "" {
		return []string{record.developer}
	}
	if record.publisher != "" {
		return []string{record.publisher}
	}
	return nil
}

func launchBoxSystem(platform string) (protocol.System, bool) {
	switch strings.TrimSpace(platform) {
	case "Sega Genesis":
		return protocol.SystemMegaDrive, true
	case "Super Nintendo Entertainment System":
		return protocol.SystemSNES, true
	case "Nintendo Entertainment System":
		return protocol.SystemNES, true
	case "Sega Master System":
		return protocol.SystemSMS, true
	case "Nintendo Game Boy":
		return protocol.SystemGameBoy, true
	case "Nintendo Game Boy Advance":
		return protocol.SystemGBA, true
	case "NEC TurboGrafx-16":
		return protocol.SystemPCE, true
	case "Sega Game Gear":
		return protocol.SystemGameGear, true
	case "Nintendo Game Boy Color":
		return protocol.SystemGameBoyColor, true
	case "Atari 2600":
		return protocol.SystemAtari2600, true
	case "ColecoVision":
		return protocol.SystemColecoVision, true
	case "Atari Lynx":
		return protocol.SystemAtariLynx, true
	default:
		return "", false
	}
}

func (b *launchBoxCatalogBatch) putLaunchBoxRecord(record launchBoxRecord) error {
	switch record.family {
	case "Game":
		system, ok := launchBoxSystem(record.game.platform)
		if !ok {
			return nil
		}
		current := b.games[record.game.databaseID]
		if current == nil {
			current = &launchBoxIndexedGame{}
			b.games[record.game.databaseID] = current
		}
		current.record = record.game
		current.system = system
	case "GameAlternateName":
		current := b.ensure(record.alias.databaseID)
		if record.alias.alternateName != "" {
			current.aliases = append(current.aliases, record.alias.alternateName)
		}
	case "GameImage":
		if validLaunchBoxImageTypeRank("cover", record.image.typeName) < 0 {
			return nil
		}
		current := b.ensure(record.image.databaseID)
		if current.cover == "" || validLaunchBoxImageTypeRank("cover", record.image.typeName) < validLaunchBoxImageTypeRank("cover", current.coverType) {
			current.cover = record.image.fileName
			current.coverType = record.image.typeName
		}
	}
	return nil
}

func (b *launchBoxCatalogBatch) ensure(id string) *launchBoxIndexedGame {
	current := b.games[id]
	if current == nil {
		current = &launchBoxIndexedGame{}
		b.games[id] = current
	}
	return current
}

func (b *launchBoxCatalogBatch) beginLaunchBoxBatch() (launchBoxRecordBatch, error) {
	return b, nil
}
func (b *launchBoxCatalogBatch) completeLaunchBoxMember(string) error { return nil }
func (b *launchBoxCatalogBatch) validateLaunchBoxBatch() error        { return nil }
func (b *launchBoxCatalogBatch) flushLaunchBoxBatch() error           { return nil }
func (b *launchBoxCatalogBatch) commitLaunchBoxBatch() error          { return nil }
func (b *launchBoxCatalogBatch) abortLaunchBoxBatch() error           { return nil }

type launchBoxCatalogRuntime struct {
	catalog *LaunchBoxCatalog
	client  *http.Client

	mu     sync.Mutex
	covers map[string]cachedLaunchBoxCover
	cache  string
}

type cachedLaunchBoxCover struct {
	mime string
	body []byte
}

// NewLaunchBoxRuntime serves catalog text and official covers.
func NewLaunchBoxRuntime(catalog *LaunchBoxCatalog, client *http.Client) Runtime {
	return newLaunchBoxCatalogRuntime(catalog, client, "")
}

func newLaunchBoxCatalogRuntime(catalog *LaunchBoxCatalog, client *http.Client, cacheDir string) Runtime {
	if client == nil {
		client = NewHardenedHTTPClient()
	} else {
		copy := *client
		if copy.CheckRedirect == nil {
			copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		}
		copy.Jar = nil
		client = &copy
	}
	return &launchBoxCatalogRuntime{catalog: catalog, client: client, covers: make(map[string]cachedLaunchBoxCover), cache: strings.TrimSpace(cacheDir)}
}

func (r *launchBoxCatalogRuntime) Lookup(_ context.Context, input LookupInput) (Result, error) {
	if r == nil || r.catalog == nil {
		return Result{}, newOpError(ErrUnconfigured, nil)
	}
	candidates := r.catalog.candidates[input.System]
	var decision MatchDecision
	for _, title := range launchBoxLookupTitles(input.Title) {
		next, lookupErr := MatchCandidates(LookupInput{Title: title, System: input.System}, string(input.System), candidates)
		if lookupErr != nil {
			return Result{}, lookupErr
		}
		if next.Outcome == OutcomeExact {
			decision = next
			break
		}
		if decision.Outcome != OutcomeExact && (next.Outcome == OutcomeConfident || decision.Outcome == "") {
			decision = next
		}
	}
	result := Result{
		Outcome: decision.Outcome,
		Attribution: Attribution{
			Provider: ProviderLaunchBox,
			Label:    launchBoxAttributionLabel,
		},
	}
	if decision.Outcome != OutcomeExact && decision.Outcome != OutcomeConfident {
		return result, nil
	}
	result.Presentation = Presentation{
		Summary: decision.Candidate.Summary,
		Genre:   firstNonEmpty(decision.Candidate.Genres),
		Studio:  firstNonEmpty(decision.Candidate.Studios),
		Players: decision.Candidate.Players,
	}
	if decision.Candidate.FirstReleaseYear > 0 {
		result.Presentation.Year = itoaYear(decision.Candidate.FirstReleaseYear)
	}
	for _, art := range decision.Candidate.Artwork {
		if art.Role == ArtworkCover {
			result.Presentation.CoverArtworkID = art.ID
			break
		}
	}
	return result, nil
}

func launchBoxLookupTitles(title string) []string {
	seen := make(map[string]struct{})
	var titles []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		titles = append(titles, value)
	}
	add(title)
	add(stripROMDumpTags(title))
	if index := strings.LastIndex(title, "~"); index >= 0 {
		add(stripROMDumpTags(title[index+1:]))
	}
	for _, current := range append([]string(nil), titles...) {
		add(strings.ReplaceAll(current, " - ", ": "))
		add(strings.ReplaceAll(current, ": ", " - "))
	}
	return titles
}

func stripROMDumpTags(title string) string {
	current := strings.TrimSpace(title)
	for {
		next, ok := stripOneROMDumpTag(current)
		if !ok {
			return current
		}
		current = next
	}
}

func stripOneROMDumpTag(title string) (string, bool) {
	title = strings.TrimSpace(title)
	if len(title) < 3 {
		return title, false
	}
	closeCh := title[len(title)-1]
	var openCh byte
	switch closeCh {
	case ')':
		openCh = '('
	case ']':
		openCh = '['
	default:
		return title, false
	}
	depth := 0
	openIndex := -1
	for index := len(title) - 1; index >= 0; index-- {
		switch title[index] {
		case closeCh:
			depth++
		case openCh:
			depth--
			if depth == 0 {
				openIndex = index
				index = -1
			}
		}
	}
	if openIndex <= 0 {
		return title, false
	}
	inside := strings.ToLower(strings.TrimSpace(title[openIndex+1 : len(title)-1]))
	if !romDumpTag(inside) {
		return title, false
	}
	return strings.TrimSpace(title[:openIndex]), true
}

func romDumpTag(value string) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "usa", "europe", "japan", "world", "asia", "australia", "brazil", "korea", "france", "germany", "spain", "italy", "canada", "beta", "proto", "sample", "demo", "unl", "rev a", "rev b", "rev 0", "rev 1", "rev 2":
			continue
		default:
			if strings.HasPrefix(part, "rev ") && len(part) == 5 {
				continue
			}
			return false
		}
	}
	return true
}

func firstNonEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func itoaYear(value int) string {
	if value < 1000 || value > 9999 {
		return ""
	}
	out := [4]byte{}
	for i := 3; i >= 0; i-- {
		out[i] = byte('0' + value%10)
		value /= 10
	}
	return string(out[:])
}

func (r *launchBoxCatalogRuntime) OpenArtwork(ctx context.Context, handle string) (Artwork, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.catalog == nil {
		return Artwork{}, newOpError(ErrUnconfigured, nil)
	}
	fileName, ok := r.catalog.covers[handle]
	if !ok || !validLaunchBoxImageFileName(fileName) || !launchBoxCoverHandleOK(handle) {
		return Artwork{}, newOpError(ErrPolicyBlocked, nil)
	}
	if art, ok := r.cachedCover(handle); ok {
		return art, nil
	}
	if mime, body, ok := r.readDiskCover(handle); ok {
		r.storeMemoryCover(handle, mime, body)
		return coverArtwork(mime, body), nil
	}

	parsed := url.URL{Scheme: "https", Host: launchBoxImageHost, Path: "/" + fileName}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Artwork{}, newOpError(ErrUpstreamUnavailable, nil)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return Artwork{}, newOpError(ErrUpstreamUnavailable, nil)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Artwork{}, newOpError(ErrUpstreamUnavailable, nil)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil || len(body) < 3 {
		return Artwork{}, newOpError(ErrInvalidResponse, err)
	}
	mime := launchBoxCoverMIME(body)
	if mime == "" {
		return Artwork{}, newOpError(ErrInvalidResponse, nil)
	}
	r.storeMemoryCover(handle, mime, body)
	r.writeDiskCover(handle, mime, body)
	return coverArtwork(mime, body), nil
}

func (r *launchBoxCatalogRuntime) cachedCover(handle string) (Artwork, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cached, ok := r.covers[handle]
	if !ok {
		return Artwork{}, false
	}
	return coverArtwork(cached.mime, cached.body), true
}

func (r *launchBoxCatalogRuntime) storeMemoryCover(handle, mime string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.covers == nil {
		r.covers = make(map[string]cachedLaunchBoxCover)
	}
	if len(r.covers) >= launchBoxCoverCacheLimit {
		for key := range r.covers {
			delete(r.covers, key)
			break
		}
	}
	r.covers[handle] = cachedLaunchBoxCover{mime: mime, body: append([]byte(nil), body...)}
}

func (r *launchBoxCatalogRuntime) coverPath(handle, mime string) string {
	if r == nil || r.cache == "" || !launchBoxCoverHandleOK(handle) {
		return ""
	}
	ext := ".jpg"
	if mime == "image/png" {
		ext = ".png"
	}
	return filepath.Join(r.cache, "launchbox-covers", handle+ext)
}

func (r *launchBoxCatalogRuntime) readDiskCover(handle string) (string, []byte, bool) {
	for _, mime := range []string{"image/jpeg", "image/png"} {
		path := r.coverPath(handle, mime)
		if path == "" {
			return "", nil, false
		}
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if launchBoxCoverMIME(body) != mime {
			continue
		}
		return mime, body, true
	}
	return "", nil, false
}

func (r *launchBoxCatalogRuntime) writeDiskCover(handle, mime string, body []byte) {
	path := r.coverPath(handle, mime)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func coverArtwork(mime string, body []byte) Artwork {
	return Artwork{MIME: mime, Size: int64(len(body)), Reader: io.NopCloser(bytes.NewReader(append([]byte(nil), body...)))}
}

func launchBoxCoverMIME(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte{0xff, 0xd8}):
		return "image/jpeg"
	case bytes.HasPrefix(body, []byte{0x89, 0x50, 0x4e, 0x47}):
		return "image/png"
	default:
		return ""
	}
}

func launchBoxCoverHandleOK(handle string) bool {
	if len(handle) != 64 {
		return false
	}
	for i := 0; i < len(handle); i++ {
		c := handle[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (r *launchBoxCatalogRuntime) Close() error { return nil }

// OpenLaunchBoxArchive loads official Metadata.xml from a local LaunchBox zip.
func OpenLaunchBoxArchive(path string, client *http.Client) (Runtime, error) {
	return OpenLaunchBoxArchiveWithCache(path, client, "")
}

// OpenLaunchBoxArchiveWithCache loads the zip and stores covers under cacheDir.
func OpenLaunchBoxArchiveWithCache(path string, client *http.Client, cacheDir string) (Runtime, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, newOpError(ErrStorage, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, newOpError(ErrStorage, err)
	}
	archive, err := openLaunchBoxArchive(file, info.Size())
	if err != nil {
		return nil, err
	}
	member, err := archive.OpenMember("Metadata.xml")
	if err != nil {
		return nil, err
	}
	defer member.Close()
	catalog, err := LoadLaunchBoxCatalog(member)
	if err != nil {
		return nil, err
	}
	return newLaunchBoxCatalogRuntime(catalog, client, cacheDir), nil
}

func launchBoxArtworkHandle(fileName string) string {
	sum := sha256.Sum256([]byte("launchbox:" + fileName))
	return hex.EncodeToString(sum[:])
}
