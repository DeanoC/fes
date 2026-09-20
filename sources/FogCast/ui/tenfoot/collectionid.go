package tenfoot

import (
	"github.com/DeanoC/FogCast/hostclient"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxCollectionIDLen   = 64
	maxCollectionNameLen = 80
)

var (
	collectionSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)
	// Host-reserved ids (libraryuser.ValidateCollectionID). Existing GET
	// collections with other slugs, including web's "home" slug-avoidance
	// extra, remain custom shelves.
	reservedCollectionIDs = map[string]struct{}{
		"all":            {},
		"favorites":      {},
		"recents":        {},
		"continue":       {},
		"unplayed":       {},
		"recently_added": {},
		"recently-added": {},
	}
	// uniqueCollectionID also avoids "home" so a new shelf does not collide
	// with the browser home nav id. That does not make an existing host
	// collection named home non-custom.
	slugReservedCollectionIDs = map[string]struct{}{
		"home": {},
	}
)

// IsReservedCollectionID reports host-reserved smart-rail slugs that sofa
// create/rename/delete must not use.
func IsReservedCollectionID(id string) bool {
	_, reserved := reservedCollectionIDs[strings.TrimSpace(id)]
	return reserved
}

func isSlugReservedCollectionID(id string) bool {
	id = strings.TrimSpace(id)
	if IsReservedCollectionID(id) {
		return true
	}
	_, reserved := slugReservedCollectionIDs[id]
	return reserved
}

func isCustomCollectionID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || IsReservedCollectionID(id) {
		return false
	}
	for _, view := range smartLibraryViews {
		if view.ID == id {
			return false
		}
	}
	return true
}

func collectionIDFromName(name string) string {
	id := strings.ToLower(strings.TrimSpace(name))
	id = collectionSlugPattern.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if len(id) > maxCollectionIDLen {
		id = id[:maxCollectionIDLen]
		id = strings.Trim(id, "-")
	}
	if id == "" {
		id = "collection"
	}
	if isSlugReservedCollectionID(id) {
		id = (id + "-list")
		if len(id) > maxCollectionIDLen {
			id = id[:maxCollectionIDLen]
		}
	}
	return id
}

func uniqueCollectionID(name string, existing []string) string {
	used := make(map[string]struct{}, len(existing))
	for _, id := range existing {
		id = strings.TrimSpace(id)
		if id != "" {
			used[id] = struct{}{}
		}
	}
	base := collectionIDFromName(name)
	if _, ok := used[base]; !ok {
		return base
	}
	for n := 2; n < 1000; n++ {
		suffix := "-" + strconv.Itoa(n)
		keep := maxCollectionIDLen - len(suffix)
		if keep < 1 {
			keep = 1
		}
		if keep > len(base) {
			keep = len(base)
		}
		id := base[:keep] + suffix
		if _, ok := used[id]; ok {
			continue
		}
		if isSlugReservedCollectionID(id) {
			continue
		}
		return id
	}
	stamp := strings.ToLower(time.Now().UTC().Format("20060102150405"))
	suffix := "-" + stamp
	keep := maxCollectionIDLen - len(suffix)
	if keep < 1 {
		keep = 1
	}
	id := base
	if len(id) > keep {
		id = strings.Trim(id[:keep], "-")
	}
	if id == "" {
		id = "collection"
		if len(id) > keep {
			id = id[:keep]
		}
	}
	return id + suffix
}

func gameHasCollection(game hostclient.Game, collectionID string) bool {
	for _, id := range game.Collections {
		if id == collectionID {
			return true
		}
	}
	return false
}

func setGameCollections(game hostclient.Game, collectionID string, member bool) hostclient.Game {
	has := gameHasCollection(game, collectionID)
	if member && has {
		return game
	}
	if !member && !has {
		return game
	}
	next := make([]string, 0, len(game.Collections)+1)
	for _, id := range game.Collections {
		if id == collectionID {
			continue
		}
		next = append(next, id)
	}
	if member {
		next = append(next, collectionID)
	}
	game.Collections = next
	return game
}
