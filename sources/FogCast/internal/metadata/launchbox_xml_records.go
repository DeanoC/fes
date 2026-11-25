package metadata

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type launchBoxRecordTable interface {
	beginLaunchBoxBatch() (launchBoxRecordBatch, error)
}

type launchBoxRecordBatch interface {
	putLaunchBoxRecord(launchBoxRecord) error
	completeLaunchBoxMember(string) error
	validateLaunchBoxBatch() error
	flushLaunchBoxBatch() error
	commitLaunchBoxBatch() error
	abortLaunchBoxBatch() error
}

type launchBoxRecord struct {
	member        string
	family        string
	game          launchBoxGameRecord
	alias         launchBoxAliasRecord
	image         launchBoxImageRecord
	platform      launchBoxPlatformRecord
	platformAlias launchBoxPlatformAliasRecord
}

type launchBoxGameRecord struct {
	databaseID  string
	name        string
	platform    string
	overview    string
	releaseYear string
	genres      string
	developer   string
	publisher   string
	maxPlayers  string
	series      string
}

type launchBoxAliasRecord struct {
	databaseID    string
	alternateName string
	region        string
}

type launchBoxImageRecord struct {
	databaseID string
	fileName   string
	typeName   string
	region     string
	crc32      string
}

type launchBoxPlatformRecord struct{ name string }

type launchBoxPlatformAliasRecord struct{ name, alternate string }

type launchBoxMemberCounts struct {
	games, aliases, images, platforms, platformAliases, emulators, emulatorPlatforms int
	records                                                                          int
}

func normalizeLaunchBoxGenres(value string) (string, bool) {
	parts := strings.Split(value, ";")
	if len(parts) > 64 {
		return "", false
	}
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = xmlBoundaryTrim(part)
		if part == "" {
			continue
		}
		if len(part) > 256 || utf8.RuneCountInString(part) > 128 {
			return "", false
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, ";"), true
}

func validLaunchBoxID(value string) bool {
	if value == "" || len(value) > 19 || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(value, 10, 63)
	return err == nil && value != "0"
}

func validLaunchBoxYear(value string) bool {
	if len(value) != 4 {
		return false
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func validLaunchBoxMaxPlayers(value string) bool {
	if value == "" || len(value) > 3 {
		return false
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	parsed, err := strconv.Atoi(value)
	return err == nil && parsed >= 1 && parsed <= 999
}

func validLaunchBoxImageFileName(value string) bool {
	if value == "" || len(value) > 44 || utf8.RuneCountInString(value) > 44 || !utf8.ValidString(value) {
		return false
	}
	dot := strings.LastIndexByte(value, '.')
	if dot < 1 || dot > 39 || dot == len(value)-1 {
		return false
	}
	for index, character := range value[:dot] {
		if index == 0 {
			if !asciiAlphaNumeric(character) {
				return false
			}
			continue
		}
		if !asciiAlphaNumeric(character) && character != '_' && character != '-' {
			return false
		}
	}
	switch value[dot+1:] {
	case "jpg", "jpeg", "png", "gif", "webp":
		return true
	default:
		return false
	}
}

func asciiAlphaNumeric(value rune) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}

func validLaunchBoxCRC32(value string) bool {
	if value == "" || len(value) > 10 {
		return false
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	return err == nil && parsed <= 0xFFFFFFFF
}

func validLaunchBoxImageType(value string) bool {
	switch value {
	case "Box - Front", "Box - Front - Reconstructed", "Fanart - Box - Front",
		"Fanart - Background", "Screenshot - Gameplay", "Screenshot - Game Title",
		"Clear Logo", "Banner", "Arcade - Marquee",
		"Box - 3D", "Cart - 3D", "Box - Spine":
		return true
	default:
		return false
	}
}

func validLaunchBoxImageTypeRank(role, value string) int {
	if role == "cover" {
		switch value {
		case "Box - Front":
			return 0
		case "Box - Front - Reconstructed":
			return 1
		case "Fanart - Box - Front":
			return 2
		}
	} else if role == "backdrop" {
		switch value {
		case "Fanart - Background":
			return 0
		case "Screenshot - Gameplay":
			return 1
		case "Screenshot - Game Title":
			return 2
		}
	} else if role == "logo" {
		switch value {
		case "Clear Logo":
			return 0
		}
	} else if role == "marquee" {
		switch value {
		case "Arcade - Marquee":
			return 0
		case "Banner":
			return 1
		}
	} else if role == "box3d" {
		switch value {
		case "Box - 3D":
			return 0
		case "Cart - 3D":
			return 1
		case "Box - Spine":
			return 2
		}
	}
	return -1
}

func parseLaunchBoxXMLMember(reader io.Reader, member string, budget *launchBoxXMLBudget, batch launchBoxRecordBatch) (launchBoxMemberCounts, error) {
	if reader == nil || batch == nil || (member != "Metadata.xml" && member != "Platforms.xml") {
		return launchBoxMemberCounts{}, errors.New("launchbox XML member is unavailable")
	}
	if budget == nil {
		budget = &launchBoxXMLBudget{}
	}
	framed := newFramedXMLReaderWithBudget(reader, budget)
	decoder := xml.NewDecoder(framed)
	decoder.Strict = true

	var counts launchBoxMemberCounts
	var rootSeen, rootComplete bool
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) && rootComplete {
				return counts, nil
			}
			return counts, fmt.Errorf("launchbox XML member is invalid: %w", err)
		}
		switch current := token.(type) {
		case xml.ProcInst:
			if rootSeen || current.Target != "xml" {
				return counts, errors.New("launchbox XML processing instruction is invalid")
			}
		case xml.Directive:
			return counts, errors.New("launchbox XML directive is not allowed")
		case xml.Comment:
			continue
		case xml.CharData:
			if !rootSeen || rootComplete {
				if !xmlWhitespaceOnly(current) {
					return counts, errors.New("launchbox XML has unexpected character data")
				}
				continue
			}
			if !xmlWhitespaceOnly(current) {
				return counts, errors.New("launchbox XML root has mixed character data")
			}
		case xml.StartElement:
			if !rootSeen {
				if current.Name.Space != "" || current.Name.Local != "LaunchBox" || len(current.Attr) != 0 {
					return counts, errors.New("launchbox XML root is invalid")
				}
				rootSeen = true
				continue
			}
			if rootComplete {
				return counts, errors.New("launchbox XML contains multiple roots")
			}
			family := current.Name.Local
			if current.Name.Space != "" || !launchBoxFamilyAllowed(member, family) || len(current.Attr) != 0 {
				return counts, errors.New("launchbox XML record family is invalid")
			}
			if err := counts.allow(family, member); err != nil {
				return counts, err
			}
			record, err := parseLaunchBoxRecord(decoder, current, member)
			if err != nil {
				return counts, err
			}
			record.member = member
			if err := validateLaunchBoxRecord(&record); err != nil {
				return counts, err
			}
			if err := batch.putLaunchBoxRecord(record); err != nil {
				return counts, err
			}
		case xml.EndElement:
			if !rootSeen || rootComplete || current.Name.Space != "" || current.Name.Local != "LaunchBox" {
				return counts, errors.New("launchbox XML root end is invalid")
			}
			rootComplete = true
		default:
			return counts, errors.New("launchbox XML member contains unsupported token")
		}
	}
}

func validateLaunchBoxRecord(record *launchBoxRecord) error {
	if record == nil {
		return errors.New("launchbox XML record is unavailable")
	}
	switch record.family {
	case "Game":
		if !validLaunchBoxID(record.game.databaseID) {
			return errors.New("launchbox Game.DatabaseID is invalid")
		}
		if xmlBoundaryTrim(record.game.name) == "" || xmlBoundaryTrim(record.game.platform) == "" {
			return errors.New("launchbox Game required field is empty")
		}
		if record.game.releaseYear != "" && !validLaunchBoxYear(record.game.releaseYear) {
			record.game.releaseYear = ""
		}
		if record.game.maxPlayers != "" && !validLaunchBoxMaxPlayers(record.game.maxPlayers) {
			record.game.maxPlayers = ""
		}
		if record.game.genres != "" {
			genres, ok := normalizeLaunchBoxGenres(record.game.genres)
			if !ok {
				return errors.New("launchbox Game.Genres is invalid")
			}
			record.game.genres = genres
		}
		if record.game.developer != "" && utf8.RuneCountInString(record.game.developer) > 256 {
			return errors.New("launchbox Game.Developer is invalid")
		}
		if record.game.publisher != "" && utf8.RuneCountInString(record.game.publisher) > 256 {
			return errors.New("launchbox Game.Publisher is invalid")
		}
		if record.game.series != "" && utf8.RuneCountInString(record.game.series) > 256 {
			return errors.New("launchbox Game.Series is invalid")
		}
	case "GameAlternateName":
		if !validLaunchBoxID(record.alias.databaseID) {
			return errors.New("launchbox alias DatabaseID is invalid")
		}
	case "GameImage":
		if !validLaunchBoxID(record.image.databaseID) {
			return errors.New("launchbox image DatabaseID is invalid")
		}
		if !validLaunchBoxImageFileName(record.image.fileName) || !validLaunchBoxImageType(record.image.typeName) || !validLaunchBoxCRC32(record.image.crc32) {
			return errors.New("launchbox image field is invalid")
		}
	case "Platform":
		if xmlBoundaryTrim(record.platform.name) == "" {
			return errors.New("launchbox platform name is empty")
		}
	case "PlatformAlternateName":
		if xmlBoundaryTrim(record.platformAlias.name) == "" || xmlBoundaryTrim(record.platformAlias.alternate) == "" {
			return errors.New("launchbox platform alternate is empty")
		}
	case "Emulator", "EmulatorPlatform":
		// Unsupported configuration families are still fully framed and counted.
	default:
		return errors.New("launchbox XML record family is unsupported")
	}
	return nil
}

type launchBoxSnapshotCounts struct {
	metadata  launchBoxMemberCounts
	platforms launchBoxMemberCounts
}

func parseLaunchBoxXMLMembers(metadataReader io.Reader, platformsReader io.Reader, table launchBoxRecordTable) (launchBoxSnapshotCounts, error) {
	var counts launchBoxSnapshotCounts
	if metadataReader == nil || platformsReader == nil || table == nil {
		return counts, errors.New("launchbox XML snapshot is unavailable")
	}
	batch, err := table.beginLaunchBoxBatch()
	if err != nil {
		return counts, err
	}
	if batch == nil {
		return counts, errors.New("launchbox XML batch is unavailable")
	}
	abort := func(primary error) (launchBoxSnapshotCounts, error) {
		if abortErr := batch.abortLaunchBoxBatch(); abortErr != nil {
			return counts, errors.Join(primary, abortErr)
		}
		return counts, primary
	}
	budget := &launchBoxXMLBudget{}
	counts.metadata, err = parseLaunchBoxXMLMember(metadataReader, "Metadata.xml", budget, batch)
	if err != nil {
		return abort(err)
	}
	if err := batch.completeLaunchBoxMember("Metadata.xml"); err != nil {
		return abort(err)
	}
	counts.platforms, err = parseLaunchBoxXMLMember(platformsReader, "Platforms.xml", budget, batch)
	if err != nil {
		return abort(err)
	}
	if err := batch.completeLaunchBoxMember("Platforms.xml"); err != nil {
		return abort(err)
	}
	if err := batch.validateLaunchBoxBatch(); err != nil {
		return abort(err)
	}
	if err := batch.flushLaunchBoxBatch(); err != nil {
		return abort(err)
	}
	if err := batch.commitLaunchBoxBatch(); err != nil {
		return abort(err)
	}
	return counts, nil
}

func (c *launchBoxMemberCounts) allow(family, member string) error {
	if !launchBoxFamilyAllowed(member, family) {
		return errors.New("launchbox XML record family is unsupported")
	}
	limit := 0
	count := 0
	switch family {
	case "Game":
		c.games++
		count, limit = c.games, launchBoxXMLMaxGames
	case "GameAlternateName":
		c.aliases++
		count, limit = c.aliases, launchBoxXMLMaxAliases
	case "GameImage":
		c.images++
		count, limit = c.images, launchBoxXMLMaxImages
	case "Platform":
		c.platforms++
		count, limit = c.platforms, launchBoxXMLMaxPlatforms
	case "PlatformAlternateName":
		c.platformAliases++
		count, limit = c.platformAliases, launchBoxXMLMaxPlatformAliases
	case "Emulator":
		c.emulators++
		count, limit = c.emulators, launchBoxXMLMaxEmulators
	case "EmulatorPlatform":
		c.emulatorPlatforms++
		count, limit = c.emulatorPlatforms, launchBoxXMLMaxEmulatorPlatforms
	default:
		return errors.New("launchbox XML record family is unsupported")
	}
	c.records++
	memberLimit := launchBoxXMLMaxMetadataRecords
	if member == "Platforms.xml" {
		memberLimit = launchBoxXMLMaxPlatformRecords
	}
	if count > limit || c.records > memberLimit {
		return fmt.Errorf("launchbox XML %s record limit exceeded", family)
	}
	return nil
}

func launchBoxFamilyAllowed(member, family string) bool {
	if member == "Platforms.xml" {
		return family == "Platform" || family == "PlatformAlternateName"
	}
	switch family {
	case "Game", "GameAlternateName", "GameImage", "Platform", "PlatformAlternateName", "Emulator", "EmulatorPlatform":
		return true
	default:
		return false
	}
}

func parseLaunchBoxRecord(decoder *xml.Decoder, start xml.StartElement, member string) (launchBoxRecord, error) {
	record := launchBoxRecord{family: start.Name.Local}
	var seen uint64
	for {
		token, err := decoder.Token()
		if err != nil {
			return launchBoxRecord{}, errors.New("launchbox XML record is truncated")
		}
		switch current := token.(type) {
		case xml.StartElement:
			if current.Name.Space != "" {
				return launchBoxRecord{}, errors.New("launchbox XML element namespace is not allowed")
			}
			if err := validateLaunchBoxAttributes(current, member, start.Name.Local); err != nil {
				return launchBoxRecord{}, err
			}
			bit := launchBoxSelectedFieldBit(start.Name.Local, current.Name.Local)
			if bit != 0 {
				if seen&bit != 0 {
					return launchBoxRecord{}, errors.New("launchbox XML selected field is duplicated")
				}
				seen |= bit
			}
			value, err := parseLaunchBoxLeaf(decoder, current, member, start.Name.Local)
			if err != nil {
				return launchBoxRecord{}, err
			}
			assignLaunchBoxField(&record, start.Name.Local, current.Name.Local, value)
		case xml.EndElement:
			if current.Name != start.Name {
				return launchBoxRecord{}, errors.New("launchbox XML record end does not match")
			}
			if err := validateLaunchBoxRequiredFields(start.Name.Local, seen, record); err != nil {
				return launchBoxRecord{}, err
			}
			return record, nil
		case xml.CharData:
			if !xmlWhitespaceOnly(current) {
				return launchBoxRecord{}, errors.New("launchbox XML record has mixed content")
			}
		case xml.Directive:
			return launchBoxRecord{}, errors.New("launchbox XML record contains a directive")
		case xml.ProcInst:
			return launchBoxRecord{}, errors.New("launchbox XML record contains a processing instruction")
		case xml.Comment:
			continue
		default:
			return launchBoxRecord{}, errors.New("launchbox XML record contains unsupported token")
		}
	}
}

func parseLaunchBoxLeaf(decoder *xml.Decoder, start xml.StartElement, member, family string) (string, error) {
	if err := validateLaunchBoxAttributes(start, member, family); err != nil {
		return "", err
	}
	selected, maxBytes, maxRunes := launchBoxFieldLimit(family, start.Name.Local)
	builder := launchBoxTextBuilder{maxBytes: maxBytes, maxRunes: maxRunes}
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", errors.New("launchbox XML leaf is truncated")
		}
		switch current := token.(type) {
		case xml.StartElement:
			return "", errors.New("launchbox XML records cannot contain grandchildren")
		case xml.EndElement:
			if current.Name != start.Name {
				return "", errors.New("launchbox XML leaf end does not match")
			}
			if !selected {
				return "", nil
			}
			return builder.String(), nil
		case xml.CharData:
			if selected {
				if err := builder.Append([]byte(current)); err != nil {
					return "", err
				}
			}
		case xml.Comment:
			continue
		default:
			return "", errors.New("launchbox XML leaf contains unsupported token")
		}
	}
}

func validateLaunchBoxAttributes(start xml.StartElement, member, family string) error {
	if len(start.Attr) == 0 {
		return nil
	}
	if len(start.Attr) != 1 || member != "Metadata.xml" || family != "GameAlternateName" || start.Name.Local != "AlternateName" {
		return errors.New("launchbox XML attribute is outside the permitted path")
	}
	attribute := start.Attr[0]
	if attribute.Name.Space != "http://www.w3.org/XML/1998/namespace" || attribute.Name.Local != "space" || attribute.Value != "preserve" {
		return errors.New("launchbox XML xml:space attribute is invalid")
	}
	return nil
}

func launchBoxSelectedFieldBit(family, field string) uint64 {
	fields := []string{}
	switch family {
	case "Game":
		fields = []string{"DatabaseID", "Name", "Platform", "Overview", "ReleaseYear", "Genres", "Developer", "Publisher", "MaxPlayers", "Series"}
	case "GameAlternateName":
		fields = []string{"DatabaseID", "AlternateName", "Region"}
	case "GameImage":
		fields = []string{"DatabaseID", "FileName", "Type", "Region", "CRC32"}
	case "Platform":
		fields = []string{"Name"}
	case "PlatformAlternateName":
		fields = []string{"Name", "Alternate"}
	default:
		return 0
	}
	for index, candidate := range fields {
		if candidate == field {
			return uint64(1) << index
		}
	}
	return 0
}

func launchBoxFieldLimit(family, field string) (bool, int, int) {
	switch family {
	case "Game":
		switch field {
		case "DatabaseID":
			return true, 19, 19
		case "Name":
			return true, 1024, 256
		case "Platform":
			return true, 256, 128
		case "Overview":
			return true, 65536, 16384
		case "ReleaseYear":
			return true, 4, 4
		case "Genres":
			return true, 4096, 1024
		case "Developer", "Publisher":
			return true, 1024, 256
		case "MaxPlayers":
			return true, 32, 32
		case "Series":
			return true, 1024, 256
		}
	case "GameAlternateName":
		switch field {
		case "DatabaseID":
			return true, 19, 19
		case "AlternateName":
			return true, 1024, 256
		case "Region":
			return true, 128, 64
		}
	case "GameImage":
		switch field {
		case "DatabaseID":
			return true, 19, 19
		case "FileName":
			return true, 44, 44
		case "Type":
			return true, 128, 64
		case "Region":
			return true, 128, 64
		case "CRC32":
			return true, 10, 10
		}
	case "Platform":
		if field == "Name" {
			return true, 256, 128
		}
	case "PlatformAlternateName":
		switch field {
		case "Name", "Alternate":
			return true, 256, 128
		}
	}
	return false, 0, 0
}

func validateLaunchBoxRequiredFields(family string, seen uint64, record launchBoxRecord) error {
	required := func(fields ...string) bool {
		for _, field := range fields {
			if seen&launchBoxSelectedFieldBit(family, field) == 0 {
				return false
			}
		}
		return true
	}
	switch family {
	case "Game":
		if !required("DatabaseID", "Name", "Platform") {
			return errors.New("launchbox Game required field is missing")
		}
	case "GameAlternateName":
		if !required("DatabaseID") {
			return errors.New("launchbox alias DatabaseID is missing")
		}
	case "GameImage":
		if !required("DatabaseID", "FileName", "Type", "CRC32") {
			return errors.New("launchbox image required field is missing")
		}
	case "Platform":
		if !required("Name") {
			return errors.New("launchbox platform name is missing")
		}
	case "PlatformAlternateName":
		if !required("Name", "Alternate") {
			return errors.New("launchbox platform alternate field is missing")
		}
	}
	return nil
}

func assignLaunchBoxField(record *launchBoxRecord, family, field, value string) {
	switch family {
	case "Game":
		switch field {
		case "DatabaseID":
			record.game.databaseID = value
		case "Name":
			record.game.name = value
		case "Platform":
			record.game.platform = value
		case "Overview":
			record.game.overview = value
		case "ReleaseYear":
			record.game.releaseYear = value
		case "Genres":
			record.game.genres = value
		case "Developer":
			record.game.developer = value
		case "Publisher":
			record.game.publisher = value
		case "MaxPlayers":
			record.game.maxPlayers = value
		case "Series":
			record.game.series = value
		}
	case "GameAlternateName":
		switch field {
		case "DatabaseID":
			record.alias.databaseID = value
		case "AlternateName":
			record.alias.alternateName = value
		case "Region":
			record.alias.region = value
		}
	case "GameImage":
		switch field {
		case "DatabaseID":
			record.image.databaseID = value
		case "FileName":
			record.image.fileName = value
		case "Type":
			record.image.typeName = value
		case "Region":
			record.image.region = value
		case "CRC32":
			record.image.crc32 = value
		}
	case "Platform":
		if field == "Name" {
			record.platform.name = value
		}
	case "PlatformAlternateName":
		if field == "Name" {
			record.platformAlias.name = value
		} else if field == "Alternate" {
			record.platformAlias.alternate = value
		}
	}
}
