package tenfoot

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var collectionIDSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func TestUniqueCollectionIDAvoidsReservedAndCollisions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		existing []string
		want     string
	}{
		{"Home", nil, "home-list"},
		{"Weekend Queue!", nil, "weekend-queue"},
		{"Weekend Queue!!", []string{"weekend-queue"}, "weekend-queue-2"},
		{"日本語", []string{"collection"}, "collection-2"},
		{"Favorites", nil, "favorites-list"},
		{"Recently Added", nil, "recently-added-list"},
		{"Recently Added", []string{"recently-added-list"}, "recently-added-list-2"},
		{"All", nil, "all-list"},
		{"Continue", nil, "continue-list"},
		{"Unplayed", nil, "unplayed-list"},
	}
	for _, tc := range cases {
		if got := uniqueCollectionID(tc.name, tc.existing); got != tc.want {
			t.Fatalf("uniqueCollectionID(%q, %v) = %q want %q", tc.name, tc.existing, got, tc.want)
		}
	}
}

func TestUniqueCollectionIDTimestampFallbackIsSlugSafe(t *testing.T) {
	t.Parallel()
	existing := []string{"weekend-queue"}
	for n := 2; n < 1000; n++ {
		existing = append(existing, "weekend-queue-"+strconv.Itoa(n))
	}
	got := uniqueCollectionID("Weekend Queue", existing)
	if strings.Contains(got, ".") {
		t.Fatalf("fallback id %q contains a period", got)
	}
	if !strings.HasPrefix(got, "weekend-queue-") {
		t.Fatalf("fallback id %q", got)
	}
	if !collectionIDSlug.MatchString(got) || len(got) > maxCollectionIDLen {
		t.Fatalf("fallback id %q is not a host slug", got)
	}
	longExisting := make([]string, 0, 999)
	longBase := strings.Repeat("a", maxCollectionIDLen)
	longExisting = append(longExisting, collectionIDFromName(longBase))
	for n := 2; n < 1000; n++ {
		suffix := "-" + strconv.Itoa(n)
		keep := maxCollectionIDLen - len(suffix)
		longExisting = append(longExisting, longBase[:keep]+suffix)
	}
	got = uniqueCollectionID(longBase, longExisting)
	if strings.Contains(got, ".") {
		t.Fatalf("long fallback id %q contains a period", got)
	}
	if !collectionIDSlug.MatchString(got) || len(got) > maxCollectionIDLen {
		t.Fatalf("long fallback id %q is not a host slug", got)
	}
}

func TestIsCustomCollectionIDRejectsReserved(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "all", "favorites", "recents", "continue", "unplayed", "recently_added", "recently-added"} {
		if isCustomCollectionID(id) {
			t.Fatalf("%q should not be custom", id)
		}
	}
	if !isCustomCollectionID("weekend-queue") {
		t.Fatal("weekend-queue should be custom")
	}
	if !isCustomCollectionID("home") {
		t.Fatal("existing host collection home should be custom")
	}
	if !isSlugReservedCollectionID("home") {
		t.Fatal("home should stay slug-reserved")
	}
	if IsReservedCollectionID("home") {
		t.Fatal("home is not a host-reserved id")
	}
}

func TestSetGameCollectionsToggle(t *testing.T) {
	t.Parallel()
	game := Game{ID: "snes-mario", Collections: []string{"weekend-queue"}}
	game = setGameCollections(game, "weekend-queue", false)
	if gameHasCollection(game, "weekend-queue") {
		t.Fatalf("removed = %#v", game.Collections)
	}
	game = setGameCollections(game, "weekend-queue", true)
	if !gameHasCollection(game, "weekend-queue") {
		t.Fatalf("added = %#v", game.Collections)
	}
}
