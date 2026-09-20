package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

const launchBoxSampleXML = `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
	`<Game><DatabaseID>42</DatabaseID><Name>Sonic the Hedgehog</Name>` +
	`<Platform>Sega Genesis</Platform><Overview>Blue hedgehog.</Overview>` +
	`<ReleaseYear>1991</ReleaseYear><Genres>Platform;Action</Genres>` +
	`<Developer>Sonic Team</Developer><Publisher>SEGA</Publisher><MaxPlayers>1</MaxPlayers>` +
	`<Series>Sonic the Hedgehog</Series></Game>` +
	`<GameAlternateName><DatabaseID>42</DatabaseID><AlternateName>Sonic</AlternateName></GameAlternateName>` +
	`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName>` +
	`<Type>Box - Front</Type><CRC32>1</CRC32></GameImage>` +
	`<Game><DatabaseID>7</DatabaseID><Name>Unrelated</Name>` +
	`<Platform>Atari 2600</Platform></Game>` +
	`</LaunchBox>`

func TestLaunchBoxCatalogMatchesGenesisTitleAndIgnoresOtherPlatforms(t *testing.T) {
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(launchBoxSampleXML))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })

	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.Outcome != OutcomeExact || result.Presentation.Summary != "Blue hedgehog." || result.Presentation.Year != "1991" ||
		result.Presentation.Genre != "Platform" || result.Presentation.Studio != "Sonic Team" || result.Presentation.Players != "1" ||
		result.Presentation.Series != "Sonic the Hedgehog" {
		t.Fatalf("presentation = %+v", result.Presentation)
	}
	if result.Attribution.Provider != ProviderLaunchBox || result.Attribution.Label != "Data from LaunchBox Games Database" {
		t.Fatalf("attribution = %+v", result.Attribution)
	}
	wantHandle := launchBoxArtworkHandle("cover_42.jpg")
	if result.Presentation.CoverArtworkID != wantHandle {
		t.Fatalf("cover = %q want %q", result.Presentation.CoverArtworkID, wantHandle)
	}
	if result.Presentation.LogoArtworkID != "" {
		t.Fatalf("unexpected logo %q", result.Presentation.LogoArtworkID)
	}

	unrelated, err := runtime.Lookup(context.Background(), LookupInput{Title: "Unrelated", System: protocol.SystemMegaDrive})
	if err != nil || unrelated.Outcome != OutcomeNoMatch {
		t.Fatalf("unrelated = %+v err=%v", unrelated, err)
	}
}

func TestLaunchBoxCatalogCoversEveryFPGALaunchableSystem(t *testing.T) {
	tests := []struct {
		system   protocol.System
		platform string
	}{
		{protocol.SystemSNES, "Super Nintendo Entertainment System"},
		{protocol.SystemMegaDrive, "Sega Genesis"},
		{protocol.SystemNES, "Nintendo Entertainment System"},
		{protocol.SystemSMS, "Sega Master System"},
		{protocol.SystemGameBoy, "Nintendo Game Boy"},
		{protocol.SystemGBA, "Nintendo Game Boy Advance"},
		{protocol.SystemPCE, "NEC TurboGrafx-16"},
		{protocol.SystemGameGear, "Sega Game Gear"},
		{protocol.SystemGameBoyColor, "Nintendo Game Boy Color"},
		{protocol.SystemAtari2600, "Atari 2600"},
		{protocol.SystemColecoVision, "ColecoVision"},
		{protocol.SystemAtariLynx, "Atari Lynx"},
	}
	var xml strings.Builder
	xml.WriteString(`<?xml version="1.0" standalone="yes"?><LaunchBox>`)
	for index, test := range tests {
		id := itoaYear(1000 + index)
		xml.WriteString(`<Game><DatabaseID>` + id + `</DatabaseID><Name>Shared Title</Name><Platform>` + test.platform + `</Platform></Game>`)
		xml.WriteString(`<GameImage><DatabaseID>` + id + `</DatabaseID><FileName>cover_` + id + `.jpg</FileName><Type>Box - Front</Type></GameImage>`)
	}
	xml.WriteString(`</LaunchBox>`)
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml.String()))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })
	for _, test := range tests {
		t.Run(string(test.system), func(t *testing.T) {
			result, err := runtime.Lookup(context.Background(), LookupInput{Title: "Shared Title", System: test.system})
			if err != nil || result.Outcome != OutcomeExact || result.Presentation.CoverArtworkID == "" {
				t.Fatalf("Lookup = %+v err=%v", result, err)
			}
		})
	}
}

func TestLaunchBoxCoverageArchive(t *testing.T) {
	archive := os.Getenv("FOGCAST_LAUNCHBOX_COVERAGE_ARCHIVE")
	if archive == "" {
		t.Skip("FOGCAST_LAUNCHBOX_COVERAGE_ARCHIVE is not set")
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(hash.Sum(nil)), "fd57f8c83d5c5dea88668a5eeb06368dea9e0b115a011076d29bf9bcf3a5c151"; got != want {
		t.Fatalf("archive SHA-256 = %s want %s", got, want)
	}
	runtimeValue, err := OpenLaunchBoxArchive(archive, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeValue.Close() })
	runtime := runtimeValue.(*launchBoxCatalogRuntime)
	want := map[protocol.System]struct{ candidates, covers int }{
		protocol.SystemSNES:         {2946, 2713},
		protocol.SystemMegaDrive:    {2094, 2072},
		protocol.SystemNES:          {3789, 3276},
		protocol.SystemSMS:          {551, 513},
		protocol.SystemGameBoy:      {1597, 1476},
		protocol.SystemGBA:          {2357, 2228},
		protocol.SystemPCE:          {342, 331},
		protocol.SystemGameGear:     {412, 399},
		protocol.SystemGameBoyColor: {1552, 1401},
		protocol.SystemAtari2600:    {1288, 1200},
		protocol.SystemColecoVision: {493, 479},
		protocol.SystemAtariLynx:    {162, 131},
	}
	if len(runtime.catalog.candidates) != len(want) {
		t.Fatalf("mapped systems = %d want %d", len(runtime.catalog.candidates), len(want))
	}
	for system, counts := range want {
		candidates := runtime.catalog.candidates[system]
		covers := 0
		for _, candidate := range candidates {
			for _, art := range candidate.Artwork {
				if art.Role == ArtworkCover {
					covers++
					break
				}
			}
		}
		if len(candidates) != counts.candidates || covers != counts.covers {
			t.Errorf("%s candidates/covers = %d/%d want %d/%d", system, len(candidates), covers, counts.candidates, counts.covers)
		}
	}
}

func TestLaunchBoxCoverFetchesOfficialJPEGOnce(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits++
		if request.URL.Path != "/cover_42.jpg" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "image/jpeg")
		_, _ = writer.Write(minimalJPEG)
	}))
	t.Cleanup(server.Close)

	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(launchBoxSampleXML))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, &http.Client{Transport: rewriteLaunchBoxHost(server.URL)})
	t.Cleanup(func() { _ = runtime.Close() })

	handle := launchBoxArtworkHandle("cover_42.jpg")
	first, err := runtime.OpenArtwork(context.Background(), handle)
	if err != nil {
		t.Fatalf("OpenArtwork: %v", err)
	}
	body, err := io.ReadAll(first.Reader)
	_ = first.Reader.Close()
	if err != nil || first.MIME != "image/jpeg" || len(body) == 0 {
		t.Fatalf("artwork mime=%q size=%d err=%v", first.MIME, len(body), err)
	}
	second, err := runtime.OpenArtwork(context.Background(), handle)
	if err != nil {
		t.Fatalf("second OpenArtwork: %v", err)
	}
	_ = second.Reader.Close()
	if hits != 1 {
		t.Fatalf("official image fetched %d times", hits)
	}
}

func TestLaunchBoxCoverDiskCacheSurvivesNewRuntime(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits++
		writer.Header().Set("Content-Type", "image/jpeg")
		_, _ = writer.Write(minimalJPEG)
	}))
	t.Cleanup(server.Close)
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(launchBoxSampleXML))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	dir := t.TempDir()
	client := &http.Client{Transport: rewriteLaunchBoxHost(server.URL)}
	first := newLaunchBoxCatalogRuntime(catalog, client, dir)
	t.Cleanup(func() { _ = first.Close() })
	handle := launchBoxArtworkHandle("cover_42.jpg")
	art, err := first.OpenArtwork(context.Background(), handle)
	if err != nil {
		t.Fatalf("first OpenArtwork: %v", err)
	}
	_ = art.Reader.Close()
	second := newLaunchBoxCatalogRuntime(catalog, client, dir)
	t.Cleanup(func() { _ = second.Close() })
	art, err = second.OpenArtwork(context.Background(), handle)
	if err != nil {
		t.Fatalf("second OpenArtwork: %v", err)
	}
	_ = art.Reader.Close()
	if hits != 1 {
		t.Fatalf("disk cache still fetched %d times", hits)
	}
}

func TestLaunchBoxCatalogSelectsClearLogoHandle(t *testing.T) {
	xml := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>42</DatabaseID><Name>Sonic the Hedgehog</Name>` +
		`<Platform>Sega Genesis</Platform><Overview>Blue hedgehog.</Overview>` +
		`<ReleaseYear>1991</ReleaseYear></Game>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName>` +
		`<Type>Box - Front</Type></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>logo_42.png</FileName>` +
		`<Type>Clear Logo</Type></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>fanart_42.jpg</FileName>` +
		`<Type>Fanart - Background</Type></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>banner_42.png</FileName>` +
		`<Type>Banner</Type></GameImage>` +
		`</LaunchBox>`
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic the Hedgehog", System: protocol.SystemMegaDrive})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	wantCover := launchBoxArtworkHandle("cover_42.jpg")
	wantLogo := launchBoxArtworkHandle("logo_42.png")
	wantMarquee := launchBoxArtworkHandle("banner_42.png")
	if result.Presentation.CoverArtworkID != wantCover {
		t.Fatalf("cover = %q want %q", result.Presentation.CoverArtworkID, wantCover)
	}
	if result.Presentation.LogoArtworkID != wantLogo {
		t.Fatalf("logo = %q want %q", result.Presentation.LogoArtworkID, wantLogo)
	}
	if result.Presentation.MarqueeArtworkID != wantMarquee {
		t.Fatalf("marquee = %q want %q", result.Presentation.MarqueeArtworkID, wantMarquee)
	}
	if _, ok := catalog.covers[wantLogo]; !ok {
		t.Fatal("logo filename missing from artwork map")
	}
	if _, ok := catalog.covers[wantMarquee]; !ok {
		t.Fatal("marquee filename missing from artwork map")
	}
}

func TestLaunchBoxCatalogSelectsBox3DOverCartAndSpine(t *testing.T) {
	xml := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>42</DatabaseID><Name>Sonic the Hedgehog</Name>` +
		`<Platform>Sega Genesis</Platform></Game>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>spine_42.png</FileName>` +
		`<Type>Box - Spine</Type></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cart_42.png</FileName>` +
		`<Type>Cart - 3D</Type></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>box3d_42.png</FileName>` +
		`<Type>Box - 3D</Type></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName>` +
		`<Type>Box - Front</Type></GameImage>` +
		`</LaunchBox>`
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic the Hedgehog", System: protocol.SystemMegaDrive})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	wantBox := launchBoxArtworkHandle("box3d_42.png")
	wantCover := launchBoxArtworkHandle("cover_42.jpg")
	if result.Presentation.Box3DArtworkID != wantBox {
		t.Fatalf("box3d = %q want %q", result.Presentation.Box3DArtworkID, wantBox)
	}
	if result.Presentation.CoverArtworkID != wantCover {
		t.Fatalf("cover = %q want %q", result.Presentation.CoverArtworkID, wantCover)
	}
	if _, ok := catalog.covers[wantBox]; !ok {
		t.Fatal("box3d filename missing from artwork map")
	}
}

func TestLaunchBoxCatalogPrefersArcadeMarqueeOverBanner(t *testing.T) {
	xml := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>7</DatabaseID><Name>OutRun</Name>` +
		`<Platform>Sega Genesis</Platform></Game>` +
		`<GameImage><DatabaseID>7</DatabaseID><FileName>banner_7.png</FileName>` +
		`<Type>Banner</Type></GameImage>` +
		`<GameImage><DatabaseID>7</DatabaseID><FileName>marquee_7.png</FileName>` +
		`<Type>Arcade - Marquee</Type></GameImage>` +
		`</LaunchBox>`
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "OutRun", System: protocol.SystemMegaDrive})
	if err != nil || result.Outcome != OutcomeExact {
		t.Fatalf("Lookup = %+v err=%v", result, err)
	}
	want := launchBoxArtworkHandle("marquee_7.png")
	if result.Presentation.MarqueeArtworkID != want {
		t.Fatalf("marquee = %q want arcade %q", result.Presentation.MarqueeArtworkID, want)
	}
	if result.Presentation.CoverArtworkID != "" || result.Presentation.LogoArtworkID != "" {
		t.Fatalf("unexpected cover/logo %+v", result.Presentation)
	}
}

func TestLaunchBoxCatalogLogoWithoutCover(t *testing.T) {
	xml := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>8</DatabaseID><Name>Pong</Name>` +
		`<Platform>Atari 2600</Platform></Game>` +
		`<GameImage><DatabaseID>8</DatabaseID><FileName>pong_logo.png</FileName>` +
		`<Type>Clear Logo</Type></GameImage>` +
		`</LaunchBox>`
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "Pong", System: protocol.SystemAtari2600})
	if err != nil || result.Outcome != OutcomeExact {
		t.Fatalf("Lookup = %+v err=%v", result, err)
	}
	if result.Presentation.CoverArtworkID != "" {
		t.Fatalf("cover = %q", result.Presentation.CoverArtworkID)
	}
	wantLogo := launchBoxArtworkHandle("pong_logo.png")
	if result.Presentation.LogoArtworkID != wantLogo {
		t.Fatalf("logo = %q want %q", result.Presentation.LogoArtworkID, wantLogo)
	}
}

func TestLaunchBoxLogoFetchesOfficialPNGOnce(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits++
		if request.URL.Path != "/logo_42.png" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(minimalPNG)
	}))
	t.Cleanup(server.Close)
	xml := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>42</DatabaseID><Name>Sonic the Hedgehog</Name>` +
		`<Platform>Sega Genesis</Platform></Game>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>logo_42.png</FileName>` +
		`<Type>Clear Logo</Type></GameImage>` +
		`</LaunchBox>`
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, &http.Client{Transport: rewriteLaunchBoxHost(server.URL)})
	t.Cleanup(func() { _ = runtime.Close() })
	handle := launchBoxArtworkHandle("logo_42.png")
	first, err := runtime.OpenArtwork(context.Background(), handle)
	if err != nil {
		t.Fatalf("OpenArtwork: %v", err)
	}
	body, err := io.ReadAll(first.Reader)
	_ = first.Reader.Close()
	if err != nil || first.MIME != "image/png" || len(body) == 0 {
		t.Fatalf("artwork mime=%q size=%d err=%v", first.MIME, len(body), err)
	}
	second, err := runtime.OpenArtwork(context.Background(), handle)
	if err != nil {
		t.Fatalf("second OpenArtwork: %v", err)
	}
	_ = second.Reader.Close()
	if hits != 1 {
		t.Fatalf("official image fetched %d times", hits)
	}
}

func TestLaunchBoxCatalogMatchesDumpTagsAndEnglishAlias(t *testing.T) {
	xml := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>9</DatabaseID><Name>Streets of Rage</Name>` +
		`<Platform>Sega Genesis</Platform><Overview>Brawler.</Overview>` +
		`<ReleaseYear>1991</ReleaseYear></Game>` +
		`<GameAlternateName><DatabaseID>9</DatabaseID><AlternateName>007 Shitou - The Duel</AlternateName></GameAlternateName>` +
		`</LaunchBox>`
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "007 Shitou - The Duel (Japan)", System: protocol.SystemMegaDrive})
	if err != nil || result.Outcome != OutcomeExact || result.Presentation.Summary != "Brawler." {
		t.Fatalf("alias lookup = %+v err=%v", result, err)
	}
	result, err = runtime.Lookup(context.Background(), LookupInput{Title: "Bare Knuckle ~ Streets of Rage (World) (Rev A)", System: protocol.SystemMegaDrive})
	if err != nil || (result.Outcome != OutcomeExact && result.Outcome != OutcomeConfident) {
		t.Fatalf("tilde lookup = %+v err=%v", result, err)
	}
}

func TestLaunchBoxCatalogMatchesHyphenAndColonAliases(t *testing.T) {
	xml := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>11</DatabaseID><Name>James Bond 007: The Duel</Name>` +
		`<Platform>Sega Genesis</Platform><Overview>Bond.</Overview>` +
		`<ReleaseYear>1993</ReleaseYear></Game>` +
		`<GameAlternateName><DatabaseID>11</DatabaseID><AlternateName>007 Shitou: The Duel</AlternateName></GameAlternateName>` +
		`</LaunchBox>`
	catalog, err := LoadLaunchBoxCatalog(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("LoadLaunchBoxCatalog: %v", err)
	}
	runtime := NewLaunchBoxRuntime(catalog, nil)
	t.Cleanup(func() { _ = runtime.Close() })
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "007 Shitou - The Duel (Japan)", System: protocol.SystemMegaDrive})
	if err != nil || result.Outcome != OutcomeExact || result.Presentation.Summary != "Bond." {
		t.Fatalf("colon alias lookup = %+v err=%v", result, err)
	}
}

func rewriteLaunchBoxHost(base string) http.RoundTripper {
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		target, err := http.NewRequestWithContext(request.Context(), request.Method, strings.TrimRight(base, "/")+request.URL.RequestURI(), request.Body)
		if err != nil {
			return nil, err
		}
		target.Header = request.Header
		return http.DefaultTransport.RoundTrip(target)
	})
}

// minimalPNG is a 1x1 PNG recognized by image/png.
var minimalPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
	0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// minimalJPEG is a 1x1 JPEG recognized by image/jpeg.
var minimalJPEG = []byte{
	0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01,
	0x01, 0x01, 0x00, 0x48, 0x00, 0x48, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43,
	0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09,
	0x09, 0x08, 0x0a, 0x0c, 0x14, 0x0d, 0x0c, 0x0b, 0x0b, 0x0c, 0x19, 0x12,
	0x13, 0x0f, 0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20,
	0x24, 0x2e, 0x27, 0x20, 0x22, 0x2c, 0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29,
	0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27, 0x39, 0x3d, 0x38, 0x32,
	0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01,
	0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x14, 0x00, 0x01,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x03, 0xff, 0xc4, 0x00, 0x14, 0x10, 0x01, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00,
	0x7b, 0xdf, 0xff, 0xd9,
}
