package catalog

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/DeanoC/FogCast-POC/protocol"
)

var ErrInvalidQuery = errors.New("catalog query is invalid")

const (
	DefaultQueryLimit = 100
	MaxQueryLimit     = 200
)

type Sort string

const (
	SortTitle    Sort = "title"
	SortPlatform Sort = "platform"
)

type Query struct {
	Text        string
	Platform    protocol.System
	Collection  string
	Restrict    bool
	RestrictIDs []string
	Sort        Sort
	Cursor      string
	Limit       int
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

type cursorKey struct {
	sort     Sort
	platform string
	title    string
	id       string
}

func normalizeQuery(query Query) (Query, error) {
	query.Text = strings.TrimSpace(query.Text)
	query.Platform = protocol.System(strings.TrimSpace(string(query.Platform)))
	query.Cursor = strings.TrimSpace(query.Cursor)
	switch query.Sort {
	case "", SortTitle:
		query.Sort = SortTitle
	case SortPlatform:
	default:
		return Query{}, fmt.Errorf("%w: unsupported catalog sort %q", ErrInvalidQuery, query.Sort)
	}
	if query.Limit <= 0 {
		query.Limit = DefaultQueryLimit
	}
	if query.Limit > MaxQueryLimit {
		query.Limit = MaxQueryLimit
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
	payload := strings.Join([]string{string(key.sort), key.platform, key.title, key.id}, "\x1f")
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(raw string) (cursorKey, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
	}
	parts := strings.Split(string(decoded), "\x1f")
	if len(parts) != 4 {
		return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
	}
	sort := Sort(parts[0])
	if sort != SortTitle && sort != SortPlatform {
		return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
	}
	if parts[3] == "" {
		return cursorKey{}, fmt.Errorf("catalog cursor is invalid")
	}
	return cursorKey{sort: sort, platform: parts[1], title: parts[2], id: parts[3]}, nil
}

func gameCursor(game Game, sort Sort) string {
	return encodeCursor(cursorKey{
		sort:     sort,
		platform: string(game.System),
		title:    asciiLower(game.Title),
		id:       game.ID,
	})
}

// CursorFor returns an opaque catalog cursor for the last item on a page.
func CursorFor(game Game, sort Sort) string {
	if sort != SortPlatform {
		sort = SortTitle
	}
	return gameCursor(game, sort)
}

// CursorGameID decodes a catalog cursor and returns its game ID.
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
	return strings.Contains(foldSearchText(game.Title), folded) ||
		strings.Contains(foldSearchText(game.ID), folded) ||
		strings.Contains(foldSearchText(string(game.System)), folded)
}

// OrderGames sorts a page in catalog title or platform order.
func OrderGames(games []Game, sort Sort) {
	slices.SortStableFunc(games, func(a, b Game) int {
		if sort == SortPlatform {
			if compared := strings.Compare(string(a.System), string(b.System)); compared != 0 {
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
	return foldSearchText(title) + " " + foldSearchText(id) + " " + foldSearchText(string(system))
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
