package catalog

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/DeanoC/FogCast/protocol"
)

var ErrInvalidQuery = errors.New("catalog query is invalid")

const (
	DefaultQueryLimit     = 100
	MaxQueryLimit         = 200
	MaxVariantLimit       = 50
	UnboundedVariantLimit = -1
)

type Sort string

const (
	SortTitle    Sort = "title"
	SortPlatform Sort = "platform"
	SortYear     Sort = "year"
	SortAdded    Sort = "recently_added"
)

const (
	AvailabilityAll     = "all"
	AvailabilityReady   = "ready"
	AvailabilityOffline = "offline"
)

type Query struct {
	Text             string
	Platform         protocol.System
	Collection       string
	Region           string
	Genre            string
	Year             string
	Availability     string
	HidePrerelease   bool
	HideHacks        bool
	Grouped          bool
	Restrict         bool
	RestrictIDs      []string
	ExcludeIDs       []string
	PreferredRegions []string
	Sort             Sort
	Cursor           string
	Limit            int
}

type Page struct {
	Games      []Game
	NextCursor string
}

type PlatformInfo struct {
	ID         protocol.System `json:"id"`
	Label      string          `json:"label"`
	GameCount  int             `json:"game_count"`
	Online     bool            `json:"online"`
	Launchable bool            `json:"launchable"`
}

type FacetValues struct {
	Genres []string `json:"genres"`
	Years  []string `json:"years"`
}

type cursorKey struct {
	sort     Sort
	platform string
	title    string
	id       string
	extra    string
}

func NormalizeQuery(query Query) (Query, error) {
	query.Text = strings.TrimSpace(query.Text)
	query.Platform = protocol.System(strings.TrimSpace(string(query.Platform)))
	rawRegion := strings.TrimSpace(query.Region)
	query.Region = mapDumpRegion(foldDumpToken(rawRegion))
	if query.Region == "" && strings.EqualFold(rawRegion, "other") {
		query.Region = "other"
	}
	query.Genre = strings.TrimSpace(query.Genre)
	query.Year = strings.TrimSpace(query.Year)
	query.Cursor = strings.TrimSpace(query.Cursor)
	switch strings.TrimSpace(strings.ToLower(query.Availability)) {
	case "", AvailabilityAll:
		query.Availability = AvailabilityAll
	case AvailabilityReady, AvailabilityOffline:
		query.Availability = strings.ToLower(query.Availability)
	default:
		return Query{}, fmt.Errorf("%w: unsupported catalog availability %q", ErrInvalidQuery, query.Availability)
	}
	switch query.Sort {
	case "", SortTitle:
		query.Sort = SortTitle
	case SortPlatform, SortYear, SortAdded:
	case "system":
		query.Sort = SortPlatform
	default:
		return Query{}, fmt.Errorf("%w: unsupported catalog sort %q", ErrInvalidQuery, query.Sort)
	}
	if query.Limit <= 0 {
		query.Limit = DefaultQueryLimit
	}
	if query.Limit > MaxQueryLimit {
		query.Limit = MaxQueryLimit
	}
	if len(query.PreferredRegions) == 0 {
		query.PreferredRegions = append([]string(nil), DefaultPreferredRegions...)
	}
	if query.Cursor != "" {
		key, err := decodeCursor(query.Cursor)
		if err != nil {
			return Query{}, fmt.Errorf("%w: %s", ErrInvalidQuery, err.Error())
		}
		if key.sort != query.Sort {
			return Query{}, fmt.Errorf("%w: catalog cursor sort mismatch", ErrInvalidQuery)
		}
	}
	return query, nil
}

func encodeCursor(key cursorKey) string {
	payload, _ := json.Marshal([]string{string(key.sort), key.platform, key.title, key.id, key.extra})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(raw string) (cursorKey, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
	}
	var encoded []string
	if json.Unmarshal(decoded, &encoded) == nil && len(encoded) == 5 {
		return cursorFromParts(encoded)
	}
	payload := string(decoded)
	parts := strings.Split(payload, "\x1e")
	if len(parts) != 5 {
		parts = strings.Split(payload, "\x1f")
		if len(parts) != 4 && len(parts) != 5 {
			return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
		}
	}
	return cursorFromParts(parts)
}

func cursorFromParts(parts []string) (cursorKey, error) {
	sort := Sort(parts[0])
	if sort != SortTitle && sort != SortPlatform && sort != SortYear && sort != SortAdded {
		return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
	}
	if parts[3] == "" {
		return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
	}
	key := cursorKey{sort: sort, platform: parts[1], title: parts[2], id: parts[3]}
	if len(parts) == 5 {
		key.extra = parts[4]
	}
	return key, nil
}

func gameCursor(game Game, sort Sort, grouped bool) string {
	title := asciiLower(game.Title)
	id := game.ID
	if grouped {
		title = asciiLower(game.CanonicalTitle)
		if game.CanonicalTitle == "" {
			title = asciiLower(game.Title)
		}
		id = game.GroupKey
		if id == "" {
			id = game.ID
		}
	}
	extra := ""
	switch sort {
	case SortYear:
		extra = game.Year
	case SortAdded:
		extra = fmt.Sprintf("%d", game.FirstSeenNS)
	}
	return encodeCursor(cursorKey{
		sort:     sort,
		platform: string(game.System),
		title:    title,
		id:       id,
		extra:    extra,
	})
}

// CursorFor returns an opaque catalog cursor for the last item on a page.
func CursorFor(game Game, sort Sort) string {
	switch sort {
	case SortPlatform, SortYear, SortAdded:
	default:
		sort = SortTitle
	}
	return gameCursor(game, sort, false)
}

func CursorForGrouped(game Game, sort Sort) string {
	switch sort {
	case SortPlatform, SortYear, SortAdded:
	default:
		sort = SortTitle
	}
	return gameCursor(game, sort, true)
}

// CursorGameID decodes a catalog cursor and returns its game ID or group key.
func CursorGameID(raw string) (string, error) {
	key, err := decodeCursor(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidQuery, err.Error())
	}
	return key.id, nil
}

// MatchesText reports whether a game belongs in a server-side q filter.
func MatchesText(game Game, text string) bool {
	folded := foldSearchText(strings.TrimSpace(text))
	if folded == "" {
		return true
	}
	if folded == foldSearchText(SeededActRaiserAlias) && ExactActRaiserTitle(game.CanonicalTitle, game.Title) {
		return true
	}
	return strings.Contains(foldSearchText(game.Title), folded) ||
		strings.Contains(foldSearchText(game.CanonicalTitle), folded) ||
		strings.Contains(foldSearchText(game.ID), folded) ||
		strings.Contains(foldSearchText(string(game.System)), folded) ||
		strings.Contains(foldSearchText(game.SearchAliases), folded)
}

func MatchesQueryFilters(game Game, query Query) bool {
	if query.Platform != "" && game.System != query.Platform {
		return false
	}
	if query.Region != "" {
		if query.Region == "other" {
			if game.Region != "" && game.Region != "other" {
				return false
			}
		} else if game.Region != query.Region {
			return false
		}
	}
	if query.Genre != "" && game.Genre != query.Genre {
		return false
	}
	if query.Year != "" && game.Year != query.Year {
		return false
	}
	dump := Dump{Flags: splitStoredFlags(game.DumpFlags)}
	if query.HidePrerelease && dump.HasPrerelease() {
		return false
	}
	if query.HideHacks && dump.HasHack() {
		return false
	}
	switch query.Availability {
	case AvailabilityReady:
		if game.State != SourceStateAvailable || !game.RootOnline {
			return false
		}
	case AvailabilityOffline:
		if game.State == SourceStateMissing {
			break
		}
		if game.State != SourceStateAvailable || game.RootOnline {
			return false
		}
	}
	return MatchesText(game, query.Text)
}

func splitStoredFlags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

// OrderGames sorts a page in catalog title or platform order.
func OrderGames(games []Game, sort Sort) {
	slices.SortStableFunc(games, func(a, b Game) int {
		if sort == SortPlatform {
			if compared := strings.Compare(string(a.System), string(b.System)); compared != 0 {
				return compared
			}
		}
		if sort == SortYear {
			if compared := strings.Compare(b.Year, a.Year); compared != 0 {
				return compared
			}
		}
		if compared := strings.Compare(asciiLower(a.Title), asciiLower(b.Title)); compared != 0 {
			return compared
		}
		return strings.Compare(a.ID, b.ID)
	})
}

func asciiLower(value string) string {
	bytes := []byte(value)
	for index, character := range bytes {
		if character >= 'A' && character <= 'Z' {
			bytes[index] = character + ('a' - 'A')
		}
	}
	return string(bytes)
}

func searchDocument(id, title string, system protocol.System) string {
	dump := ParseDump(title)
	return dumpSearchDocument(id, title, dump.CanonicalTitle, "", system)
}

func escapeLIKE(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func ftsMatchQuery(folded string) string {
	tokens := make([]string, 0)
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		tokens = append(tokens, builder.String()+"*")
		builder.Reset()
	}
	for _, character := range folded {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
			continue
		}
		flush()
	}
	flush()
	return strings.Join(tokens, " ")
}
