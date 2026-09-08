package metadata

import (
	"errors"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestValidLaunchBoxImageTypeAdmitsClearLogo(t *testing.T) {
	if !validLaunchBoxImageType("Clear Logo") {
		t.Fatal("Clear Logo rejected")
	}
	if validLaunchBoxImageTypeRank("logo", "Clear Logo") != 0 {
		t.Fatalf("Clear Logo rank = %d", validLaunchBoxImageTypeRank("logo", "Clear Logo"))
	}
	if validLaunchBoxImageTypeRank("cover", "Clear Logo") >= 0 {
		t.Fatal("Clear Logo ranked as cover")
	}
	if validLaunchBoxImageType("Banner") {
		t.Fatal("Banner admitted")
	}
}

func TestFramedXMLReaderRejectsOversizedOrdinaryTextBeforeEmission(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><Game><Overview>` + strings.Repeat("x", launchBoxXMLMaxTextBytes+1) + `</Overview></Game></LaunchBox>`
	reader := newFramedXMLReader(strings.NewReader(input))
	if _, err := io.Copy(io.Discard, reader); err == nil {
		t.Fatal("oversized ordinary text frame was accepted")
	}
}

func TestFramedXMLReaderTracksRejectedFrameWithoutEmittingItsPrefix(t *testing.T) {
	prefix := `<?xml version="1.0" standalone="yes"?><LaunchBox><Game><Overview>`
	input := prefix + strings.Repeat("x", launchBoxXMLMaxTextBytes+1) + `</Overview></Game></LaunchBox>`
	reader := newFramedXMLReader(strings.NewReader(input))
	if _, err := io.Copy(io.Discard, reader); err == nil {
		t.Fatal("oversized ordinary text frame was accepted")
	}
	if reader.emittedBytes != int64(len(prefix)) {
		t.Fatalf("offending text prefix was emitted: emitted=%d prefix=%d", reader.emittedBytes, len(prefix))
	}
	if reader.offendingFrameBytes != launchBoxXMLMaxTextBytes+1 {
		t.Fatalf("unexpected offending frame size: %d", reader.offendingFrameBytes)
	}
	if reader.frameHighWater != launchBoxXMLMaxTextBytes {
		t.Fatalf("unexpected actual buffered high-water: %d", reader.frameHighWater)
	}
}

func TestParseLaunchBoxXMLMemberStreamsSelectedFieldsAndIgnoresUnknownLeaves(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><Game>
    <DatabaseID>42</DatabaseID>
    <Name>  Super Game  </Name>
    <Platform>Super Nintendo Entertainment System</Platform>
    <Unknown>discarded</Unknown>
    <ReleaseYear>1991</ReleaseYear>
    <ReleaseDate>2000-01-01</ReleaseDate>
  </Game>
  <GameAlternateName><DatabaseID>42</DatabaseID><AlternateName xml:space="preserve">  Super  Game  </AlternateName></GameAlternateName>
  <GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><CRC32>1356669519</CRC32></GameImage>
</LaunchBox>`
	batch := newTestLaunchBoxRecordBatch()
	counts, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, batch)
	if err != nil {
		t.Fatalf("parse member: %v", err)
	}
	if counts.games != 1 || counts.aliases != 1 || counts.images != 1 || len(batch.observed) != 3 {
		t.Fatalf("unexpected counts=%+v records=%d", counts, len(batch.observed))
	}
	if got := batch.observed[0].game; got.databaseID != "42" || got.name != "  Super Game  " || got.platform == "" || got.releaseYear != "1991" {
		t.Fatalf("unexpected game record: %+v", got)
	}
	if batch.observed[1].alias.alternateName != "  Super  Game  " {
		t.Fatalf("raw alias was not retained for table classification: %+v", batch.observed[1].alias)
	}
}

func TestParseLaunchBoxXMLMemberRejectsDTDAndEntityReferences(t *testing.T) {
	cases := []string{
		`<?xml version="1.0" standalone="yes"?><!DOCTYPE LaunchBox><LaunchBox/>`,
		`<?xml version="1.0" standalone="yes"?><LaunchBox><Game><Name>&custom;</Name></Game></LaunchBox>`,
	}
	for _, input := range cases {
		t.Run(input[:minIntLaunchBoxXML(len(input), 20)], func(t *testing.T) {
			_, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, newTestLaunchBoxRecordBatch())
			if err == nil {
				t.Fatal("unsafe XML was accepted")
			}
		})
	}
}

func TestParseLaunchBoxXMLMemberRejectsDuplicateSelectedFieldsAndMissingReferences(t *testing.T) {
	duplicate := `<?xml version="1.0" standalone="yes"?><LaunchBox><Game><DatabaseID>42</DatabaseID><DatabaseID>43</DatabaseID><Name>x</Name><Platform>p</Platform></Game></LaunchBox>`
	missing := `<?xml version="1.0" standalone="yes"?><LaunchBox><GameAlternateName><DatabaseID>42</DatabaseID></GameAlternateName></LaunchBox>`
	for name, input := range map[string]string{"duplicate": duplicate, "missing": missing} {
		t.Run(name, func(t *testing.T) {
			batch := newTestLaunchBoxRecordBatch()
			_, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, batch)
			if err == nil {
				err = batch.validateLaunchBoxBatch()
			}
			if err == nil {
				t.Fatal("invalid XML member was accepted")
			}
		})
	}
}

func TestFramedXMLReaderRejectsMalformedEmptyElementAndEndTagWhitespace(t *testing.T) {
	for _, input := range []string{
		`<?xml version="1.0" standalone="yes"?><LaunchBox/ >`,
		`<?xml version="1.0" standalone="yes"?><LaunchBox></ LaunchBox>`,
	} {
		if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(input))); err == nil {
			t.Fatalf("malformed empty/end tag accepted: %q", input)
		}
	}
	if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(`<?xml version="1.0" standalone="yes"?><LaunchBox><N/></LaunchBox>`))); err != nil {
		t.Fatalf("valid empty element rejected: %v", err)
	}
}

type testLaunchBoxRecordTable struct {
	batch    *testLaunchBoxRecordBatch
	beginErr error
}

func (t *testLaunchBoxRecordTable) beginLaunchBoxBatch() (launchBoxRecordBatch, error) {
	if t.batch == nil {
		t.batch = newTestLaunchBoxRecordBatch()
	}
	t.batch.events = append(t.batch.events, "begin")
	if t.beginErr != nil {
		return nil, t.beginErr
	}
	return t.batch, nil
}

type testLaunchBoxRecordBatch struct {
	observed       []launchBoxRecord
	staged         []launchBoxRecord
	committed      []launchBoxRecord
	events         []string
	failures       map[string]error
	completed      map[string]bool
	validated      bool
	flushed        bool
	committedState bool
	aborted        bool

	rawAliasCount        int
	normalizedAliasCount int
	canonicalAliasCount  int
	blankAliasCount      int
	duplicateAliasCount  int
	matcherAliases       map[string][]string
	aliasPerGame         map[string]int
	imagePerGame         map[string]int
}

func newTestLaunchBoxRecordBatch() *testLaunchBoxRecordBatch {
	return &testLaunchBoxRecordBatch{
		failures:       make(map[string]error),
		completed:      make(map[string]bool),
		matcherAliases: make(map[string][]string),
		aliasPerGame:   make(map[string]int),
		imagePerGame:   make(map[string]int),
	}
}

func TestTestLaunchBoxBatchRejectsPerGameCapsBeforeAppend(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		limit  int
		record func(int) launchBoxRecord
	}{
		{name: "aliases", limit: launchBoxXMLMaxAliasesPerGame, record: func(index int) launchBoxRecord {
			return launchBoxRecord{
				member: "Metadata.xml",
				family: "GameAlternateName",
				alias:  launchBoxAliasRecord{databaseID: "1", alternateName: "Alias" + strconv.Itoa(index)},
			}
		}},
		{name: "images", limit: launchBoxXMLMaxImagesPerGame, record: func(index int) launchBoxRecord {
			return launchBoxRecord{
				member: "Metadata.xml",
				family: "GameImage",
				image: launchBoxImageRecord{
					databaseID: "1",
					fileName:   "cover_" + strconv.Itoa(index) + ".jpg",
					typeName:   "Box - Front",
					crc32:      strconv.Itoa(index),
				},
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			batch := newTestLaunchBoxRecordBatch()
			for index := 0; index < testCase.limit; index++ {
				if err := batch.putLaunchBoxRecord(testCase.record(index)); err != nil {
					t.Fatalf("record %d within per-game cap rejected: %v", index+1, err)
				}
			}
			if err := batch.putLaunchBoxRecord(testCase.record(testCase.limit)); err == nil {
				t.Fatal("per-game cap plus one was retained instead of rejected before append")
			}
			if len(batch.observed) != testCase.limit {
				t.Fatalf("offending record was retained: got=%d want=%d", len(batch.observed), testCase.limit)
			}
		})
	}
}

func (b *testLaunchBoxRecordBatch) failure(event string) error {
	return b.failures[event]
}

func (b *testLaunchBoxRecordBatch) putLaunchBoxRecord(record launchBoxRecord) error {
	b.events = append(b.events, "put:"+record.family+":"+record.member)
	if err := b.failure("put"); err != nil {
		return err
	}
	if b.aborted || b.committedState {
		return errors.New("test batch is closed")
	}
	if b.completed[record.member] {
		return errors.New("test batch member is already complete")
	}
	if b.aliasPerGame == nil {
		b.aliasPerGame = make(map[string]int)
	}
	if b.imagePerGame == nil {
		b.imagePerGame = make(map[string]int)
	}
	switch record.family {
	case "GameAlternateName":
		if b.aliasPerGame[record.alias.databaseID] >= launchBoxXMLMaxAliasesPerGame {
			return errors.New("test batch alias per-game limit exceeded")
		}
	case "GameImage":
		if b.imagePerGame[record.image.databaseID] >= launchBoxXMLMaxImagesPerGame {
			return errors.New("test batch image per-game limit exceeded")
		}
	}
	b.observed = append(b.observed, record)
	switch record.family {
	case "GameAlternateName":
		b.aliasPerGame[record.alias.databaseID]++
	case "GameImage":
		b.imagePerGame[record.image.databaseID]++
	}
	return nil
}

func (b *testLaunchBoxRecordBatch) completeLaunchBoxMember(member string) error {
	b.events = append(b.events, "complete:"+member)
	if err := b.failure("complete:" + member); err != nil {
		return err
	}
	if member != "Metadata.xml" && member != "Platforms.xml" {
		return errors.New("test batch member is invalid")
	}
	if b.aborted || b.committedState {
		return errors.New("test batch is closed")
	}
	if b.completed[member] {
		return errors.New("test batch member completed twice")
	}
	b.completed[member] = true
	return nil
}

func (b *testLaunchBoxRecordBatch) validateLaunchBoxBatch() error {
	b.events = append(b.events, "validate")
	if err := b.failure("validate"); err != nil {
		return err
	}
	if b.aborted || b.committedState || b.validated {
		return errors.New("test batch validation state is invalid")
	}
	if !b.completed["Metadata.xml"] || !b.completed["Platforms.xml"] {
		return errors.New("test batch requires both members")
	}
	type mirrorSet struct {
		platforms map[string]struct{}
		aliases   map[string]struct{}
	}
	sets := map[string]*mirrorSet{
		"Metadata.xml":  {platforms: make(map[string]struct{}), aliases: make(map[string]struct{})},
		"Platforms.xml": {platforms: make(map[string]struct{}), aliases: make(map[string]struct{})},
	}
	games := make(map[string]launchBoxRecord)
	rawAliases := make(map[string]struct{})
	normalizedAliases := make(map[string]struct{})
	images := make(map[string]struct{})
	imageIdentities := make(map[string]string)
	for _, record := range b.observed {
		switch record.family {
		case "Game":
			if _, exists := games[record.game.databaseID]; exists {
				return errors.New("test batch duplicate Game")
			}
			games[record.game.databaseID] = record
		case "GameAlternateName":
			b.rawAliasCount++
			rawKey := record.alias.databaseID + "\x00" + record.alias.alternateName + "\x00" + record.alias.region
			if _, exists := rawAliases[rawKey]; exists {
				return errors.New("test batch duplicate alias tuple")
			}
			rawAliases[rawKey] = struct{}{}
			canonicalKey := record.alias.databaseID + "\x00" + xmlBoundaryTrim(record.alias.alternateName) + "\x00" + xmlBoundaryTrim(record.alias.region)
			if _, exists := normalizedAliases[canonicalKey]; exists {
				b.duplicateAliasCount++
			} else {
				normalizedAliases[canonicalKey] = struct{}{}
			}
			b.normalizedAliasCount = len(normalizedAliases)
		case "GameImage":
			identity := record.image.databaseID + "\x00" + record.image.fileName + "\x00" + record.image.typeName + "\x00" + xmlBoundaryTrim(record.image.region)
			if previousCRC, exists := imageIdentities[identity]; exists {
				if previousCRC != record.image.crc32 {
					return errors.New("test batch image tuple has conflicting CRC32")
				}
				return errors.New("test batch image tuple is duplicated")
			}
			imageIdentities[identity] = record.image.crc32
			key := record.image.databaseID + "\x00" + record.image.fileName + "\x00" + record.image.typeName + "\x00" + record.image.region + "\x00" + record.image.crc32
			images[key] = struct{}{}
		case "Platform":
			name := xmlBoundaryTrim(record.platform.name)
			if _, exists := sets[record.member].platforms[name]; exists {
				return errors.New("test batch duplicate Platform")
			}
			sets[record.member].platforms[name] = struct{}{}
		case "PlatformAlternateName":
			name := xmlBoundaryTrim(record.platformAlias.name)
			alternate := xmlBoundaryTrim(record.platformAlias.alternate)
			key := name + "\x00" + alternate
			if _, exists := sets[record.member].aliases[key]; exists {
				return errors.New("test batch duplicate PlatformAlternateName")
			}
			sets[record.member].aliases[key] = struct{}{}
		}
	}
	for key := range rawAliases {
		databaseID := strings.SplitN(key, "\x00", 2)[0]
		if _, exists := games[databaseID]; !exists {
			return errors.New("test batch alias references unknown Game")
		}
	}
	for key := range images {
		databaseID := strings.SplitN(key, "\x00", 2)[0]
		if _, exists := games[databaseID]; !exists {
			return errors.New("test batch image references unknown Game")
		}
	}
	for _, set := range sets {
		for key := range set.aliases {
			name := strings.SplitN(key, "\x00", 2)[0]
			if _, exists := set.platforms[name]; !exists {
				return errors.New("test batch alternate references unknown Platform")
			}
		}
	}
	if !equalTestLaunchBoxSets(sets["Metadata.xml"].platforms, sets["Platforms.xml"].platforms) || !equalTestLaunchBoxSets(sets["Metadata.xml"].aliases, sets["Platforms.xml"].aliases) {
		return errors.New("test batch platform mirror mismatch")
	}
	b.validated = true
	return nil
}

func equalTestLaunchBoxSets(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, exists := right[key]; !exists {
			return false
		}
	}
	return true
}

func (b *testLaunchBoxRecordBatch) flushLaunchBoxBatch() error {
	b.events = append(b.events, "flush")
	if err := b.failure("flush"); err != nil {
		return err
	}
	if b.aborted || b.committedState || b.flushed {
		return errors.New("test batch flush state is invalid")
	}
	if !b.validated {
		return errors.New("test batch was not validated")
	}
	games := make(map[string]string)
	for _, record := range b.observed {
		if record.family == "Game" {
			games[record.game.databaseID] = xmlBoundaryTrim(record.game.platform)
		}
	}
	type canonicalAlias struct {
		databaseID    string
		numericID     uint64
		alternateName string
		region        string
	}
	canonicalAliases := make(map[string]canonicalAlias)
	blankAliases := make(map[string]struct{})
	aliasInsertAt := -1
	for _, record := range b.observed {
		flush := false
		switch record.family {
		case "Game":
			flush = supportedTestLaunchBoxPlatform(record.game.platform)
		case "GameAlternateName":
			if aliasInsertAt < 0 {
				aliasInsertAt = len(b.staged)
			}
			alternateName := xmlBoundaryTrim(record.alias.alternateName)
			region := xmlBoundaryTrim(record.alias.region)
			if alternateName == "" {
				blankKey := record.alias.databaseID + "\x00" + region
				if _, exists := blankAliases[blankKey]; !exists {
					blankAliases[blankKey] = struct{}{}
					b.blankAliasCount++
				}
				continue
			}
			if !supportedTestLaunchBoxPlatform(games[record.alias.databaseID]) {
				continue
			}
			numericID, err := strconv.ParseUint(record.alias.databaseID, 10, 64)
			if err != nil {
				return errors.New("test batch alias DatabaseID is not numeric")
			}
			canonical := record.alias.databaseID + "\x00" + alternateName + "\x00" + region
			if _, exists := canonicalAliases[canonical]; exists {
				continue
			}
			canonicalAliases[canonical] = canonicalAlias{databaseID: record.alias.databaseID, numericID: numericID, alternateName: alternateName, region: region}
			continue
		case "GameImage":
			flush = supportedTestLaunchBoxPlatform(games[record.image.databaseID])
		case "Platform", "PlatformAlternateName":
			flush = record.member == "Platforms.xml"
		}
		if flush {
			b.staged = append(b.staged, record)
		}
	}
	rows := make([]canonicalAlias, 0, len(canonicalAliases))
	for _, row := range canonicalAliases {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].numericID != rows[right].numericID {
			return rows[left].numericID < rows[right].numericID
		}
		if rows[left].alternateName != rows[right].alternateName {
			return rows[left].alternateName < rows[right].alternateName
		}
		return rows[left].region < rows[right].region
	})
	b.canonicalAliasCount = len(rows)
	canonicalRows := make([]launchBoxRecord, 0, len(rows))
	matcherSeen := make(map[string]map[string]struct{})
	for _, row := range rows {
		matcherName, err := NormalizeTitle(row.alternateName)
		if err != nil {
			return errors.New("test batch alias cannot be normalized for matching")
		}
		canonicalRows = append(canonicalRows, launchBoxRecord{
			member: "Metadata.xml",
			family: "GameAlternateName",
			alias:  launchBoxAliasRecord{databaseID: row.databaseID, alternateName: row.alternateName, region: row.region},
		})
		seenGames := matcherSeen[matcherName]
		if seenGames == nil {
			seenGames = make(map[string]struct{})
			matcherSeen[matcherName] = seenGames
		}
		if _, exists := seenGames[row.databaseID]; !exists {
			seenGames[row.databaseID] = struct{}{}
			b.matcherAliases[matcherName] = append(b.matcherAliases[matcherName], row.databaseID)
		}
	}
	if len(canonicalRows) > 0 {
		if aliasInsertAt < 0 || aliasInsertAt > len(b.staged) {
			aliasInsertAt = len(b.staged)
		}
		staged := make([]launchBoxRecord, 0, len(b.staged)+len(canonicalRows))
		staged = append(staged, b.staged[:aliasInsertAt]...)
		staged = append(staged, canonicalRows...)
		staged = append(staged, b.staged[aliasInsertAt:]...)
		b.staged = staged
	}
	b.flushed = true
	return nil
}

func supportedTestLaunchBoxPlatform(platform string) bool {
	platform = xmlBoundaryTrim(platform)
	return platform == "Super Nintendo Entertainment System" || platform == "Sega Genesis"
}

func (b *testLaunchBoxRecordBatch) commitLaunchBoxBatch() error {
	b.events = append(b.events, "commit")
	if err := b.failure("commit"); err != nil {
		return err
	}
	if b.aborted || b.committedState || !b.flushed {
		return errors.New("test batch commit state is invalid")
	}
	b.committed = append([]launchBoxRecord(nil), b.staged...)
	b.committedState = true
	return nil
}

func (b *testLaunchBoxRecordBatch) abortLaunchBoxBatch() error {
	if b.aborted {
		return errors.New("test batch already aborted")
	}
	b.events = append(b.events, "abort")
	b.aborted = true
	b.staged = nil
	return b.failure("abort")
}

func countTestLaunchBoxEvent(events []string, want string) int {
	count := 0
	for _, event := range events {
		if event == want {
			count++
		}
	}
	return count
}

func launchBoxTestMembers(metadata, platforms string) (io.Reader, io.Reader) {
	return strings.NewReader(metadata), strings.NewReader(platforms)
}

func TestParseLaunchBoxXMLMembersRunsOneAtomicLifecycle(t *testing.T) {
	metadata := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform><Game><DatabaseID>42</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game><GameAlternateName><DatabaseID>42</DatabaseID><AlternateName>Alias</AlternateName></GameAlternateName><GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><CRC32>1</CRC32></GameImage></LaunchBox>`
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform></LaunchBox>`
	metadataReader, platformsReader := launchBoxTestMembers(metadata, platforms)
	table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
	if _, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	want := []string{"begin", "put:Platform:Metadata.xml", "put:Game:Metadata.xml", "put:GameAlternateName:Metadata.xml", "put:GameImage:Metadata.xml", "complete:Metadata.xml", "put:Platform:Platforms.xml", "complete:Platforms.xml", "validate", "flush", "commit"}
	if strings.Join(table.batch.events, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected lifecycle: %v", table.batch.events)
	}
	if table.batch.aborted || len(table.batch.committed) != 4 {
		t.Fatalf("unexpected durable state: aborted=%v committed=%d", table.batch.aborted, len(table.batch.committed))
	}
	if table.batch.committed[0].family != "Game" || table.batch.committed[1].family != "GameAlternateName" || table.batch.committed[2].family != "GameImage" || table.batch.committed[3].family != "Platform" || table.batch.committed[3].member != "Platforms.xml" {
		t.Fatalf("mirror rows or unsupported authority were published: %+v", table.batch.committed)
	}
}

func TestParseLaunchBoxXMLMembersFlushesCanonicalAliasesInStableOrder(t *testing.T) {
	const platform = "Super Nintendo Entertainment System"
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>` + platform + `</Name></Platform></LaunchBox>`
	type aliasInput struct{ databaseID, name, region string }
	metadataFor := func(aliases []aliasInput) string {
		var builder strings.Builder
		builder.WriteString(`<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>` + platform + `</Name></Platform>`)
		builder.WriteString(`<Game><DatabaseID>10</DatabaseID><Name>Ten</Name><Platform>` + platform + `</Platform></Game>`)
		builder.WriteString(`<Game><DatabaseID>2</DatabaseID><Name>Two</Name><Platform>` + platform + `</Platform></Game>`)
		for _, alias := range aliases {
			builder.WriteString(`<GameAlternateName><DatabaseID>` + alias.databaseID + `</DatabaseID><AlternateName xml:space="preserve">` + alias.name + `</AlternateName><Region>` + alias.region + `</Region></GameAlternateName>`)
		}
		builder.WriteString(`</LaunchBox>`)
		return builder.String()
	}
	parseAliases := func(metadata string) ([]string, *testLaunchBoxRecordBatch) {
		t.Helper()
		batch := newTestLaunchBoxRecordBatch()
		table := &testLaunchBoxRecordTable{batch: batch}
		if _, err := parseLaunchBoxXMLMembers(strings.NewReader(metadata), strings.NewReader(platforms), table); err != nil {
			t.Fatalf("parse snapshot: %v", err)
		}
		var got []string
		for _, record := range batch.committed {
			if record.family == "GameAlternateName" {
				got = append(got, record.alias.databaseID+"\x00"+record.alias.alternateName+"\x00"+record.alias.region)
			}
		}
		return got, batch
	}
	forward, forwardBatch := parseAliases(metadataFor([]aliasInput{
		{databaseID: "10", name: " Zulu ", region: " World "},
		{databaseID: "2", name: " Alpha ", region: " World "},
		{databaseID: "2", name: "Alpha", region: "World"},
		{databaseID: "2", name: "", region: "Japan"},
		{databaseID: "10", name: "Same", region: "US"},
		{databaseID: "2", name: "Same", region: "US"},
		{databaseID: "2", name: "Same", region: "Japan"},
		{databaseID: "2", name: " SAME ", region: "EU"},
	}))
	reverse, _ := parseAliases(metadataFor([]aliasInput{
		{databaseID: "2", name: "Same", region: "US"},
		{databaseID: "10", name: "Same", region: "US"},
		{databaseID: "2", name: "Alpha", region: "World"},
		{databaseID: "2", name: " Alpha ", region: " World "},
		{databaseID: "10", name: " Zulu ", region: " World "},
		{databaseID: "2", name: "", region: "Japan"},
		{databaseID: "2", name: "Same", region: "Japan"},
		{databaseID: "2", name: " SAME ", region: "EU"},
	}))
	want := []string{"2\x00Alpha\x00World", "2\x00SAME\x00EU", "2\x00Same\x00Japan", "2\x00Same\x00US", "10\x00Same\x00US", "10\x00Zulu\x00World"}
	if !reflect.DeepEqual(forward, want) {
		t.Fatalf("unexpected canonical aliases: got=%q want=%q", forward, want)
	}
	if !reflect.DeepEqual(reverse, want) || !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("alias output changed with source order: forward=%q reverse=%q", forward, reverse)
	}
	if forwardBatch.rawAliasCount != 8 || forwardBatch.normalizedAliasCount != 7 || forwardBatch.canonicalAliasCount != 6 || forwardBatch.blankAliasCount != 1 || forwardBatch.duplicateAliasCount != 1 {
		t.Fatalf("unexpected alias reconciliation counts: raw=%d normalized=%d canonical=%d blank=%d duplicate=%d", forwardBatch.rawAliasCount, forwardBatch.normalizedAliasCount, forwardBatch.canonicalAliasCount, forwardBatch.blankAliasCount, forwardBatch.duplicateAliasCount)
	}
	if got := forwardBatch.matcherAliases["same"]; !reflect.DeepEqual(got, []string{"2", "10"}) {
		t.Fatalf("normalized same alias did not preserve ambiguity/provenance across games and regions: %q", got)
	}
	decision, err := MatchCandidates(LookupInput{Title: "same", System: "megadrive"}, "megadrive", []Candidate{
		{ProviderID: "2", Name: "Two", AlternativeNames: []string{"Same"}, PlatformIDs: []string{"megadrive"}},
		{ProviderID: "10", Name: "Ten", AlternativeNames: []string{"Same"}, PlatformIDs: []string{"megadrive"}},
	})
	if err != nil || decision.Outcome != OutcomeAmbiguous {
		t.Fatalf("same alias across games was not ambiguous: decision=%+v err=%v", decision, err)
	}
}

func TestParseLaunchBoxXMLMembersRejectsExactRawAliasRepetitionBeforeTrim(t *testing.T) {
	metadata := `<?xml version="1.0" standalone="yes"?><LaunchBox><Game><DatabaseID>1</DatabaseID><Name>One</Name><Platform>Super Nintendo Entertainment System</Platform></Game>` +
		`<GameAlternateName><DatabaseID>1</DatabaseID><AlternateName xml:space="preserve">Alias</AlternateName><Region>US</Region></GameAlternateName>` +
		`<GameAlternateName><DatabaseID>1</DatabaseID><AlternateName xml:space="preserve">Alias</AlternateName><Region>US</Region></GameAlternateName></LaunchBox>`
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform></LaunchBox>`
	batch := newTestLaunchBoxRecordBatch()
	table := &testLaunchBoxRecordTable{batch: batch}
	if _, err := parseLaunchBoxXMLMembers(strings.NewReader(metadata), strings.NewReader(platforms), table); err == nil {
		t.Fatal("exact raw alias repetition was accepted")
	}
	if countTestLaunchBoxEvent(batch.events, "abort") != 1 || len(batch.committed) != 0 {
		t.Fatalf("raw alias repetition was not atomic: events=%v committed=%d", batch.events, len(batch.committed))
	}
}

func TestParseLaunchBoxXMLMembersRejectsPlatformMirrorMismatches(t *testing.T) {
	cases := map[string][2]string{
		"name": {
			`<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform></LaunchBox>`,
			`<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Sega Genesis</Name></Platform></LaunchBox>`,
		},
		"alternate": {
			`<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform><PlatformAlternateName><Name>Super Nintendo Entertainment System</Name><Alternate>SNES</Alternate></PlatformAlternateName></LaunchBox>`,
			`<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform><PlatformAlternateName><Name>Super Nintendo Entertainment System</Name><Alternate>Super NES</Alternate></PlatformAlternateName></LaunchBox>`,
		},
	}
	for name, members := range cases {
		t.Run(name, func(t *testing.T) {
			metadataReader, platformsReader := launchBoxTestMembers(members[0], members[1])
			table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
			if _, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table); err == nil {
				t.Fatal("mirror mismatch was accepted")
			}
			if !table.batch.aborted || countTestLaunchBoxEvent(table.batch.events, "abort") != 1 || len(table.batch.committed) != 0 {
				t.Fatalf("mirror failure was not atomic: events=%v committed=%d", table.batch.events, len(table.batch.committed))
			}
		})
	}
}

func TestParseLaunchBoxXMLMembersDoesNotFlushUnsupportedGraphChildren(t *testing.T) {
	metadata := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<GameAlternateName><DatabaseID>7</DatabaseID><AlternateName>Before</AlternateName></GameAlternateName>` +
		`<GameImage><DatabaseID>7</DatabaseID><FileName>before.jpg</FileName><Type>Box - Front</Type><CRC32>1</CRC32></GameImage>` +
		`<Game><DatabaseID>7</DatabaseID><Name>Unsupported</Name><Platform>Atari 2600</Platform></Game>` +
		`<GameAlternateName><DatabaseID>7</DatabaseID><AlternateName>After</AlternateName></GameAlternateName>` +
		`<GameImage><DatabaseID>7</DatabaseID><FileName>after.jpg</FileName><Type>Box - Front</Type><CRC32>2</CRC32></GameImage>` +
		`<Platform><Name>Atari 2600</Name></Platform></LaunchBox>`
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Atari 2600</Name></Platform></LaunchBox>`
	metadataReader, platformsReader := launchBoxTestMembers(metadata, platforms)
	table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
	if _, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table); err != nil {
		t.Fatalf("parse unsupported graph: %v", err)
	}
	if len(table.batch.observed) != 7 || len(table.batch.committed) != 1 {
		t.Fatalf("unexpected unsupported graph publication: observed=%d committed=%d", len(table.batch.observed), len(table.batch.committed))
	}
	for _, record := range table.batch.committed {
		if record.family != "Platform" || record.member != "Platforms.xml" {
			t.Fatalf("unsupported record was flushed: %+v", record)
		}
	}
}

func TestParseLaunchBoxXMLMembersFailureAbortsExactlyOnce(t *testing.T) {
	metadata := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform><Game><DatabaseID>42</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game></LaunchBox>`
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform></LaunchBox>`
	for _, failurePoint := range []string{"put", "complete:Metadata.xml", "complete:Platforms.xml", "validate", "flush", "commit"} {
		t.Run(failurePoint, func(t *testing.T) {
			metadataReader, platformsReader := launchBoxTestMembers(metadata, platforms)
			batch := newTestLaunchBoxRecordBatch()
			batch.failures[failurePoint] = errors.New("injected " + failurePoint)
			table := &testLaunchBoxRecordTable{batch: batch}
			if _, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table); err == nil {
				t.Fatal("injected lifecycle failure was swallowed")
			}
			if countTestLaunchBoxEvent(batch.events, "abort") != 1 || len(batch.committed) != 0 {
				t.Fatalf("failure was not aborted atomically: events=%v committed=%d", batch.events, len(batch.committed))
			}
		})
	}

	t.Run("begin", func(t *testing.T) {
		batch := newTestLaunchBoxRecordBatch()
		table := &testLaunchBoxRecordTable{batch: batch, beginErr: errors.New("injected begin")}
		metadataReader, platformsReader := launchBoxTestMembers(metadata, platforms)
		if _, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table); err == nil {
			t.Fatal("begin failure was swallowed")
		}
		if countTestLaunchBoxEvent(batch.events, "abort") != 0 {
			t.Fatal("begin failure attempted an abort without a batch")
		}
	})
}

func TestTestLaunchBoxBatchRejectsInvalidLifecycleState(t *testing.T) {
	batch := newTestLaunchBoxRecordBatch()
	if err := batch.completeLaunchBoxMember("Metadata.xml"); err != nil {
		t.Fatalf("first member completion rejected: %v", err)
	}
	if err := batch.completeLaunchBoxMember("Metadata.xml"); err == nil {
		t.Fatal("duplicate member completion accepted")
	}
	if err := batch.completeLaunchBoxMember("invalid.xml"); err == nil {
		t.Fatal("invalid member completion accepted")
	}
	if err := batch.putLaunchBoxRecord(launchBoxRecord{member: "Metadata.xml", family: "Game"}); err == nil {
		t.Fatal("Put-after-complete accepted")
	}
	if err := batch.abortLaunchBoxBatch(); err != nil {
		t.Fatalf("first abort rejected: %v", err)
	}
	if err := batch.abortLaunchBoxBatch(); err == nil {
		t.Fatal("duplicate abort accepted")
	}
}

func TestParseLaunchBoxXMLMembersSecondMemberErrorAbortsWithoutPartialCommit(t *testing.T) {
	metadata := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform></LaunchBox>`
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>`
	batch := newTestLaunchBoxRecordBatch()
	table := &testLaunchBoxRecordTable{batch: batch}
	if _, err := parseLaunchBoxXMLMembers(strings.NewReader(metadata), strings.NewReader(platforms), table); err == nil {
		t.Fatal("truncated second member accepted")
	}
	if countTestLaunchBoxEvent(batch.events, "complete:Metadata.xml") != 1 || countTestLaunchBoxEvent(batch.events, "abort") != 1 || len(batch.committed) != 0 {
		t.Fatalf("second-member error published partial state: events=%v committed=%d", batch.events, len(batch.committed))
	}
}

func TestParseLaunchBoxXMLMembersJoinsAbortFailureWithoutSourceData(t *testing.T) {
	metadata := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform></LaunchBox>`
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox><Platform><Name>Super Nintendo Entertainment System</Name></Platform></LaunchBox>`
	primary := errors.New("primary lifecycle failure")
	abort := errors.New("abort lifecycle failure")
	batch := newTestLaunchBoxRecordBatch()
	batch.failures["validate"] = primary
	batch.failures["abort"] = abort
	table := &testLaunchBoxRecordTable{batch: batch}
	metadataReader, platformsReader := launchBoxTestMembers(metadata, platforms)
	_, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table)
	if !errors.Is(err, primary) || !errors.Is(err, abort) {
		t.Fatalf("primary and abort errors were not joined: %v", err)
	}
	if strings.Contains(err.Error(), "<LaunchBox>") || strings.Contains(err.Error(), "Super Nintendo") {
		t.Fatalf("joined lifecycle error exposed source data: %v", err)
	}
	if countTestLaunchBoxEvent(batch.events, "abort") != 1 || len(batch.committed) != 0 {
		t.Fatalf("abort error did not preserve atomicity: events=%v committed=%d", batch.events, len(batch.committed))
	}
}

func TestParseLaunchBoxXMLMembersUsesOneSharedAggregateAttributeBudget(t *testing.T) {
	metadata := launchBoxMetadataWithAliases(launchBoxXMLMaxAggregateAttributes)
	platforms := `<?xml version="1.0" standalone="yes"?><LaunchBox/>`
	metadataReader, platformsReader := launchBoxTestMembers(metadata, platforms)
	table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
	if _, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table); err != nil {
		t.Fatalf("exact shared aggregate attribute budget was rejected: %v", err)
	}
	if table.batch.aborted || len(table.batch.committed) == 0 {
		t.Fatalf("exact shared budget did not commit: events=%v committed=%d", table.batch.events, len(table.batch.committed))
	}

	metadataReader, platformsReader = launchBoxTestMembers(metadata, `<?xml version="1.0" standalone="yes"?><LaunchBox xml:space="preserve"/>`)
	table = &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
	if _, err := parseLaunchBoxXMLMembers(metadataReader, platformsReader, table); err == nil || !strings.Contains(err.Error(), "aggregate structure exceeds bound") {
		t.Fatalf("shared aggregate attribute budget was not enforced across members: %v", err)
	}
	if countTestLaunchBoxEvent(table.batch.events, "abort") != 1 || len(table.batch.committed) != 0 {
		t.Fatalf("shared budget overflow was not atomic: events=%v committed=%d", table.batch.events, len(table.batch.committed))
	}
}

func launchBoxMetadataWithAliases(aliasCount int) string {
	const aliasesPerGame = launchBoxXMLMaxAliasesPerGame
	gameCount := (aliasCount + aliasesPerGame - 1) / aliasesPerGame
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" standalone="yes"?><LaunchBox>`)
	for gameIndex := 1; gameIndex <= gameCount; gameIndex++ {
		builder.WriteString(`<Game><DatabaseID>`)
		builder.WriteString(strconv.Itoa(gameIndex))
		builder.WriteString(`</DatabaseID><Name>Title`)
		builder.WriteString(strconv.Itoa(gameIndex))
		builder.WriteString(`</Name><Platform>Super Nintendo Entertainment System</Platform></Game>`)
	}
	aliasIndex := 0
	for gameIndex := 1; gameIndex <= gameCount && aliasIndex < aliasCount; gameIndex++ {
		for gameAliasIndex := 0; gameAliasIndex < aliasesPerGame && aliasIndex < aliasCount; gameAliasIndex++ {
			aliasIndex++
			builder.WriteString(`<GameAlternateName><DatabaseID>`)
			builder.WriteString(strconv.Itoa(gameIndex))
			builder.WriteString(`</DatabaseID><AlternateName xml:space="preserve">Alias`)
			builder.WriteString(strconv.Itoa(aliasIndex))
			builder.WriteString(`</AlternateName></GameAlternateName>`)
		}
	}
	builder.WriteString(`</LaunchBox>`)
	return builder.String()
}

func TestParseLaunchBoxXMLMemberCannotPublish(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><Game><DatabaseID>42</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game></LaunchBox>`
	batch := newTestLaunchBoxRecordBatch()
	if _, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, batch); err != nil {
		t.Fatalf("parse member: %v", err)
	}
	if len(batch.committed) != 0 || len(batch.staged) != 0 || len(batch.events) != 1 || batch.events[0] != "put:Game:Metadata.xml" {
		t.Fatalf("member helper performed lifecycle/publication: events=%v staged=%d committed=%d", batch.events, len(batch.staged), len(batch.committed))
	}
}

func minIntLaunchBoxXML(left, right int) int {
	if left < right {
		return left
	}
	return right
}
