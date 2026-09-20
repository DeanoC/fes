package libraryuser

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	maxEditionQueryLen    = 200
	maxEditionPlatformLen = 64
)

// EditionPreference is the household choice for an ambiguous room match
// (rooms-experience §4 Needs a choice).
type EditionPreference struct {
	Query    string
	Platform string
	GameID   string
	ChosenAt int64
}

// CanonicalEditionQuery lowercases, strips punctuation, and collapses spaces
// so "Super Mario Bros." and "super mario bros" share a preference key.
func CanonicalEditionQuery(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	b := make([]rune, 0, len(s))
	lastSpace := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, r)
			lastSpace = false
		case r == ' ' || r == '\t':
			if !lastSpace && len(b) > 0 {
				b = append(b, ' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(string(b))
}

// CanonicalEditionPlatform lowercases a room destination platform id.
func CanonicalEditionPlatform(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// EditionKey is the durable household key for one query+platform match set.
func EditionKey(query, platform string) string {
	q := CanonicalEditionQuery(query)
	if q == "" {
		return ""
	}
	return q + "|" + CanonicalEditionPlatform(platform)
}

func (s *Store) SetEditionPreference(ctx context.Context, query, platform, gameID string) (EditionPreference, error) {
	pref, err := validateEditionPreference(query, platform, gameID)
	if err != nil {
		return EditionPreference{}, err
	}
	key := EditionKey(pref.Query, pref.Platform)
	now := time.Now().UnixNano()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO edition_preference(match_key, query, platform, game_id, chosen_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(match_key) DO UPDATE SET
		  query = excluded.query,
		  platform = excluded.platform,
		  game_id = excluded.game_id,
		  chosen_at = excluded.chosen_at`,
		key, pref.Query, pref.Platform, pref.GameID, now,
	)
	if err != nil {
		return EditionPreference{}, fmt.Errorf("write edition preference: %w", err)
	}
	pref.ChosenAt = now
	return pref, nil
}

func (s *Store) EditionPreference(ctx context.Context, query, platform string) (EditionPreference, error) {
	key := EditionKey(query, platform)
	if key == "" {
		return EditionPreference{}, fmt.Errorf("%w: edition query is required", ErrInvalid)
	}
	var pref EditionPreference
	err := s.db.QueryRowContext(ctx, `
		SELECT query, platform, game_id, chosen_at
		FROM edition_preference WHERE match_key = ?`, key,
	).Scan(&pref.Query, &pref.Platform, &pref.GameID, &pref.ChosenAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return EditionPreference{}, ErrNotFound
		}
		return EditionPreference{}, fmt.Errorf("read edition preference: %w", err)
	}
	return pref, nil
}

func (s *Store) EditionPreferences(ctx context.Context) ([]EditionPreference, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT query, platform, game_id, chosen_at
		FROM edition_preference
		ORDER BY chosen_at DESC, match_key`)
	if err != nil {
		return nil, fmt.Errorf("list edition preferences: %w", err)
	}
	defer rows.Close()
	out := make([]EditionPreference, 0)
	for rows.Next() {
		var pref EditionPreference
		if err := rows.Scan(&pref.Query, &pref.Platform, &pref.GameID, &pref.ChosenAt); err != nil {
			return nil, err
		}
		out = append(out, pref)
	}
	return out, rows.Err()
}

func validateEditionPreference(query, platform, gameID string) (EditionPreference, error) {
	rawQuery := strings.TrimSpace(query)
	if rawQuery == "" || !utf8.ValidString(rawQuery) {
		return EditionPreference{}, fmt.Errorf("%w: edition query is required", ErrInvalid)
	}
	if utf8.RuneCountInString(rawQuery) > maxEditionQueryLen {
		return EditionPreference{}, fmt.Errorf("%w: edition query is too long", ErrInvalid)
	}
	canonicalQuery := CanonicalEditionQuery(rawQuery)
	if canonicalQuery == "" {
		return EditionPreference{}, fmt.Errorf("%w: edition query is required", ErrInvalid)
	}
	canonicalPlatform := CanonicalEditionPlatform(platform)
	if utf8.RuneCountInString(canonicalPlatform) > maxEditionPlatformLen {
		return EditionPreference{}, fmt.Errorf("%w: edition platform is too long", ErrInvalid)
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return EditionPreference{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return EditionPreference{
		Query:    canonicalQuery,
		Platform: canonicalPlatform,
		GameID:   gameID,
	}, nil
}
