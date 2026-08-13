package metadata

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

type launchBoxFixtureEntry struct {
	name   string
	body   []byte
	extra  []byte
	method uint16
	mode   os.FileMode
	flags  uint16
}

type launchBoxCommentArchiveFixture struct {
	entries []launchBoxFixtureEntry
	comment string
}

type launchBoxArchiveReadGuard struct {
	source        io.ReaderAt
	blockedOffset int64
	blockedReads  int
	block         bool
}

func (r *launchBoxArchiveReadGuard) ReadAt(dst []byte, offset int64) (int, error) {
	if r.block && offset == r.blockedOffset {
		r.blockedReads++
		return 0, io.ErrUnexpectedEOF
	}
	return r.source.ReadAt(dst, offset)
}

func buildLaunchBoxArchive(t *testing.T, entries []launchBoxFixtureEntry) []byte {
	return buildLaunchBoxArchiveWithComment(t, launchBoxCommentArchiveFixture{entries: entries})
}

func buildLaunchBoxArchiveWithComment(t *testing.T, fixture launchBoxCommentArchiveFixture) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range fixture.entries {
		header := &zip.FileHeader{Name: entry.name, Method: entry.method, Flags: entry.flags}
		header.Extra = entry.extra
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		member, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create ZIP member %q: %v", entry.name, err)
		}
		if _, err := member.Write(entry.body); err != nil {
			t.Fatalf("write ZIP member %q: %v", entry.name, err)
		}
	}
	if err := writer.SetComment(fixture.comment); err != nil {
		t.Fatalf("set ZIP comment: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP writer: %v", err)
	}
	return output.Bytes()
}

func validLaunchBoxFixtureEntries() []launchBoxFixtureEntry {
	return []launchBoxFixtureEntry{
		{name: "Metadata.xml", body: []byte("metadata"), method: zip.Store},
		{name: "Platforms.xml", body: []byte("platforms"), method: zip.Store},
		{name: "Mame.xml", body: []byte("mame"), method: zip.Store},
		{name: "Files.xml", body: []byte("files"), method: zip.Store},
	}
}

func buildLaunchBoxArchiveWithoutDataDescriptors(t *testing.T, entries []launchBoxFixtureEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		header := &zip.FileHeader{
			Name:               entry.name,
			Method:             entry.method,
			Flags:              entry.flags,
			CRC32:              crc32.ChecksumIEEE(entry.body),
			CompressedSize64:   uint64(len(entry.body)),
			UncompressedSize64: uint64(len(entry.body)),
			Extra:              entry.extra,
		}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		member, err := writer.CreateRaw(header)
		if err != nil {
			t.Fatalf("create raw ZIP member %q: %v", entry.name, err)
		}
		if _, err := member.Write(entry.body); err != nil {
			t.Fatalf("write raw ZIP member %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close raw ZIP writer: %v", err)
	}
	return output.Bytes()
}

func buildLaunchBoxDataDescriptorArchive(t *testing.T, target string, zip64, signed bool) []byte {
	t.Helper()
	type record struct {
		entry       launchBoxFixtureEntry
		localOffset uint32
		crc         uint32
	}
	entries := validLaunchBoxFixtureEntries()
	records := make([]record, 0, len(entries))
	archive := make([]byte, 0, 1024)
	for _, entry := range entries {
		if len(archive) > math.MaxUint32 {
			t.Fatal("descriptor fixture local offset exceeds ZIP32")
		}
		record := record{entry: entry, localOffset: uint32(len(archive)), crc: crc32.ChecksumIEEE(entry.body)}
		header := make([]byte, 30)
		binary.LittleEndian.PutUint32(header[:4], 0x04034b50)
		binary.LittleEndian.PutUint16(header[4:6], 20)
		binary.LittleEndian.PutUint16(header[6:8], 8)
		binary.LittleEndian.PutUint16(header[8:10], entry.method)
		binary.LittleEndian.PutUint16(header[26:28], uint16(len(entry.name)))
		binary.LittleEndian.PutUint16(header[28:30], uint16(len(entry.extra)))
		archive = append(archive, header...)
		archive = append(archive, entry.name...)
		archive = append(archive, entry.extra...)
		archive = append(archive, entry.body...)
		useZIP64Descriptor := entry.name == target && zip64
		useSignature := entry.name != target || signed
		if useSignature {
			signature := make([]byte, 4)
			binary.LittleEndian.PutUint32(signature, 0x08074b50)
			archive = append(archive, signature...)
		}
		crcBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(crcBytes, record.crc)
		archive = append(archive, crcBytes...)
		if useZIP64Descriptor {
			sizes := make([]byte, 16)
			binary.LittleEndian.PutUint64(sizes[:8], uint64(len(entry.body)))
			binary.LittleEndian.PutUint64(sizes[8:], uint64(len(entry.body)))
			archive = append(archive, sizes...)
		} else {
			sizes := make([]byte, 8)
			binary.LittleEndian.PutUint32(sizes[:4], uint32(len(entry.body)))
			binary.LittleEndian.PutUint32(sizes[4:], uint32(len(entry.body)))
			archive = append(archive, sizes...)
		}
		records = append(records, record)
	}
	centralStart := len(archive)
	for _, record := range records {
		entry := record.entry
		central := make([]byte, 46)
		binary.LittleEndian.PutUint32(central[:4], 0x02014b50)
		binary.LittleEndian.PutUint16(central[4:6], 20)
		binary.LittleEndian.PutUint16(central[6:8], 20)
		binary.LittleEndian.PutUint16(central[8:10], 8)
		binary.LittleEndian.PutUint16(central[10:12], entry.method)
		binary.LittleEndian.PutUint32(central[16:20], record.crc)
		isZIP64 := entry.name == target && zip64
		if isZIP64 {
			binary.LittleEndian.PutUint32(central[20:24], math.MaxUint32)
			binary.LittleEndian.PutUint32(central[24:28], math.MaxUint32)
		} else {
			binary.LittleEndian.PutUint32(central[20:24], uint32(len(entry.body)))
			binary.LittleEndian.PutUint32(central[24:28], uint32(len(entry.body)))
		}
		binary.LittleEndian.PutUint16(central[28:30], uint16(len(entry.name)))
		extra := entry.extra
		if isZIP64 {
			extra = make([]byte, 20)
			binary.LittleEndian.PutUint16(extra[:2], 1)
			binary.LittleEndian.PutUint16(extra[2:4], 16)
			binary.LittleEndian.PutUint64(extra[4:12], uint64(len(entry.body)))
			binary.LittleEndian.PutUint64(extra[12:20], uint64(len(entry.body)))
		}
		binary.LittleEndian.PutUint16(central[30:32], uint16(len(extra)))
		binary.LittleEndian.PutUint32(central[42:46], record.localOffset)
		archive = append(archive, central...)
		archive = append(archive, entry.name...)
		archive = append(archive, extra...)
	}
	centralSize := len(archive) - centralStart
	eocd := make([]byte, 22)
	binary.LittleEndian.PutUint32(eocd[:4], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[8:10], uint16(len(records)))
	binary.LittleEndian.PutUint16(eocd[10:12], uint16(len(records)))
	binary.LittleEndian.PutUint32(eocd[12:16], uint32(centralSize))
	binary.LittleEndian.PutUint32(eocd[16:20], uint32(centralStart))
	archive = append(archive, eocd...)
	return archive
}

func patchLaunchBoxDataDescriptorValueForForm(t *testing.T, archive []byte, name string, zip64, signed bool, field string) {
	t.Helper()
	localOffset := launchBoxArchiveLocalHeaderOffset(t, archive, name)
	nameLength := int(binary.LittleEndian.Uint16(archive[localOffset+26 : localOffset+28]))
	extraLength := int(binary.LittleEndian.Uint16(archive[localOffset+28 : localOffset+30]))
	dataOffset := localOffset + 30 + nameLength + extraLength
	bodyLength := map[string]int{"Metadata.xml": 8, "Platforms.xml": 8, "Mame.xml": 4, "Files.xml": 5}[name]
	width := 4
	if zip64 {
		width = 8
	}
	descriptorOffset := dataOffset + bodyLength
	if signed {
		if descriptorOffset+4 > len(archive) || binary.LittleEndian.Uint32(archive[descriptorOffset:descriptorOffset+4]) != 0x08074b50 {
			t.Fatalf("signed data descriptor for %q not found at %d", name, descriptorOffset)
		}
		descriptorOffset += 4
	}
	if descriptorOffset+4+2*width > len(archive) {
		t.Fatalf("truncated data descriptor for %q", name)
	}
	fieldOffset := descriptorOffset
	switch field {
	case "compressed":
		fieldOffset += 4
	case "expanded":
		fieldOffset += 4 + width
	case "crc":
	default:
		t.Fatalf("unknown data descriptor field %q", field)
	}
	if field == "crc" {
		binary.LittleEndian.PutUint32(archive[fieldOffset:fieldOffset+4], ^binary.LittleEndian.Uint32(archive[fieldOffset:fieldOffset+4]))
	} else if width == 4 {
		binary.LittleEndian.PutUint32(archive[fieldOffset:fieldOffset+4], binary.LittleEndian.Uint32(archive[fieldOffset:fieldOffset+4])+1)
	} else {
		binary.LittleEndian.PutUint64(archive[fieldOffset:fieldOffset+8], binary.LittleEndian.Uint64(archive[fieldOffset:fieldOffset+8])+1)
	}
}

func patchLaunchBoxCentralEntry(t *testing.T, archive []byte, name string, compressed, expanded uint64, crc *uint32) {
	t.Helper()
	if compressed > math.MaxUint32 || expanded > math.MaxUint32 {
		t.Fatalf("fixture central-directory patch exceeds non-ZIP64 helper: compressed=%d expanded=%d", compressed, expanded)
	}
	for offset := 0; offset+46 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(archive[offset+32 : offset+34]))
		end := offset + 46 + nameLength + extraLength + commentLength
		if end > len(archive) {
			t.Fatalf("truncated central-directory fixture while finding %q", name)
		}
		if string(archive[offset+46:offset+46+nameLength]) != name {
			continue
		}
		binary.LittleEndian.PutUint32(archive[offset+20:offset+24], uint32(compressed))
		binary.LittleEndian.PutUint32(archive[offset+24:offset+28], uint32(expanded))
		if crc != nil {
			binary.LittleEndian.PutUint32(archive[offset+16:offset+20], *crc)
		}
		return
	}
	t.Fatalf("central-directory member %q not found", name)
}

func patchLaunchBoxCentralMethod(t *testing.T, archive []byte, name string, method uint16) {
	t.Helper()
	for offset := 0; offset+46 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(archive[offset+32 : offset+34]))
		end := offset + 46 + nameLength + extraLength + commentLength
		if end > len(archive) {
			t.Fatalf("truncated central-directory fixture while finding %q", name)
		}
		if string(archive[offset+46:offset+46+nameLength]) != name {
			continue
		}
		binary.LittleEndian.PutUint16(archive[offset+10:offset+12], method)
		return
	}
	t.Fatalf("central-directory member %q not found", name)
}

func patchLaunchBoxLocalSignature(t *testing.T, archive []byte, name string, signature [4]byte) {
	t.Helper()
	for offset := 0; offset+30 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+26 : offset+28]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		end := offset + 30 + nameLength + extraLength
		if end > len(archive) {
			t.Fatalf("truncated local-header fixture while finding %q", name)
		}
		if string(archive[offset+30:offset+30+nameLength]) != name {
			continue
		}
		copy(archive[offset:offset+4], signature[:])
		return
	}
	t.Fatalf("local-header member %q not found", name)
}

func patchLaunchBoxDecoyLocalHeader(t *testing.T, archive []byte, name string) {
	t.Helper()
	localOffset := -1
	for offset := 0; offset+30 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+26 : offset+28]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		end := offset + 30 + nameLength + extraLength
		if end <= len(archive) && string(archive[offset+30:offset+30+nameLength]) == name {
			if extraLength != 30+nameLength {
				t.Fatalf("unexpected extra length for %q: %d", name, extraLength)
			}
			localOffset = offset
			break
		}
	}
	if localOffset < 0 {
		t.Fatalf("local-header member %q not found", name)
	}

	centralOffset := -1
	for offset := 0; offset+46 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(archive[offset+32 : offset+34]))
		end := offset + 46 + nameLength + extraLength + commentLength
		if end <= len(archive) && string(archive[offset+46:offset+46+nameLength]) == name {
			centralOffset = offset
			break
		}
	}
	if centralOffset < 0 {
		t.Fatalf("central-directory member %q not found", name)
	}

	nameLength := int(binary.LittleEndian.Uint16(archive[localOffset+26 : localOffset+28]))
	fakeOffset := localOffset + 30 + nameLength
	copy(archive[fakeOffset:fakeOffset+4], []byte{'P', 'K', 3, 4})
	copy(archive[fakeOffset+6:fakeOffset+8], archive[centralOffset+8:centralOffset+10])
	copy(archive[fakeOffset+8:fakeOffset+10], archive[centralOffset+10:centralOffset+12])
	copy(archive[fakeOffset+10:fakeOffset+12], archive[centralOffset+12:centralOffset+14])
	copy(archive[fakeOffset+12:fakeOffset+14], archive[centralOffset+14:centralOffset+16])
	copy(archive[fakeOffset+14:fakeOffset+18], archive[centralOffset+16:centralOffset+20])
	copy(archive[fakeOffset+18:fakeOffset+22], archive[centralOffset+20:centralOffset+24])
	copy(archive[fakeOffset+22:fakeOffset+26], archive[centralOffset+24:centralOffset+28])
	binary.LittleEndian.PutUint16(archive[fakeOffset+26:fakeOffset+28], uint16(nameLength))
	binary.LittleEndian.PutUint16(archive[fakeOffset+28:fakeOffset+30], 0)
	copy(archive[fakeOffset+30:fakeOffset+30+nameLength], name)

	// The real local method is malformed while the central entry remains valid.
	binary.LittleEndian.PutUint16(archive[localOffset+8:localOffset+10], 99)
}

func launchBoxArchiveLocalHeaderOffset(t *testing.T, archive []byte, name string) int {
	t.Helper()
	for offset := 0; offset+30 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+26 : offset+28]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		end := offset + 30 + nameLength + extraLength
		if end <= len(archive) && string(archive[offset+30:offset+30+nameLength]) == name {
			return offset
		}
	}
	t.Fatalf("local-header member %q not found", name)
	return -1
}

func launchBoxArchiveCentralEntryOffset(t *testing.T, archive []byte, name string) int {
	t.Helper()
	for offset := 0; offset+46 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(archive[offset+32 : offset+34]))
		end := offset + 46 + nameLength + extraLength + commentLength
		if end <= len(archive) && string(archive[offset+46:offset+46+nameLength]) == name {
			return offset
		}
	}
	t.Fatalf("central-directory member %q not found", name)
	return -1
}

func patchLaunchBoxCentralOffsetToNestedDecoy(t *testing.T, archive []byte, name string) {
	t.Helper()
	patchLaunchBoxDecoyLocalHeader(t, archive, name)
	localOffset := launchBoxArchiveLocalHeaderOffset(t, archive, name)
	nameLength := int(binary.LittleEndian.Uint16(archive[localOffset+26 : localOffset+28]))
	fakeOffset := localOffset + 30 + nameLength
	centralOffset := launchBoxArchiveCentralEntryOffset(t, archive, name)
	if uint64(fakeOffset) > math.MaxUint32 {
		t.Fatalf("nested decoy offset exceeds non-ZIP64 fixture: %d", fakeOffset)
	}
	binary.LittleEndian.PutUint32(archive[centralOffset+42:centralOffset+46], uint32(fakeOffset))
}

func patchLaunchBoxCentralCompressedSizeToOverlapNextLocalRecord(t *testing.T, archive []byte, name string) {
	t.Helper()
	localOffset := launchBoxArchiveLocalHeaderOffset(t, archive, name)
	nameLength := int(binary.LittleEndian.Uint16(archive[localOffset+26 : localOffset+28]))
	extraLength := int(binary.LittleEndian.Uint16(archive[localOffset+28 : localOffset+30]))
	dataOffset := localOffset + 30 + nameLength + extraLength
	nextLocalOffset := -1
	for offset := dataOffset; offset+4 <= len(archive); offset++ {
		if bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			nextLocalOffset = offset
			break
		}
	}
	if nextLocalOffset < 0 || nextLocalOffset <= dataOffset {
		t.Fatalf("next local-header record after %q not found", name)
	}
	patchLaunchBoxCentralEntry(t, archive, name, uint64(nextLocalOffset-dataOffset+1), 0, nil)
}

func patchLaunchBoxMemberWithoutDataDescriptor(t *testing.T, archive []byte, name string, crc uint32) {
	t.Helper()
	var compressed, expanded uint32
	centralFound := false
	for offset := 0; offset+46 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+30 : offset+32]))
		commentLength := int(binary.LittleEndian.Uint16(archive[offset+32 : offset+34]))
		end := offset + 46 + nameLength + extraLength + commentLength
		if end > len(archive) {
			t.Fatalf("truncated central-directory fixture while finding %q", name)
		}
		if string(archive[offset+46:offset+46+nameLength]) != name {
			continue
		}
		binary.LittleEndian.PutUint16(archive[offset+8:offset+10], 0)
		binary.LittleEndian.PutUint32(archive[offset+16:offset+20], crc)
		compressed = binary.LittleEndian.Uint32(archive[offset+20 : offset+24])
		expanded = binary.LittleEndian.Uint32(archive[offset+24 : offset+28])
		centralFound = true
		break
	}
	if !centralFound {
		t.Fatalf("central-directory member %q not found", name)
	}
	for offset := 0; offset+30 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+26 : offset+28]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		end := offset + 30 + nameLength + extraLength
		if end > len(archive) {
			t.Fatalf("truncated local-header fixture while finding %q", name)
		}
		if string(archive[offset+30:offset+30+nameLength]) != name {
			continue
		}
		binary.LittleEndian.PutUint16(archive[offset+6:offset+8], 0)
		binary.LittleEndian.PutUint32(archive[offset+14:offset+18], crc)
		binary.LittleEndian.PutUint32(archive[offset+18:offset+22], compressed)
		binary.LittleEndian.PutUint32(archive[offset+22:offset+26], expanded)
		return
	}
	t.Fatalf("local-header member %q not found", name)
}

func assertLaunchBoxArchiveInvalid(t *testing.T, archive []byte) {
	t.Helper()
	opened, err := openLaunchBoxArchive(bytes.NewReader(archive), int64(len(archive)))
	if opened != nil || opCode(err) != ErrInvalidResponse {
		t.Fatalf("openLaunchBoxArchive = archive:%v err:%v, want invalid_response and nil archive", opened, err)
	}
}

func launchBoxArchiveDataOffset(t *testing.T, archive []byte, name string) int {
	t.Helper()
	localOffset := launchBoxArchiveLocalHeaderOffset(t, archive, name)
	nameLength := int(binary.LittleEndian.Uint16(archive[localOffset+26 : localOffset+28]))
	extraLength := int(binary.LittleEndian.Uint16(archive[localOffset+28 : localOffset+30]))
	return localOffset + 30 + nameLength + extraLength
}

func TestLaunchBoxArchivePreflightAdmitsRequiredMembersAndStreamsAfterAdmission(t *testing.T) {
	archiveBytes := buildLaunchBoxArchive(t, []launchBoxFixtureEntry{
		{name: "Metadata.xml", body: []byte("metadata"), method: zip.Store},
		{name: "Platforms.xml", body: []byte("platforms"), method: zip.Store},
	})

	archive, err := openLaunchBoxArchive(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("openLaunchBoxArchive: %v", err)
	}
	reader, err := archive.OpenMember("Metadata.xml")
	if err != nil {
		t.Fatalf("OpenMember: %v", err)
	}
	body, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read/close member: read=%v close=%v", err, closeErr)
	}
	if string(body) != "metadata" {
		t.Fatalf("member body = %q", body)
	}
	for _, ignored := range []string{"Mame.xml", "Files.xml"} {
		if _, err := archive.OpenMember(ignored); opCode(err) != ErrInvalidResponse {
			t.Fatalf("OpenMember(%q) error = %v, want invalid_response", ignored, err)
		}
	}
}

func TestLaunchBoxArchiveAdmitsIgnoredMembersOnlyAfterTheSamePreflight(t *testing.T) {
	for _, ignored := range []string{"Mame.xml", "Files.xml"} {
		t.Run(ignored, func(t *testing.T) {
			archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
			patchLaunchBoxCentralEntry(t, archive, ignored, 1, 33, nil)
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func TestLaunchBoxArchiveRatioUsesInclusiveOverflowSafeIntegerRule(t *testing.T) {
	tests := []struct {
		name       string
		expanded   uint64
		compressed uint64
		want       bool
	}{
		{name: "empty zero compressed", expanded: 0, compressed: 0, want: true},
		{name: "zero compressed nonempty", expanded: 1, compressed: 0},
		{name: "exact maximum", expanded: 32, compressed: 1, want: true},
		{name: "maximum plus one", expanded: 33, compressed: 1},
		{name: "large exact maximum", expanded: (math.MaxUint64 / 32) * 32, compressed: math.MaxUint64 / 32, want: true},
		{name: "multiplication overflow adversary", expanded: math.MaxUint64, compressed: math.MaxUint64 / 32},
		{name: "large quotient below maximum", expanded: math.MaxUint64, compressed: math.MaxUint64 / 31, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validLaunchBoxArchiveRatio(test.expanded, test.compressed); got != test.want {
				t.Fatalf("validLaunchBoxArchiveRatio(%d, %d) = %v, want %v", test.expanded, test.compressed, got, test.want)
			}
		})
	}
}

func TestLaunchBoxArchiveCentralDirectoryRatioBoundaries(t *testing.T) {
	tests := []struct {
		name              string
		fixtureCompressed uint64
		fixtureExpanded   uint64
		want              bool
	}{
		{name: "exact maximum", fixtureCompressed: 8, fixtureExpanded: 256, want: true},
		{name: "maximum plus one", fixtureCompressed: 8, fixtureExpanded: 264},
		{name: "zero compressed nonempty", fixtureCompressed: 0, fixtureExpanded: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
			patchLaunchBoxCentralEntry(t, archive, "Metadata.xml", test.fixtureCompressed, test.fixtureExpanded, nil)
			if test.fixtureCompressed != 0 {
				patchLaunchBoxDataDescriptorSizes(t, archive, "Metadata.xml", test.fixtureCompressed, test.fixtureExpanded)
			}
			opened, err := openLaunchBoxArchive(bytes.NewReader(archive), int64(len(archive)))
			if test.want {
				if err != nil || opened == nil {
					t.Fatalf("exact ratio rejected: archive=%v err=%v", opened, err)
				}
				return
			}
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func patchLaunchBoxDataDescriptorSizes(t *testing.T, archive []byte, name string, compressed, expanded uint64) {
	t.Helper()
	if compressed > math.MaxUint32 || expanded > math.MaxUint32 {
		t.Fatalf("fixture data descriptor exceeds 32-bit form: compressed=%d expanded=%d", compressed, expanded)
	}
	dataOffset := launchBoxArchiveDataOffset(t, archive, name)
	descriptorOffset := dataOffset + int(compressed)
	if descriptorOffset+16 > len(archive) || binary.LittleEndian.Uint32(archive[descriptorOffset:descriptorOffset+4]) != 0x08074b50 {
		t.Fatalf("signed data descriptor for %q not found at %d", name, descriptorOffset)
	}
	binary.LittleEndian.PutUint32(archive[descriptorOffset+8:descriptorOffset+12], uint32(compressed))
	binary.LittleEndian.PutUint32(archive[descriptorOffset+12:descriptorOffset+16], uint32(expanded))
}

func TestLaunchBoxArchiveAggregateRatioBoundaries(t *testing.T) {
	for _, test := range []struct {
		name       string
		expanded   uint64
		compressed uint64
		want       bool
	}{
		{name: "exact maximum", expanded: 4 * 32, compressed: 4, want: true},
		{name: "maximum plus one", expanded: 4*32 + 1, compressed: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validLaunchBoxArchiveRatio(test.expanded, test.compressed); got != test.want {
				t.Fatalf("aggregate ratio (%d, %d) = %v, want %v", test.expanded, test.compressed, got, test.want)
			}
		})
	}
}

func TestLaunchBoxArchiveIndependentByteCaps(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, []byte)
	}{
		{
			name: "compressed archive source maximum plus one",
			mutate: func(t *testing.T, archive []byte) {
				t.Helper()
				assertLaunchBoxArchiveInvalidWithSize(t, archive, int64(launchBoxArchiveMaxCompressedBytes)+1)
			},
		},
		{
			name: "member compressed maximum plus one",
			mutate: func(t *testing.T, archive []byte) {
				patchLaunchBoxCentralEntry(t, archive, "Metadata.xml", launchBoxArchiveMaxCompressedBytes+1, 0, nil)
			},
		},
		{
			name: "member expanded maximum plus one",
			mutate: func(t *testing.T, archive []byte) {
				compressed := (launchBoxArchiveMaxMemberBytes + 31) / 32
				patchLaunchBoxCentralEntry(t, archive, "Metadata.xml", compressed, launchBoxArchiveMaxMemberBytes+1, nil)
			},
		},
		{
			name: "aggregate compressed maximum plus one",
			mutate: func(t *testing.T, archive []byte) {
				for _, name := range []string{"Metadata.xml", "Platforms.xml", "Mame.xml", "Files.xml"} {
					patchLaunchBoxCentralEntry(t, archive, name, (launchBoxArchiveMaxCompressedBytes/4)+1, 0, nil)
				}
			},
		},
		{
			name: "aggregate expanded maximum plus one",
			mutate: func(t *testing.T, archive []byte) {
				patchLaunchBoxCentralEntry(t, archive, "Metadata.xml", 64<<20, launchBoxArchiveMaxMemberBytes, nil)
				patchLaunchBoxCentralEntry(t, archive, "Platforms.xml", 32<<20, (256<<20)+1, nil)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
			test.mutate(t, archive)
			if test.name == "compressed archive source maximum plus one" {
				return
			}
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func assertLaunchBoxArchiveInvalidWithSize(t *testing.T, archive []byte, size int64) {
	t.Helper()
	opened, err := openLaunchBoxArchive(bytes.NewReader(archive), size)
	if opened != nil || opCode(err) != ErrInvalidResponse {
		t.Fatalf("openLaunchBoxArchive with source size %d = archive:%v err:%v", size, opened, err)
	}
}

func TestLaunchBoxArchiveRejectsEntryCountAndUnknownMemberAuthority(t *testing.T) {
	entries := validLaunchBoxFixtureEntries()
	entries = append(entries,
		launchBoxFixtureEntry{name: "Extra-1.xml", body: []byte("extra"), method: zip.Store},
		launchBoxFixtureEntry{name: "Extra-2.xml", body: []byte("extra"), method: zip.Store},
		launchBoxFixtureEntry{name: "Extra-3.xml", body: []byte("extra"), method: zip.Store},
		launchBoxFixtureEntry{name: "Extra-4.xml", body: []byte("extra"), method: zip.Store},
	)
	assertLaunchBoxArchiveInvalid(t, buildLaunchBoxArchive(t, entries))

	entries = append(entries, launchBoxFixtureEntry{name: "Extra-5.xml", body: []byte("extra"), method: zip.Store})
	assertLaunchBoxArchiveInvalid(t, buildLaunchBoxArchive(t, entries))
}

func TestLaunchBoxArchiveRejectsDeclaredEntryCountBeforeCentralDirectoryPreflight(t *testing.T) {
	archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	centralDirectoryOffset := int64(-1)
	for offset := 0; offset+4 <= len(archive); offset++ {
		if bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			centralDirectoryOffset = int64(offset)
			break
		}
	}
	if centralDirectoryOffset < 0 {
		t.Fatal("central directory not found")
	}
	eocdFound := false
	for offset := len(archive) - 22; offset >= 0; offset-- {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 5, 6}) {
			continue
		}
		binary.LittleEndian.PutUint16(archive[offset+8:offset+10], launchBoxArchiveMaxMembers+1)
		binary.LittleEndian.PutUint16(archive[offset+10:offset+12], launchBoxArchiveMaxMembers+1)
		eocdFound = true
		break
	}
	if !eocdFound {
		t.Fatal("end of central directory not found")
	}
	guard := &launchBoxArchiveReadGuard{source: bytes.NewReader(archive), blockedOffset: centralDirectoryOffset, block: true}
	opened, err := openLaunchBoxArchive(guard, int64(len(archive)))
	if opened != nil || opCode(err) != ErrInvalidResponse {
		t.Fatalf("openLaunchBoxArchive = archive:%v err:%v, want invalid_response and nil archive", opened, err)
	}
	if guard.blockedReads != 0 {
		t.Fatalf("central directory was read %d time(s) before rejecting its declared count", guard.blockedReads)
	}
}

func TestLaunchBoxArchiveExposesOnlyRequiredMembers(t *testing.T) {
	archiveBytes := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	archive, err := openLaunchBoxArchive(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("openLaunchBoxArchive: %v", err)
	}
	for _, name := range []string{"Metadata.xml", "Platforms.xml"} {
		reader, err := archive.OpenMember(name)
		if err != nil {
			t.Fatalf("OpenMember(%q): %v", name, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("Close(%q): %v", name, err)
		}
	}
	for _, name := range []string{"Mame.xml", "Files.xml"} {
		if _, err := archive.OpenMember(name); opCode(err) != ErrInvalidResponse {
			t.Fatalf("OpenMember(%q) error = %v, want invalid_response", name, err)
		}
	}
}

func TestLaunchBoxArchiveRejectsPathAndMemberBounds(t *testing.T) {
	for _, name := range []string{
		"",
		"/Metadata.xml",
		"../Metadata.xml",
		"Metadata.xml/child",
		`Metadata\\xml`,
		"Metadata.xml?query",
		"Metadata.xml#fragment",
		strings.Repeat("a", launchBoxArchiveMaxMemberNameBytes+1),
		"Metadata.xml." + strings.Repeat("a", launchBoxArchiveMaxMemberNameBytes),
	} {
		t.Run(name, func(t *testing.T) {
			entries := validLaunchBoxFixtureEntries()
			entries = append(entries, launchBoxFixtureEntry{name: name, body: []byte("bad"), method: zip.Store})
			assertLaunchBoxArchiveInvalid(t, buildLaunchBoxArchive(t, entries))
		})
	}
	if validLaunchBoxArchiveMemberName("Mame.xml") != true {
		t.Fatal("known ignored member rejected")
	}
	if validLaunchBoxArchiveMemberName(strings.Repeat("é", launchBoxArchiveMaxMemberNameRunes)) {
		t.Fatal("non-ASCII member authority accepted")
	}
}

func TestLaunchBoxArchiveRejectsSymlinksAndEverySpecialFileType(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{name: "symlink", mode: os.ModeSymlink | 0o777},
		{name: "directory", mode: os.ModeDir | 0o755},
		{name: "named pipe", mode: os.ModeNamedPipe | 0o600},
		{name: "socket", mode: os.ModeSocket | 0o600},
		{name: "block device", mode: os.ModeDevice | 0o600},
		{name: "character device", mode: os.ModeDevice | os.ModeCharDevice | 0o600},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := validLaunchBoxFixtureEntries()
			entries[2] = launchBoxFixtureEntry{name: "Mame.xml", body: []byte("special"), method: zip.Store, mode: test.mode}
			archive := buildLaunchBoxArchive(t, entries)
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func TestLaunchBoxArchiveRejectsEncryptedUnsupportedDuplicateAndMalformedEntries(t *testing.T) {
	encrypted := validLaunchBoxFixtureEntries()
	encrypted[2].flags = 1
	assertLaunchBoxArchiveInvalid(t, buildLaunchBoxArchive(t, encrypted))

	unsupported := validLaunchBoxFixtureEntries()
	unsupportedArchive := buildLaunchBoxArchive(t, unsupported)
	patchLaunchBoxCentralMethod(t, unsupportedArchive, "Mame.xml", 99)
	assertLaunchBoxArchiveInvalid(t, unsupportedArchive)

	duplicate := append(validLaunchBoxFixtureEntries(), launchBoxFixtureEntry{name: "Metadata.xml", body: []byte("duplicate"), method: zip.Store})
	assertLaunchBoxArchiveInvalid(t, buildLaunchBoxArchive(t, duplicate))

	assertLaunchBoxArchiveInvalid(t, []byte("not a ZIP archive"))
	archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	assertLaunchBoxArchiveInvalid(t, archive[:len(archive)-1])
}

func TestLaunchBoxArchiveRejectsPrefixAndTrailingBytes(t *testing.T) {
	archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	assertLaunchBoxArchiveInvalid(t, append([]byte{'x'}, archive...))
	assertLaunchBoxArchiveInvalid(t, append(append([]byte(nil), archive...), 'x'))
}

func TestLaunchBoxArchiveRejectsMalformedIgnoredLocalEntryBeforeExposure(t *testing.T) {
	archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	patchLaunchBoxLocalSignature(t, archive, "Mame.xml", [4]byte{'P', 'K', 0, 0})
	assertLaunchBoxArchiveInvalid(t, archive)
}

func TestLaunchBoxArchiveRejectsNonEmptyArchiveComment(t *testing.T) {
	archive := buildLaunchBoxArchiveWithComment(t, launchBoxCommentArchiveFixture{
		entries: validLaunchBoxFixtureEntries(),
		comment: "unvalidated comment",
	})
	assertLaunchBoxArchiveInvalid(t, archive)
}

func TestLaunchBoxArchiveRejectsDecoyLocalHeadersBeforeExposure(t *testing.T) {
	for _, name := range []string{"Metadata.xml", "Mame.xml"} {
		t.Run(name, func(t *testing.T) {
			entries := validLaunchBoxFixtureEntries()
			for index := range entries {
				if entries[index].name == name {
					entries[index].extra = make([]byte, 30+len(name))
				}
			}
			archive := buildLaunchBoxArchive(t, entries)
			patchLaunchBoxDecoyLocalHeader(t, archive, name)
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func TestLaunchBoxArchiveRejectsOverlappingLocalRecordDataBeforeExposure(t *testing.T) {
	for _, name := range []string{"Metadata.xml", "Mame.xml"} {
		t.Run(name, func(t *testing.T) {
			archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
			patchLaunchBoxCentralCompressedSizeToOverlapNextLocalRecord(t, archive, name)
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func TestLaunchBoxArchiveRejectsCentralOffsetIntoNestedDecoyBeforeExposure(t *testing.T) {
	for _, name := range []string{"Metadata.xml", "Mame.xml"} {
		t.Run(name, func(t *testing.T) {
			entries := validLaunchBoxFixtureEntries()
			for index := range entries {
				if entries[index].name == name {
					entries[index].extra = make([]byte, 30+len(name))
				}
			}
			archive := buildLaunchBoxArchive(t, entries)
			patchLaunchBoxCentralOffsetToNestedDecoy(t, archive, name)
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func TestLaunchBoxArchiveDataDescriptorForms(t *testing.T) {
	const (
		crc        = uint32(0x10203040)
		compressed = uint64(0x50607080)
		expanded   = uint64(0x90a0b0c0)
	)
	tests := []struct {
		name      string
		zip64     bool
		signature bool
		wantBytes uint64
	}{
		{name: "32-bit without signature", wantBytes: 12},
		{name: "32-bit with signature", signature: true, wantBytes: 16},
		{name: "ZIP64 without signature", zip64: true, wantBytes: 20},
		{name: "ZIP64 with signature", zip64: true, signature: true, wantBytes: 24},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := make([]byte, test.wantBytes)
			offset := 0
			if test.signature {
				binary.LittleEndian.PutUint32(archive[:4], 0x08074b50)
				offset = 4
			}
			binary.LittleEndian.PutUint32(archive[offset:offset+4], crc)
			offset += 4
			if test.zip64 {
				binary.LittleEndian.PutUint64(archive[offset:offset+8], compressed)
				offset += 8
				binary.LittleEndian.PutUint64(archive[offset:offset+8], expanded)
			} else {
				binary.LittleEndian.PutUint32(archive[offset:offset+4], uint32(compressed))
				offset += 4
				binary.LittleEndian.PutUint32(archive[offset:offset+4], uint32(expanded))
			}
			got, ok := launchBoxArchiveDataDescriptorBytes(bytes.NewReader(archive), int64(len(archive)), int64(len(archive)), 0, launchBoxArchiveCentralEntry{
				crc:              crc,
				compressedSize:   compressed,
				uncompressedSize: expanded,
				zip64Sizes:       test.zip64,
			})
			if !ok || got != test.wantBytes {
				t.Fatalf("descriptor extent = %d, %v; want %d, true", got, ok, test.wantBytes)
			}

			truncated := archive[:len(archive)-1]
			if got, ok := launchBoxArchiveDataDescriptorBytes(bytes.NewReader(truncated), int64(len(truncated)), int64(len(truncated)), 0, launchBoxArchiveCentralEntry{
				crc:              crc,
				compressedSize:   compressed,
				uncompressedSize: expanded,
				zip64Sizes:       test.zip64,
			}); ok {
				t.Fatalf("truncated descriptor extent = %d, want rejection", got)
			}

			if got, ok := launchBoxArchiveDataDescriptorBytes(bytes.NewReader(archive), int64(len(archive)), int64(len(archive)), 0, launchBoxArchiveCentralEntry{
				crc:              crc,
				compressedSize:   compressed,
				uncompressedSize: expanded,
				zip64Sizes:       !test.zip64,
			}); ok {
				t.Fatalf("wrong-width descriptor extent = %d, want rejection", got)
			}
		})
	}
}

func TestLaunchBoxArchiveUnsignedDescriptorWithSignatureValuedCRC(t *testing.T) {
	const signature = uint32(0x08074b50)
	archive := make([]byte, 12)
	binary.LittleEndian.PutUint32(archive[0:4], signature)
	binary.LittleEndian.PutUint32(archive[4:8], 8)
	binary.LittleEndian.PutUint32(archive[8:12], 8)
	got, ok := launchBoxArchiveDataDescriptorBytes(bytes.NewReader(archive), int64(len(archive)), int64(len(archive)), 0, launchBoxArchiveCentralEntry{
		crc:              signature,
		compressedSize:   8,
		uncompressedSize: 8,
	})
	if !ok || got != 12 {
		t.Fatalf("unsigned descriptor with signature-valued CRC = %d, %v; want 12, true", got, ok)
	}
}

func TestLaunchBoxArchiveSignedDescriptorWithSignatureValuedFieldsPrefersSignedForm(t *testing.T) {
	const signature = uint32(0x08074b50)
	archive := make([]byte, 16)
	for offset := 0; offset < len(archive); offset += 4 {
		binary.LittleEndian.PutUint32(archive[offset:offset+4], signature)
	}
	got, ok := launchBoxArchiveDataDescriptorBytes(bytes.NewReader(archive), int64(len(archive)), int64(len(archive)), 0, launchBoxArchiveCentralEntry{
		crc:              signature,
		compressedSize:   uint64(signature),
		uncompressedSize: uint64(signature),
	})
	if !ok || got != 16 {
		t.Fatalf("signed descriptor with signature-valued fields = %d, %v; want 16, true", got, ok)
	}
}

func TestLaunchBoxArchiveDataDescriptorValuesFormsBeforeExposure(t *testing.T) {
	forms := []struct {
		name   string
		zip64  bool
		signed bool
	}{
		{name: "32-bit without signature"},
		{name: "32-bit with signature", signed: true},
		{name: "ZIP64 without signature", zip64: true},
		{name: "ZIP64 with signature", zip64: true, signed: true},
	}
	for _, form := range forms {
		for _, member := range []string{"Metadata.xml", "Mame.xml"} {
			form, member := form, member
			t.Run(form.name+"/"+member+"/valid", func(t *testing.T) {
				archive := buildLaunchBoxDataDescriptorArchive(t, member, form.zip64, form.signed)
				opened, err := openLaunchBoxArchive(bytes.NewReader(archive), int64(len(archive)))
				if opened == nil || err != nil {
					t.Fatalf("valid descriptor form rejected: archive=%v err=%v", opened, err)
				}
			})
			for _, field := range []string{"crc", "compressed", "expanded"} {
				field := field
				t.Run(form.name+"/"+member+"/"+field, func(t *testing.T) {
					archive := buildLaunchBoxDataDescriptorArchive(t, member, form.zip64, form.signed)
					patchLaunchBoxDataDescriptorValueForForm(t, archive, member, form.zip64, form.signed, field)
					assertLaunchBoxArchiveInvalid(t, archive)
				})
			}
		}
	}
}

func TestLaunchBoxArchiveCorruptAndTruncatedMemberFailuresAreClosed(t *testing.T) {
	archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	archive[launchBoxArchiveDataOffset(t, archive, "Metadata.xml")] ^= 0xff
	admitted, err := openLaunchBoxArchive(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("corrupt member was rejected before stream: %v", err)
	}
	reader, err := admitted.OpenMember("Metadata.xml")
	if err != nil {
		t.Fatalf("OpenMember CRC-corrupt entry: %v", err)
	}
	_, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr == nil || closeErr != nil {
		t.Fatalf("CRC-corrupt member read=%v close=%v, want read failure and clean close", readErr, closeErr)
	}

	truncated := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	guard := &launchBoxArchiveReadGuard{
		source:        bytes.NewReader(truncated),
		blockedOffset: int64(launchBoxArchiveDataOffset(t, truncated, "Metadata.xml")),
	}
	admitted, err = openLaunchBoxArchive(guard, int64(len(truncated)))
	if err != nil {
		t.Fatalf("truncated member was rejected before stream: %v", err)
	}
	guard.block = true
	reader, err = admitted.OpenMember("Metadata.xml")
	if err != nil {
		t.Fatalf("OpenMember truncated entry: %v", err)
	}
	_, readErr = io.ReadAll(reader)
	closeErr = reader.Close()
	if readErr == nil || closeErr != nil {
		t.Fatalf("truncated member read=%v close=%v, want read failure and clean close", readErr, closeErr)
	}
}

func TestLaunchBoxArchiveDetectsCorruptMemberWithZeroCentralCRC(t *testing.T) {
	archive := buildLaunchBoxArchiveWithoutDataDescriptors(t, validLaunchBoxFixtureEntries())
	patchLaunchBoxMemberWithoutDataDescriptor(t, archive, "Metadata.xml", 0)
	admitted, err := openLaunchBoxArchive(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("zero-CRC member was rejected before stream: %v", err)
	}
	reader, err := admitted.OpenMember("Metadata.xml")
	if err != nil {
		t.Fatalf("OpenMember zero-CRC member: %v", err)
	}
	_, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if opCode(readErr) != ErrInvalidResponse || closeErr != nil {
		t.Fatalf("zero-CRC member read=%v close=%v, want invalid_response and clean close", readErr, closeErr)
	}
}

func TestLaunchBoxArchiveRejectsLocalCentralMetadataMismatchBeforeStreaming(t *testing.T) {
	archive := buildLaunchBoxArchive(t, validLaunchBoxFixtureEntries())
	for offset := 0; offset+30 <= len(archive); offset++ {
		if !bytes.Equal(archive[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+26 : offset+28]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+28 : offset+30]))
		if offset+30+nameLength+extraLength > len(archive) {
			t.Fatalf("truncated local-header fixture")
		}
		if string(archive[offset+30:offset+30+nameLength]) == "Mame.xml" {
			binary.LittleEndian.PutUint16(archive[offset+8:offset+10], uint16(zip.Deflate))
			break
		}
	}
	assertLaunchBoxArchiveInvalid(t, archive)
}

func TestLaunchBoxArchiveCheckedAdditionRejectsOverflow(t *testing.T) {
	if sum, ok := checkedLaunchBoxArchiveAdd(math.MaxUint64, 1); ok || sum != 0 {
		t.Fatalf("overflow addition = %d, %v", sum, ok)
	}
	if sum, ok := checkedLaunchBoxArchiveAdd(math.MaxUint64-1, 1); !ok || sum != math.MaxUint64 {
		t.Fatalf("boundary addition = %d, %v", sum, ok)
	}
}

func TestLaunchBoxArchiveRejectsMalformedSignedDescriptorUnsignedFallback(t *testing.T) {
	const descriptorSignature = uint32(0x08074b50)
	for _, zip64 := range []bool{false, true} {
		for _, member := range []string{"Metadata.xml", "Mame.xml"} {
			formName := map[bool]string{false: "32-bit", true: "ZIP64"}[zip64]
			t.Run(formName+"/"+member, func(t *testing.T) {
				archive := buildLaunchBoxDataDescriptorArchive(t, member, zip64, true)
				localOffset := launchBoxArchiveLocalHeaderOffset(t, archive, member)
				nameLength := int(binary.LittleEndian.Uint16(archive[localOffset+26 : localOffset+28]))
				extraLength := int(binary.LittleEndian.Uint16(archive[localOffset+28 : localOffset+30]))
				bodyLength := map[string]int{"Metadata.xml": 8, "Mame.xml": 4}[member]
				descriptorOffset := localOffset + 30 + nameLength + extraLength + bodyLength
				if binary.LittleEndian.Uint32(archive[descriptorOffset:descriptorOffset+4]) != descriptorSignature {
					t.Fatalf("signed descriptor missing at %d", descriptorOffset)
				}
				centralOffset := launchBoxArchiveCentralEntryOffset(t, archive, member)
				binary.LittleEndian.PutUint32(archive[centralOffset+16:centralOffset+20], descriptorSignature)
				binary.LittleEndian.PutUint32(archive[descriptorOffset+4:descriptorOffset+8], uint32(bodyLength))
				if zip64 {
					binary.LittleEndian.PutUint32(archive[descriptorOffset+8:descriptorOffset+12], 0)
					binary.LittleEndian.PutUint32(archive[descriptorOffset+12:descriptorOffset+16], uint32(bodyLength))
					binary.LittleEndian.PutUint32(archive[descriptorOffset+16:descriptorOffset+20], 0)
					binary.LittleEndian.PutUint32(archive[descriptorOffset+20:descriptorOffset+24], uint32(bodyLength+1))
				} else {
					binary.LittleEndian.PutUint32(archive[descriptorOffset+8:descriptorOffset+12], uint32(bodyLength))
					binary.LittleEndian.PutUint32(archive[descriptorOffset+12:descriptorOffset+16], uint32(bodyLength+1))
				}
				assertLaunchBoxArchiveInvalid(t, archive)
			})
		}
	}
}

func TestLaunchBoxArchiveAcceptsUnsignedDescriptorWithSignatureValuedCRCAtRecordBoundary(t *testing.T) {
	const descriptorSignature = uint32(0x08074b50)
	for _, zip64 := range []bool{false, true} {
		for _, member := range []string{"Metadata.xml", "Mame.xml"} {
			formName := map[bool]string{false: "32-bit", true: "ZIP64"}[zip64]
			t.Run(formName+"/"+member, func(t *testing.T) {
				archive := buildLaunchBoxDataDescriptorArchive(t, member, zip64, false)
				localOffset := launchBoxArchiveLocalHeaderOffset(t, archive, member)
				nameLength := int(binary.LittleEndian.Uint16(archive[localOffset+26 : localOffset+28]))
				extraLength := int(binary.LittleEndian.Uint16(archive[localOffset+28 : localOffset+30]))
				bodyLength := map[string]int{"Metadata.xml": 8, "Mame.xml": 4}[member]
				descriptorOffset := localOffset + 30 + nameLength + extraLength + bodyLength
				binary.LittleEndian.PutUint32(archive[descriptorOffset:descriptorOffset+4], descriptorSignature)
				centralOffset := launchBoxArchiveCentralEntryOffset(t, archive, member)
				binary.LittleEndian.PutUint32(archive[centralOffset+16:centralOffset+20], descriptorSignature)
				opened, err := openLaunchBoxArchive(bytes.NewReader(archive), int64(len(archive)))
				if opened == nil || err != nil {
					t.Fatalf("unsigned signature-valued descriptor rejected: archive=%v err=%v", opened, err)
				}
			})
		}
	}
}

func TestLaunchBoxArchiveRejectsNoDescriptorRecordGapsBeforeExposure(t *testing.T) {
	for _, test := range []struct {
		name   string
		member string
	}{
		{name: "required member before next local header", member: "Metadata.xml"},
		{name: "ignored member before next local header", member: "Mame.xml"},
		{name: "ignored final member before central directory", member: "Files.xml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := buildLaunchBoxDataDescriptorArchive(t, test.member, false, true)
			body := map[string][]byte{
				"Metadata.xml": []byte("metadata"),
				"Mame.xml":     []byte("mame"),
				"Files.xml":    []byte("files"),
			}[test.member]
			patchLaunchBoxMemberWithoutDataDescriptor(t, archive, test.member, crc32.ChecksumIEEE(body))
			assertLaunchBoxArchiveInvalid(t, archive)
		})
	}
}

func TestLaunchBoxArchiveAcceptsContiguousNoDescriptorRecords(t *testing.T) {
	archive := buildLaunchBoxArchiveWithoutDataDescriptors(t, validLaunchBoxFixtureEntries())
	opened, err := openLaunchBoxArchive(bytes.NewReader(archive), int64(len(archive)))
	if opened == nil || err != nil {
		t.Fatalf("contiguous no-descriptor records rejected: archive=%v err=%v", opened, err)
	}
}
