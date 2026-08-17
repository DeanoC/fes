package metadata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/protocol"
)

const launchBoxSampleXML = `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
	`<Game><DatabaseID>42</DatabaseID><Name>Sonic the Hedgehog</Name>` +
	`<Platform>Sega Genesis</Platform><Overview>Blue hedgehog.</Overview>` +
	`<ReleaseYear>1991</ReleaseYear><Genres>Platform;Action</Genres>` +
	`<Developer>Sonic Team</Developer><Publisher>SEGA</Publisher><MaxPlayers>1</MaxPlayers></Game>` +
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
		result.Presentation.Genre != "Platform" || result.Presentation.Studio != "Sonic Team" || result.Presentation.Players != "1" {
		t.Fatalf("presentation = %+v", result.Presentation)
	}
	if result.Attribution.Provider != ProviderLaunchBox || result.Attribution.Label != "Data from LaunchBox Games Database" {
		t.Fatalf("attribution = %+v", result.Attribution)
	}
	wantHandle := launchBoxArtworkHandle("cover_42.jpg")
	if result.Presentation.CoverArtworkID != wantHandle {
		t.Fatalf("cover = %q want %q", result.Presentation.CoverArtworkID, wantHandle)
	}

	unrelated, err := runtime.Lookup(context.Background(), LookupInput{Title: "Unrelated", System: protocol.SystemMegaDrive})
	if err != nil || unrelated.Outcome != OutcomeNoMatch {
		t.Fatalf("unrelated = %+v err=%v", unrelated, err)
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
