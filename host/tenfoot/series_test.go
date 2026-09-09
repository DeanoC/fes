package tenfoot

import "testing"

func TestSeriesMatesHideWhenAlone(t *testing.T) {
	t.Parallel()
	catalog := []Game{
		{ID: "sonic", Title: "Sonic the Hedgehog", System: "megadrive"},
		{ID: "streets", Title: "Streets of Rage", System: "megadrive"},
	}
	p := Presentation{Presentation: &PresentationInfo{Series: "Sonic the Hedgehog"}}
	mates, label := SeriesMates(catalog, catalog[0], p)
	if len(mates) != 0 || label != "" {
		t.Fatalf("alone mates=%v label=%q", mates, label)
	}
}

func TestSeriesMatesFilterCatalogBySeriesString(t *testing.T) {
	t.Parallel()
	catalog := []Game{
		{ID: "sonic1", Title: "Sonic the Hedgehog", System: "megadrive"},
		{ID: "sonic2", Title: "Sonic the Hedgehog 2", System: "megadrive"},
		{ID: "sonic3", Title: "Sonic the Hedgehog 3", System: "snes"},
		{ID: "spinball", Title: "Sonic Spinball", System: "megadrive"},
		{ID: "streets", Title: "Streets of Rage", System: "megadrive"},
	}
	p := Presentation{Presentation: &PresentationInfo{Series: "Sonic the Hedgehog"}}
	mates, label := SeriesMates(catalog, catalog[0], p)
	if label != "Sonic the Hedgehog" {
		t.Fatalf("label %q", label)
	}
	if len(mates) != 2 || mates[0].ID != "sonic2" || mates[1].ID != "sonic3" {
		t.Fatalf("mates %v", idsOf(mates))
	}
}

func TestSeriesMatesUseRelatedIDsAndCollection(t *testing.T) {
	t.Parallel()
	catalog := []Game{
		{ID: "zelda", Title: "Zelda", System: "snes", Collections: []string{"weekend-queue"}},
		{ID: "link", Title: "Link's Awakening", System: "gb"},
		{ID: "mario", Title: "Mario", System: "snes", Collections: []string{"weekend-queue"}},
		{ID: "pong", Title: "Pong", System: "pong"},
	}
	p := Presentation{Presentation: &PresentationInfo{
		Related:    []string{"link"},
		Collection: "weekend-queue",
	}}
	mates, label := SeriesMates(catalog, catalog[0], p)
	if label != "Related" {
		t.Fatalf("label %q", label)
	}
	if len(mates) != 2 || mates[0].ID != "link" || mates[1].ID != "mario" {
		t.Fatalf("mates %v", idsOf(mates))
	}
}

func TestSeriesMatesIgnoreReservedCollectionAndSelf(t *testing.T) {
	t.Parallel()
	catalog := []Game{
		{ID: "mario", Title: "Mario", Collections: []string{"favorites"}},
		{ID: "sonic", Title: "Sonic", Collections: []string{"favorites"}},
	}
	p := Presentation{Presentation: &PresentationInfo{Collection: "favorites"}}
	mates, label := SeriesMates(catalog, catalog[0], p)
	if len(mates) != 0 || label != "" {
		t.Fatalf("reserved mates=%v label=%q", mates, label)
	}
}

func TestSeriesMatesCapAndGameSeriesField(t *testing.T) {
	t.Parallel()
	catalog := []Game{{ID: "ff1", Title: "Final Fantasy", Series: "Final Fantasy"}}
	for i := 2; i <= 10; i++ {
		catalog = append(catalog, Game{ID: "ff" + itoa(i), Title: "Final Fantasy " + itoa(i), Series: "Final Fantasy"})
	}
	p := Presentation{Presentation: &PresentationInfo{Series: "Final Fantasy"}}
	mates, _ := SeriesMates(catalog, catalog[0], p)
	if len(mates) != seriesMax {
		t.Fatalf("cap %d", len(mates))
	}
}

func TestTitleMatchesSeriesPrefix(t *testing.T) {
	t.Parallel()
	if !titleMatchesSeries("Sonic the Hedgehog 2", "Sonic the Hedgehog") {
		t.Fatal("numeric sequel")
	}
	if !titleMatchesSeries("Final Fantasy VII", "Final Fantasy") {
		t.Fatal("letter sequel")
	}
	if titleMatchesSeries("Sonic Spinball", "Sonic the Hedgehog") {
		t.Fatal("different title")
	}
	if titleMatchesSeries("Sonic", "Son") {
		t.Fatal("short prefix")
	}
}

func idsOf(games []Game) []string {
	out := make([]string, len(games))
	for i, game := range games {
		out[i] = game.ID
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
