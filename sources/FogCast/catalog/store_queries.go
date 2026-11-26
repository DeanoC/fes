package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const selectGames = `
	SELECT g.game_id, g.title, g.library_id, g.relative_path, g.reason, g.system,
	       g.source_kind, g.source_state, l.online,
	       g.source_size, g.modified_ns, g.zip_member, g.zip_size, g.zip_crc32, g.zip_entry_count,
	       g.content_sha256, g.content_size, g.content_extension,
	       g.canonical_title, g.region, g.revision, g.dump_flags, g.group_key,
	       g.first_seen_ns, g.genre, g.year, g.search_aliases, 1
	FROM games AS g JOIN libraries AS l ON l.id = g.library_id
	  AND (g.source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = g.game_id))`

// A core-package parent without a core entry is retained solely for saved
// media. It is not a library title until the entry is explicitly recreated.
func visibleGameSQL(alias string) string {
	return "(" + alias + ".source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = " + alias + ".game_id))"
}

const selectGameColumns = `g.game_id, g.title, g.library_id, g.relative_path, g.reason, g.system,
	       g.source_kind, g.source_state, l.online,
	       g.source_size, g.modified_ns, g.zip_member, g.zip_size, g.zip_crc32, g.zip_entry_count,
	       g.content_sha256, g.content_size, g.content_extension,
	       g.canonical_title, g.region, g.revision, g.dump_flags, g.group_key,
	       g.first_seen_ns, g.genre, g.year, g.search_aliases`

func (s *Store) Games(ctx context.Context) ([]Game, error) {
	return s.queryGames(ctx, selectGames+" WHERE NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = g.game_id) ORDER BY lower(g.title), g.game_id")
}

func (s *Store) Search(ctx context.Context, query string) ([]Game, error) {
	foldedQuery := foldSearchText(query)
	if foldedQuery == "" {
		return s.Games(ctx)
	}
	return s.queryGames(ctx, selectGames+` WHERE NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = g.game_id) AND g.search_text LIKE '%' || ? || '%' ESCAPE '\' ORDER BY lower(g.title), g.game_id`, escapeLIKE(foldedQuery))
}

func foldSearchText(value string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(value)))
}

func (s *Store) Game(ctx context.Context, id string) (Game, error) {
	game, err := scanGame(s.db.QueryRowContext(ctx, selectGames+" WHERE g.game_id = ?", id))
	if err != nil {
		return Game{}, fmt.Errorf("read catalog game %q: %w", id, err)
	}
	return game, nil
}

func (s *Store) Platforms(ctx context.Context) ([]PlatformInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.system,
		       COUNT(g.game_id),
		       MAX(l.online)
		FROM libraries AS l
		LEFT JOIN games AS g ON g.library_id = l.id AND NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = g.game_id)
		  AND (g.source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = g.game_id))
		GROUP BY l.system
		ORDER BY lower(l.system)`)
	if err != nil {
		return nil, fmt.Errorf("query catalog platforms: %w", err)
	}
	defer rows.Close()
	platforms := make([]PlatformInfo, 0)
	for rows.Next() {
		var info PlatformInfo
		var online int
		if err := rows.Scan(&info.ID, &info.GameCount, &online); err != nil {
			return nil, fmt.Errorf("scan catalog platform: %w", err)
		}
		info.Label = PlatformLabel(info.ID)
		info.Online = online != 0
		info.Launchable = Launchable(info.ID)
		info.Tags = PlatformTags(info.ID)
		platforms = append(platforms, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog platforms: %w", err)
	}
	return platforms, nil
}

func (s *Store) QueryGames(ctx context.Context, query Query) (Page, error) {
	normalized, err := NormalizeQuery(query)
	if err != nil {
		return Page{}, err
	}
	if normalized.Restrict && len(normalized.RestrictIDs) == 0 {
		return Page{}, nil
	}
	args := make([]any, 0, 24)
	filters, filterArgs := gameFilterSQL(normalized, "g", "l")
	args = append(args, filterArgs...)
	rankSQL, rankArgs := regionCaseSQL(normalized.PreferredRegions, "g.region")
	from := ` FROM games AS g JOIN libraries AS l ON l.id = g.library_id` + filters
	var builder strings.Builder
	if normalized.Grouped {
		builder.WriteString(`SELECT `)
		builder.WriteString(selectGameColumns)
		builder.WriteString(`, ranked.variant_count FROM (SELECT `)
		builder.WriteString(selectGameColumns)
		builder.WriteString(`, ROW_NUMBER() OVER (PARTITION BY g.group_key ORDER BY `)
		builder.WriteString(rankSQL)
		builder.WriteString(`, `)
		builder.WriteString(dumpPenaltySQL("g.dump_flags"))
		builder.WriteString(`, g.revision DESC, g.game_id) AS rn, COUNT(*) OVER (PARTITION BY g.group_key) AS variant_count`)
		builder.WriteString(from)
		builder.WriteString(`) AS ranked JOIN games AS g ON g.game_id = ranked.game_id JOIN libraries AS l ON l.id = g.library_id WHERE ranked.rn = 1`)
		args = append(append([]any{}, rankArgs...), args...)
		if normalized.Cursor != "" {
			cursorSQL, cursorArgs, err := groupedCursorSQL(normalized)
			if err != nil {
				return Page{}, err
			}
			builder.WriteString(cursorSQL)
			args = append(args, cursorArgs...)
		}
		builder.WriteString(groupedOrderSQL(normalized.Sort))
	} else {
		builder.WriteString(`SELECT `)
		builder.WriteString(selectGameColumns)
		builder.WriteString(`, 1`)
		builder.WriteString(from)
		if normalized.Cursor != "" {
			cursorSQL, cursorArgs, err := fileCursorSQL(normalized)
			if err != nil {
				return Page{}, err
			}
			builder.WriteString(cursorSQL)
			args = append(args, cursorArgs...)
		}
		builder.WriteString(fileOrderSQL(normalized.Sort))
	}
	builder.WriteString(" LIMIT ?")
	args = append(args, normalized.Limit+1)
	games, err := s.queryGames(ctx, builder.String(), args...)
	if err != nil {
		return Page{}, err
	}
	page := Page{Games: games}
	if len(page.Games) > normalized.Limit {
		page.Games = page.Games[:normalized.Limit]
		if normalized.Grouped {
			page.NextCursor = gameCursor(page.Games[len(page.Games)-1], normalized.Sort, true)
		} else {
			page.NextCursor = gameCursor(page.Games[len(page.Games)-1], normalized.Sort, false)
		}
	}
	return page, nil
}

func gameFilterSQL(query Query, gameAlias, libraryAlias string) (string, []any) {
	var builder strings.Builder
	builder.WriteString(" WHERE NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = " + gameAlias + ".game_id)")
	builder.WriteString(" AND " + visibleGameSQL(gameAlias))
	args := make([]any, 0, 16)
	if query.Platform != "" {
		builder.WriteString(" AND " + gameAlias + ".system = ?")
		args = append(args, query.Platform)
	}
	if len(query.CoreIDs) > 0 {
		clause, coreArgs := idListSQL("core_id", query.CoreIDs, false)
		builder.WriteString(" AND " + gameAlias + ".game_id IN (SELECT game_id FROM core_entries WHERE 1 = 1")
		builder.WriteString(clause)
		builder.WriteString(")")
		args = append(args, coreArgs...)
	}
	if query.CoreEntryKind != "" {
		// Imported remote titles may not have cached media yet; their origin
		// distinguishes them from an empty machine entry.
		machine := "COALESCE(e.media_id, '') = '' AND NOT EXISTS (SELECT 1 FROM remote_core_entries r WHERE r.game_id = e.game_id)"
		if query.CoreEntryKind == "title" {
			// A disk made on a shelf is not one of the family's games: it
			// is listed on its shelf only.
			machine = "NOT (" + machine + ") AND NOT EXISTS (SELECT 1 FROM shelf_disks d WHERE d.game_id = e.game_id)"
		}
		builder.WriteString(" AND EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = " + gameAlias + ".game_id AND " + machine + ")")
	}
	if query.Region == "other" {
		builder.WriteString(" AND (" + gameAlias + ".region = '' OR " + gameAlias + ".region = 'other')")
	} else if query.Region != "" {
		builder.WriteString(" AND " + gameAlias + ".region = ?")
		args = append(args, query.Region)
	}
	if query.Genre != "" {
		builder.WriteString(" AND " + gameAlias + ".genre = ?")
		args = append(args, query.Genre)
	}
	if query.Year != "" {
		builder.WriteString(" AND " + gameAlias + ".year = ?")
		args = append(args, query.Year)
	}
	if query.HidePrerelease {
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,beta,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,proto,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,sample,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,demo,%'")
	}
	if query.HideHacks {
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,hack,%'")
		builder.WriteString(" AND (',' || " + gameAlias + ".dump_flags || ',') NOT LIKE '%,unl,%'")
	}
	switch query.Availability {
	case AvailabilityReady:
		builder.WriteString(" AND " + gameAlias + ".source_state = ? AND " + libraryAlias + ".online = 1")
		args = append(args, SourceStateAvailable)
	case AvailabilityOffline:
		builder.WriteString(" AND ((" + gameAlias + ".source_state = ?) OR (" + libraryAlias + ".online = 0 AND " + gameAlias + ".source_state = ?))")
		args = append(args, SourceStateMissing, SourceStateAvailable)
	}
	if query.Restrict {
		clause, listArgs := idListSQL(gameAlias+".game_id", query.RestrictIDs, false)
		builder.WriteString(clause)
		args = append(args, listArgs...)
	}
	if len(query.ExcludeIDs) > 0 {
		clause, listArgs := idListSQL(gameAlias+".game_id", query.ExcludeIDs, true)
		builder.WriteString(clause)
		args = append(args, listArgs...)
	}
	if folded := foldSearchText(query.Text); folded != "" {
		like := escapeLIKE(folded)
		seedClause := ""
		if folded == foldSearchText(SeededActRaiserAlias) {
			seedClause = " OR lower(" + gameAlias + ".canonical_title) = 'actraiser'"
		}
		if match := ftsMatchQuery(folded); match != "" {
			builder.WriteString(" AND (" + gameAlias + ".rowid IN (SELECT rowid FROM games_fts WHERE games_fts MATCH ?) OR " + gameAlias + ".search_text LIKE '%' || ? || '%' ESCAPE '\\'" + seedClause + ")")
			args = append(args, match, like)
		} else {
			builder.WriteString(" AND (" + gameAlias + ".search_text LIKE '%' || ? || '%' ESCAPE '\\'" + seedClause + ")")
			args = append(args, like)
		}
	}
	return builder.String(), args
}

func fileOrderSQL(sort Sort) string {
	switch sort {
	case SortPlatform:
		return " ORDER BY g.system, lower(g.title), g.game_id"
	case SortYear:
		return " ORDER BY CASE WHEN g.year = '' THEN 1 ELSE 0 END, g.year DESC, lower(g.title), g.game_id"
	case SortAdded:
		return " ORDER BY g.first_seen_ns DESC, g.game_id"
	default:
		return " ORDER BY lower(g.title), g.game_id"
	}
}

func groupedOrderSQL(sort Sort) string {
	switch sort {
	case SortPlatform:
		return " ORDER BY g.system, lower(g.canonical_title), g.group_key"
	case SortYear:
		return " ORDER BY CASE WHEN g.year = '' THEN 1 ELSE 0 END, g.year DESC, lower(g.canonical_title), g.group_key"
	case SortAdded:
		return " ORDER BY g.first_seen_ns DESC, g.group_key"
	default:
		return " ORDER BY lower(g.canonical_title), g.group_key"
	}
}

func fileCursorSQL(query Query) (string, []any, error) {
	key, err := decodeCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}
	switch query.Sort {
	case SortPlatform:
		return " AND (g.system > ? OR (g.system = ? AND (lower(g.title) > ? OR (lower(g.title) = ? AND g.game_id > ?))))",
			[]any{key.platform, key.platform, key.title, key.title, key.id}, nil
	case SortYear:
		return " AND ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) > (CASE WHEN ? = '' THEN 1 ELSE 0 END) OR ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) = (CASE WHEN ? = '' THEN 1 ELSE 0 END) AND (g.year < ? OR (g.year = ? AND (lower(g.title) > ? OR (lower(g.title) = ? AND g.game_id > ?))))))",
			[]any{key.extra, key.extra, key.extra, key.extra, key.title, key.title, key.id}, nil
	case SortAdded:
		return " AND (g.first_seen_ns < ? OR (g.first_seen_ns = ? AND g.game_id > ?))",
			[]any{key.extra, key.extra, key.id}, nil
	default:
		return " AND (lower(g.title) > ? OR (lower(g.title) = ? AND g.game_id > ?))",
			[]any{key.title, key.title, key.id}, nil
	}
}

func groupedCursorSQL(query Query) (string, []any, error) {
	key, err := decodeCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}
	switch query.Sort {
	case SortPlatform:
		return " AND (g.system > ? OR (g.system = ? AND (lower(g.canonical_title) > ? OR (lower(g.canonical_title) = ? AND g.group_key > ?))))",
			[]any{key.platform, key.platform, key.title, key.title, key.id}, nil
	case SortYear:
		return " AND ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) > (CASE WHEN ? = '' THEN 1 ELSE 0 END) OR ((CASE WHEN g.year = '' THEN 1 ELSE 0 END) = (CASE WHEN ? = '' THEN 1 ELSE 0 END) AND (g.year < ? OR (g.year = ? AND (lower(g.canonical_title) > ? OR (lower(g.canonical_title) = ? AND g.group_key > ?))))))",
			[]any{key.extra, key.extra, key.extra, key.extra, key.title, key.title, key.id}, nil
	case SortAdded:
		return " AND (g.first_seen_ns < ? OR (g.first_seen_ns = ? AND g.group_key > ?))",
			[]any{key.extra, key.extra, key.id}, nil
	default:
		return " AND (lower(g.canonical_title) > ? OR (lower(g.canonical_title) = ? AND g.group_key > ?))",
			[]any{key.title, key.title, key.id}, nil
	}
}

func (s *Store) GamesByIDs(ctx context.Context, ids []string) ([]Game, error) {
	if len(ids) == 0 {
		return []Game{}, nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	games, err := s.queryGames(ctx, selectGames+" WHERE g.game_id IN (SELECT value FROM json_each(?))", string(raw))
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Game, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	ordered := make([]Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := byID[id]; ok {
			ordered = append(ordered, game)
		}
	}
	return ordered, nil
}

func idListSQL(column string, ids []string, exclude bool) (string, []any) {
	if ids == nil {
		ids = []string{}
	}
	raw, _ := json.Marshal(ids)
	op := " IN "
	if exclude {
		op = " NOT IN "
	}
	return " AND " + column + op + "(SELECT value FROM json_each(?))", []any{string(raw)}
}

func (s *Store) queryGames(ctx context.Context, query string, args ...any) ([]Game, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query catalog games: %w", err)
	}
	defer rows.Close()
	games := make([]Game, 0)
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog game: %w", err)
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog games: %w", err)
	}
	return games, nil
}

func (s *Store) GamesInGroup(ctx context.Context, groupKey string, limit int) ([]Game, error) {
	groupKey = strings.TrimSpace(groupKey)
	if groupKey == "" {
		return []Game{}, nil
	}
	query := `SELECT ` + selectGameColumns + `, 1` + `
		FROM games AS g JOIN libraries AS l ON l.id = g.library_id
		WHERE g.group_key = ? AND NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = g.game_id)
		AND (g.source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = g.game_id))
		ORDER BY g.region, g.revision DESC, g.game_id`
	if limit == UnboundedVariantLimit {
		return s.queryGames(ctx, query, groupKey)
	}
	if limit <= 0 || limit > MaxVariantLimit {
		limit = MaxVariantLimit
	}
	return s.queryGames(ctx, query+` LIMIT ?`, groupKey, limit)
}

func (s *Store) SetFacets(ctx context.Context, id, genre, year, aliases string) error {
	if err := protocol.ValidateGameID(id); err != nil {
		return err
	}
	game, err := s.Game(ctx, id)
	if err != nil {
		return err
	}
	genre = strings.TrimSpace(genre)
	year = strings.TrimSpace(year)
	aliases = strings.TrimSpace(aliases)
	if genre == "" {
		genre = game.Genre
	}
	if year == "" {
		year = game.Year
	}
	if aliases == "" {
		aliases = game.SearchAliases
	}
	aliases = SeededSearchAliases(game.CanonicalTitle, game.Title, aliases)
	search := dumpSearchDocument(game.ID, game.Title, game.CanonicalTitle, aliases, game.System)
	_, err = s.db.ExecContext(ctx, `
		UPDATE games SET genre = ?, year = ?, search_aliases = ?, search_text = ?
		WHERE game_id = ?`, genre, year, aliases, search, id)
	if err != nil {
		return fmt.Errorf("update catalog facets for %q: %w", id, err)
	}
	return nil
}

func (s *Store) Facets(ctx context.Context) (FacetValues, error) {
	genres, err := s.distinctColumn(ctx, `SELECT DISTINCT genre FROM games WHERE genre <> '' AND NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = games.game_id) AND (games.source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = games.game_id)) ORDER BY lower(genre) LIMIT 200`)
	if err != nil {
		return FacetValues{}, err
	}
	years, err := s.distinctColumn(ctx, `SELECT DISTINCT year FROM games WHERE year <> '' AND NOT EXISTS (SELECT 1 FROM kit_host_entries k WHERE k.game_id = games.game_id) AND (games.source_kind <> 'core_package' OR EXISTS (SELECT 1 FROM core_entries e WHERE e.game_id = games.game_id)) ORDER BY year DESC LIMIT 64`)
	if err != nil {
		return FacetValues{}, err
	}
	return FacetValues{Genres: genres, Years: years}, nil
}

func (s *Store) distinctColumn(ctx context.Context, query string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query catalog facets: %w", err)
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanGame(row rowScanner) (Game, error) {
	var game Game
	var online int
	var contentSHA256, contentExtension sql.NullString
	var contentSize sql.NullInt64
	if err := row.Scan(
		&game.ID, &game.Title, &game.LibraryID, &game.RelativePath, &game.Reason, &game.System,
		&game.Kind, &game.State, &online,
		&game.Fingerprint.SourceSize, &game.Fingerprint.ModifiedNS, &game.Fingerprint.ZIPMember,
		&game.Fingerprint.ZIPSize, &game.Fingerprint.ZIPCRC32, &game.Fingerprint.ZIPEntryCount,
		&contentSHA256, &contentSize, &contentExtension,
		&game.CanonicalTitle, &game.Region, &game.Revision, &game.DumpFlags, &game.GroupKey,
		&game.FirstSeenNS, &game.Genre, &game.Year, &game.SearchAliases, &game.VariantCount,
	); err != nil {
		return Game{}, err
	}
	if game.VariantCount <= 0 {
		game.VariantCount = 1
	}
	game.RootOnline = online != 0
	validContentFields := 0
	if contentSHA256.Valid {
		validContentFields++
	}
	if contentSize.Valid {
		validContentFields++
	}
	if contentExtension.Valid {
		validContentFields++
	}
	switch validContentFields {
	case 0:
	case 3:
		game.Content = &Content{SHA256: contentSHA256.String, Size: contentSize.Int64, Extension: contentExtension.String}
	default:
		return Game{}, fmt.Errorf("game %q has invalid partial content tuple", game.ID)
	}
	return game, nil
}
