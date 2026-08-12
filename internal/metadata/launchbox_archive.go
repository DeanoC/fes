package metadata

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	launchBoxArchiveMaxCompressedBytes  uint64 = 256 << 20
	launchBoxArchiveMaxMembers                 = 8
	launchBoxArchiveMaxExpandedBytes    uint64 = 1 << 30
	launchBoxArchiveMaxMemberBytes      uint64 = 768 << 20
	launchBoxArchiveMaxCompressionRatio uint64 = 32
	launchBoxArchiveMaxMemberNameBytes         = 64
	launchBoxArchiveMaxMemberNameRunes         = 64
)

// launchBoxArchive is the admitted central directory plus the only streaming
// seam needed by the XML reader. No member stream is opened while the archive
// is being admitted.
type launchBoxArchive struct {
	members map[string]*zip.File
}

// openLaunchBoxArchive parses and admits a LaunchBox snapshot's central
// directory. It validates every entry before returning an object that can open
// a member stream.
func openLaunchBoxArchive(source io.ReaderAt, sourceSize int64) (*launchBoxArchive, error) {
	if source == nil || sourceSize < 0 || uint64(sourceSize) > launchBoxArchiveMaxCompressedBytes {
		return nil, launchBoxArchiveInvalid()
	}
	centralDirectoryStart, centralDirectoryEnd, declaredMembers, ok := validLaunchBoxArchiveEnvelope(source, sourceSize)
	if !ok || declaredMembers == 0 || declaredMembers > launchBoxArchiveMaxMembers {
		return nil, launchBoxArchiveInvalid()
	}
	centralEntries, ok := launchBoxArchiveCentralDirectoryEntries(source, centralDirectoryStart, centralDirectoryEnd, declaredMembers)
	if !ok {
		return nil, launchBoxArchiveInvalid()
	}

	reader, err := zip.NewReader(source, sourceSize)
	if err != nil || len(reader.File) != int(declaredMembers) {
		return nil, launchBoxArchiveInvalid()
	}
	if !validLaunchBoxArchiveLocalRecords(source, sourceSize, centralDirectoryStart, centralEntries, reader.File) {
		return nil, launchBoxArchiveInvalid()
	}

	seen := make(map[string]struct{}, len(reader.File))
	members := make(map[string]*zip.File, 2)
	var compressedTotal, expandedTotal uint64
	for _, member := range reader.File {
		if member == nil || !validLaunchBoxArchiveMemberName(member.Name) {
			return nil, launchBoxArchiveInvalid()
		}
		if _, exists := seen[member.Name]; exists {
			return nil, launchBoxArchiveInvalid()
		}
		seen[member.Name] = struct{}{}
		if member.Flags&1 != 0 { // general-purpose bit 0: encrypted
			return nil, launchBoxArchiveInvalid()
		}
		switch member.Method {
		case zip.Store, zip.Deflate:
		default:
			return nil, launchBoxArchiveInvalid()
		}
		mode := member.FileInfo().Mode()
		// ModeType includes ModeSymlink. Keep the explicit symlink check as a
		// defense against callers or future archive/zip mode interpretation
		// changes: every non-regular type is rejected before any Open call.
		if mode&os.ModeType != 0 || mode&os.ModeSymlink != 0 || !mode.IsRegular() {
			return nil, launchBoxArchiveInvalid()
		}

		compressed, expanded := member.CompressedSize64, member.UncompressedSize64
		if !validLaunchBoxArchiveMemberSizes(compressed, expanded) {
			return nil, launchBoxArchiveInvalid()
		}
		var ok bool
		compressedTotal, ok = checkedLaunchBoxArchiveAdd(compressedTotal, compressed)
		if !ok || compressedTotal > launchBoxArchiveMaxCompressedBytes {
			return nil, launchBoxArchiveInvalid()
		}
		expandedTotal, ok = checkedLaunchBoxArchiveAdd(expandedTotal, expanded)
		if !ok || expandedTotal > launchBoxArchiveMaxExpandedBytes {
			return nil, launchBoxArchiveInvalid()
		}
		if validLaunchBoxArchiveRequiredMemberName(member.Name) {
			members[member.Name] = member
		}
	}

	if _, ok := members["Metadata.xml"]; !ok {
		return nil, launchBoxArchiveInvalid()
	}
	if _, ok := members["Platforms.xml"]; !ok {
		return nil, launchBoxArchiveInvalid()
	}
	if !validLaunchBoxArchiveRatio(expandedTotal, compressedTotal) {
		return nil, launchBoxArchiveInvalid()
	}
	return &launchBoxArchive{members: members}, nil
}

// OpenMember is the intentionally narrow archive-to-parser seam. Admission is
// complete before this method can be called; CRC, decompression, and truncation
// errors are returned by the standard library while the caller consumes the
// stream.
func (a *launchBoxArchive) OpenMember(name string) (io.ReadCloser, error) {
	if a == nil || !validLaunchBoxArchiveRequiredMemberName(name) {
		return nil, launchBoxArchiveInvalid()
	}
	member, ok := a.members[name]
	if !ok {
		return nil, launchBoxArchiveInvalid()
	}
	stream, err := member.Open()
	if err != nil {
		return nil, launchBoxArchiveInvalid()
	}
	return &launchBoxArchiveMemberReader{stream: stream, checksum: crc32.NewIEEE(), expectedCRC: member.CRC32}, nil
}

type launchBoxArchiveMemberReader struct {
	stream      io.ReadCloser
	checksum    hash.Hash32
	expectedCRC uint32
}

func (r *launchBoxArchiveMemberReader) Read(dst []byte) (int, error) {
	n, err := r.stream.Read(dst)
	if n > 0 {
		_, _ = r.checksum.Write(dst[:n])
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, launchBoxArchiveInvalid()
	}
	if errors.Is(err, io.EOF) && r.checksum.Sum32() != r.expectedCRC {
		return n, launchBoxArchiveInvalid()
	}
	return n, err
}

func (r *launchBoxArchiveMemberReader) Close() error {
	if err := r.stream.Close(); err != nil {
		return launchBoxArchiveInvalid()
	}
	return nil
}

func validLaunchBoxArchiveMemberName(name string) bool {
	if name == "" || !utf8.ValidString(name) ||
		len(name) > launchBoxArchiveMaxMemberNameBytes ||
		utf8.RuneCountInString(name) > launchBoxArchiveMaxMemberNameRunes ||
		filepath.IsAbs(name) || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." || strings.Contains(name, "..") {
		return false
	}
	switch name {
	case "Metadata.xml", "Platforms.xml", "Mame.xml", "Files.xml":
		return true
	default:
		return false
	}
}

func validLaunchBoxArchiveEnvelope(source io.ReaderAt, sourceSize int64) (int64, int64, uint64, bool) {
	const (
		localFileHeaderBytes       = 4
		endOfCentralDirectoryBytes = 22
		maximumCommentBytes        = 1<<16 - 1
	)
	if source == nil || sourceSize < endOfCentralDirectoryBytes {
		return 0, 0, 0, false
	}

	var localHeader [localFileHeaderBytes]byte
	if _, err := source.ReadAt(localHeader[:], 0); err != nil || !bytes.Equal(localHeader[:], []byte{'P', 'K', 3, 4}) {
		return 0, 0, 0, false
	}

	windowStart := sourceSize - int64(endOfCentralDirectoryBytes+maximumCommentBytes)
	if windowStart < 0 {
		windowStart = 0
	}
	window := make([]byte, int(sourceSize-windowStart))
	if _, err := source.ReadAt(window, windowStart); err != nil {
		return 0, 0, 0, false
	}
	for offset := len(window) - endOfCentralDirectoryBytes; offset >= 0; offset-- {
		if !bytes.Equal(window[offset:offset+4], []byte{'P', 'K', 5, 6}) {
			continue
		}
		commentLength := int64(binary.LittleEndian.Uint16(window[offset+20 : offset+22]))
		if commentLength != 0 || windowStart+int64(offset)+endOfCentralDirectoryBytes+commentLength != sourceSize {
			continue
		}
		eocdOffset := windowStart + int64(offset)
		centralDirectoryStart, centralDirectoryEnd, declaredMembers, ok := launchBoxArchiveCentralDirectoryStart(source, sourceSize, eocdOffset)
		if ok {
			return centralDirectoryStart, centralDirectoryEnd, declaredMembers, true
		}
	}
	return 0, 0, 0, false
}

func launchBoxArchiveCentralDirectoryStart(source io.ReaderAt, sourceSize, eocdOffset int64) (int64, int64, uint64, bool) {
	if source == nil || eocdOffset < 0 || eocdOffset+22 > sourceSize {
		return 0, 0, 0, false
	}
	var eocd [22]byte
	if _, err := source.ReadAt(eocd[:], eocdOffset); err != nil || !bytes.Equal(eocd[:4], []byte{'P', 'K', 5, 6}) {
		return 0, 0, 0, false
	}
	if binary.LittleEndian.Uint16(eocd[4:6]) != 0 || binary.LittleEndian.Uint16(eocd[6:8]) != 0 ||
		binary.LittleEndian.Uint16(eocd[20:22]) != 0 {
		return launchBoxArchiveZIP64CentralDirectoryStart(source, sourceSize, eocdOffset)
	}
	entriesThisDisk := binary.LittleEndian.Uint16(eocd[8:10])
	entriesTotal := binary.LittleEndian.Uint16(eocd[10:12])
	if entriesThisDisk != entriesTotal {
		return 0, 0, 0, false
	}
	directorySize := uint64(binary.LittleEndian.Uint32(eocd[12:16]))
	directoryStart := uint64(binary.LittleEndian.Uint32(eocd[16:20]))
	if directorySize == uint64(^uint32(0)) || directoryStart == uint64(^uint32(0)) ||
		binary.LittleEndian.Uint16(eocd[8:10]) == ^uint16(0) || binary.LittleEndian.Uint16(eocd[10:12]) == ^uint16(0) {
		return launchBoxArchiveZIP64CentralDirectoryStart(source, sourceSize, eocdOffset)
	}
	if directoryStart > uint64(sourceSize) || directorySize > uint64(sourceSize)-directoryStart || directoryStart+directorySize != uint64(eocdOffset) {
		return 0, 0, 0, false
	}
	return int64(directoryStart), eocdOffset, uint64(entriesTotal), true
}

func launchBoxArchiveZIP64CentralDirectoryStart(source io.ReaderAt, sourceSize, eocdOffset int64) (int64, int64, uint64, bool) {
	const (
		locatorBytes = 20
		endBytes     = 56
	)
	if eocdOffset < locatorBytes {
		return 0, 0, 0, false
	}
	var locator [locatorBytes]byte
	if _, err := source.ReadAt(locator[:], eocdOffset-locatorBytes); err != nil || !bytes.Equal(locator[:4], []byte{'P', 'K', 6, 7}) ||
		binary.LittleEndian.Uint32(locator[4:8]) != 0 || binary.LittleEndian.Uint32(locator[16:20]) != 1 {
		return 0, 0, 0, false
	}
	zip64Offset := binary.LittleEndian.Uint64(locator[8:16])
	if zip64Offset > uint64(sourceSize) || zip64Offset > uint64(^uint64(0)>>1) || uint64(sourceSize)-zip64Offset < 12 {
		return 0, 0, 0, false
	}
	var zip64 [endBytes]byte
	if _, err := source.ReadAt(zip64[:], int64(zip64Offset)); err != nil || !bytes.Equal(zip64[:4], []byte{'P', 'K', 6, 6}) {
		return 0, 0, 0, false
	}
	recordSize := binary.LittleEndian.Uint64(zip64[4:12])
	if recordSize < 44 || recordSize > uint64(sourceSize)-zip64Offset-12 || zip64Offset+12+recordSize != uint64(eocdOffset-locatorBytes) {
		return 0, 0, 0, false
	}
	if binary.LittleEndian.Uint32(zip64[16:20]) != 0 || binary.LittleEndian.Uint32(zip64[20:24]) != 0 {
		return 0, 0, 0, false
	}
	entriesThisDisk := binary.LittleEndian.Uint64(zip64[24:32])
	entriesTotal := binary.LittleEndian.Uint64(zip64[32:40])
	if entriesThisDisk != entriesTotal {
		return 0, 0, 0, false
	}
	directorySize := binary.LittleEndian.Uint64(zip64[40:48])
	directoryStart := binary.LittleEndian.Uint64(zip64[48:56])
	if directoryStart > uint64(sourceSize) || directorySize > uint64(sourceSize)-directoryStart || directoryStart+directorySize != zip64Offset {
		return 0, 0, 0, false
	}
	return int64(directoryStart), int64(zip64Offset), entriesTotal, true
}

type launchBoxArchiveCentralEntry struct {
	name              string
	flags             uint16
	method            uint16
	crc               uint32
	compressedSize    uint64
	uncompressedSize  uint64
	localHeaderOffset uint64
	zip64Sizes        bool
}

func launchBoxArchiveCentralDirectoryEntries(source io.ReaderAt, start, end int64, declaredMembers uint64) ([]launchBoxArchiveCentralEntry, bool) {
	const fixedHeaderBytes = 46
	if source == nil || start < 0 || end < start || declaredMembers == 0 || declaredMembers > launchBoxArchiveMaxMembers {
		return nil, false
	}
	entries := make([]launchBoxArchiveCentralEntry, int(declaredMembers))
	position := start
	var header [fixedHeaderBytes]byte
	for index := uint64(0); index < declaredMembers; index++ {
		if end-position < fixedHeaderBytes {
			return nil, false
		}
		if _, err := source.ReadAt(header[:], position); err != nil || !bytes.Equal(header[:4], []byte{'P', 'K', 1, 2}) {
			return nil, false
		}
		nameLength := uint64(binary.LittleEndian.Uint16(header[28:30]))
		extraLength := uint64(binary.LittleEndian.Uint16(header[30:32]))
		commentLength := uint64(binary.LittleEndian.Uint16(header[32:34]))
		recordBytes := uint64(fixedHeaderBytes) + nameLength + extraLength + commentLength
		if recordBytes > uint64(end-position) {
			return nil, false
		}
		if nameLength+extraLength+commentLength > uint64(^uint(0)>>1) {
			return nil, false
		}
		body := make([]byte, int(nameLength+extraLength+commentLength))
		if _, err := source.ReadAt(body, position+fixedHeaderBytes); err != nil {
			return nil, false
		}
		entry, ok := parseLaunchBoxArchiveCentralEntry(header[:], body)
		if !ok {
			return nil, false
		}
		entries[index] = entry
		position += int64(recordBytes)
	}
	if position != end {
		return nil, false
	}
	return entries, true
}

func parseLaunchBoxArchiveCentralEntry(header, body []byte) (launchBoxArchiveCentralEntry, bool) {
	const (
		fixedHeaderBytes = 46
		zip64ExtraID     = 0x0001
	)
	if len(header) != fixedHeaderBytes || len(body) < int(binary.LittleEndian.Uint16(header[28:30]))+
		int(binary.LittleEndian.Uint16(header[30:32]))+int(binary.LittleEndian.Uint16(header[32:34])) {
		return launchBoxArchiveCentralEntry{}, false
	}
	nameLength := int(binary.LittleEndian.Uint16(header[28:30]))
	extraLength := int(binary.LittleEndian.Uint16(header[30:32]))
	commentLength := int(binary.LittleEndian.Uint16(header[32:34]))
	if len(body) != nameLength+extraLength+commentLength {
		return launchBoxArchiveCentralEntry{}, false
	}
	entry := launchBoxArchiveCentralEntry{
		name:              string(body[:nameLength]),
		flags:             binary.LittleEndian.Uint16(header[8:10]),
		method:            binary.LittleEndian.Uint16(header[10:12]),
		crc:               binary.LittleEndian.Uint32(header[16:20]),
		compressedSize:    uint64(binary.LittleEndian.Uint32(header[20:24])),
		uncompressedSize:  uint64(binary.LittleEndian.Uint32(header[24:28])),
		localHeaderOffset: uint64(binary.LittleEndian.Uint32(header[42:46])),
	}
	diskNumber := uint32(binary.LittleEndian.Uint16(header[34:36]))
	needUncompressed, needCompressed := entry.uncompressedSize == uint64(^uint32(0)), entry.compressedSize == uint64(^uint32(0))
	needOffset, needDiskNumber := entry.localHeaderOffset == uint64(^uint32(0)), diskNumber == uint32(^uint16(0))
	entry.zip64Sizes = needUncompressed || needCompressed
	if needUncompressed || needCompressed || needOffset || needDiskNumber {
		extra := body[nameLength : nameLength+extraLength]
		found := false
		resolvedUncompressed, resolvedCompressed := !needUncompressed, !needCompressed
		resolvedOffset, resolvedDiskNumber := !needOffset, !needDiskNumber
		for len(extra) >= 4 {
			fieldID := binary.LittleEndian.Uint16(extra[:2])
			fieldLength := int(binary.LittleEndian.Uint16(extra[2:4]))
			extra = extra[4:]
			if fieldLength > len(extra) {
				return launchBoxArchiveCentralEntry{}, false
			}
			field := extra[:fieldLength]
			extra = extra[fieldLength:]
			if fieldID != zip64ExtraID {
				continue
			}
			if found {
				return launchBoxArchiveCentralEntry{}, false
			}
			found = true
			read := 0
			readUint64 := func() (uint64, bool) {
				if len(field)-read < 8 {
					return 0, false
				}
				value := binary.LittleEndian.Uint64(field[read : read+8])
				read += 8
				return value, true
			}
			if needUncompressed {
				value, ok := readUint64()
				if !ok {
					return launchBoxArchiveCentralEntry{}, false
				}
				entry.uncompressedSize = value
				resolvedUncompressed = true
			}
			if needCompressed {
				value, ok := readUint64()
				if !ok {
					return launchBoxArchiveCentralEntry{}, false
				}
				entry.compressedSize = value
				resolvedCompressed = true
			}
			if needOffset {
				value, ok := readUint64()
				if !ok {
					return launchBoxArchiveCentralEntry{}, false
				}
				entry.localHeaderOffset = value
				resolvedOffset = true
			}
			if needDiskNumber {
				if len(field)-read < 4 {
					return launchBoxArchiveCentralEntry{}, false
				}
				diskNumber = binary.LittleEndian.Uint32(field[read : read+4])
				read += 4
				resolvedDiskNumber = true
			}
		}
		if !found || !resolvedUncompressed || !resolvedCompressed || !resolvedOffset || !resolvedDiskNumber {
			return launchBoxArchiveCentralEntry{}, false
		}
	}
	if diskNumber != 0 {
		return launchBoxArchiveCentralEntry{}, false
	}
	return entry, true
}

func validLaunchBoxArchiveCentralEntry(entry launchBoxArchiveCentralEntry, member *zip.File) bool {
	return member != nil && entry.name == member.Name && entry.flags == member.Flags && entry.method == member.Method &&
		entry.crc == member.CRC32 && entry.compressedSize == member.CompressedSize64 && entry.uncompressedSize == member.UncompressedSize64
}

type launchBoxArchiveLocalRecord struct {
	start     uint64
	end       uint64
	bodyStart uint64
}

func validLaunchBoxArchiveLocalRecords(source io.ReaderAt, sourceSize, centralDirectoryStart int64, entries []launchBoxArchiveCentralEntry, members []*zip.File) bool {
	if source == nil || sourceSize < 0 || centralDirectoryStart < 0 || centralDirectoryStart > sourceSize || len(entries) == 0 || len(entries) != len(members) {
		return false
	}
	records := make([]launchBoxArchiveLocalRecord, len(entries))
	for index, entry := range entries {
		if !validLaunchBoxArchiveCentralEntry(entry, members[index]) {
			return false
		}
		record, ok := launchBoxArchiveLocalRecordAt(source, sourceSize, centralDirectoryStart, entry, members[index])
		if !ok {
			return false
		}
		records[index] = record
	}
	sort.Slice(records, func(left, right int) bool {
		return records[left].start < records[right].start
	})
	if records[0].start != 0 {
		return false
	}
	for _, record := range records {
		if launchBoxArchiveContainsLocalHeader(source, sourceSize, record.bodyStart, record.end) {
			return false
		}
	}
	for index := 1; index < len(records); index++ {
		if records[index].start < records[index-1].end {
			return false
		}
		if launchBoxArchiveContainsLocalHeader(source, sourceSize, records[index-1].end, records[index].start) {
			return false
		}
	}
	return !launchBoxArchiveContainsLocalHeader(source, sourceSize, records[len(records)-1].end, uint64(centralDirectoryStart))
}

func launchBoxArchiveContainsLocalHeader(source io.ReaderAt, sourceSize int64, start, end uint64) bool {
	const (
		scanChunkBytes        = 64 << 10
		localHeaderFixedBytes = 30
	)
	if source == nil || sourceSize < 0 || start >= end || end > uint64(sourceSize) || end > uint64(^uint64(0)>>1) {
		return false
	}
	buffer := make([]byte, scanChunkBytes+3)
	for offset := start; offset < end; offset += scanChunkBytes - 3 {
		readBytes := uint64(scanChunkBytes + 3)
		if remaining := end - offset; remaining < readBytes {
			readBytes = remaining
		}
		if readBytes < 4 {
			break
		}
		if _, err := source.ReadAt(buffer[:int(readBytes)], int64(offset)); err != nil {
			return true
		}
		for index := bytes.Index(buffer[:int(readBytes)], []byte{'P', 'K', 3, 4}); index >= 0; {
			candidate := offset + uint64(index)
			if candidate >= end {
				break
			}
			if candidate > uint64(^uint64(0)>>1)-localHeaderFixedBytes || candidate+localHeaderFixedBytes > uint64(sourceSize) {
				return true
			}
			var header [localHeaderFixedBytes]byte
			if _, err := source.ReadAt(header[:], int64(candidate)); err != nil {
				return true
			}
			nameLength := uint64(binary.LittleEndian.Uint16(header[26:28]))
			nameStart, ok := checkedLaunchBoxArchiveAdd(candidate, localHeaderFixedBytes)
			if !ok {
				return true
			}
			nameEnd, ok := checkedLaunchBoxArchiveAdd(nameStart, nameLength)
			if !ok || nameEnd > uint64(sourceSize) {
				return true
			}
			name := make([]byte, int(nameLength))
			if _, err := source.ReadAt(name, int64(nameStart)); err != nil {
				return true
			}
			if validLaunchBoxArchiveMemberName(string(name)) {
				return true
			}
			next := index + 1
			if next >= int(readBytes) {
				break
			}
			nextIndex := bytes.Index(buffer[next:int(readBytes)], []byte{'P', 'K', 3, 4})
			if nextIndex < 0 {
				break
			}
			index = next + nextIndex
		}
	}
	return false
}

func launchBoxArchiveLocalRecordAt(source io.ReaderAt, sourceSize, centralDirectoryStart int64, entry launchBoxArchiveCentralEntry, member *zip.File) (launchBoxArchiveLocalRecord, bool) {
	const localHeaderFixedBytes = 30
	if source == nil || member == nil || sourceSize < 0 || centralDirectoryStart < 0 || centralDirectoryStart > sourceSize ||
		entry.localHeaderOffset > uint64(^uint64(0)>>1) {
		return launchBoxArchiveLocalRecord{}, false
	}
	start := entry.localHeaderOffset
	centralLimit := uint64(centralDirectoryStart)
	sourceLimit := uint64(sourceSize)
	if start >= centralLimit || uint64(localHeaderFixedBytes) > centralLimit-start || uint64(localHeaderFixedBytes) > sourceLimit-start {
		return launchBoxArchiveLocalRecord{}, false
	}

	var header [localHeaderFixedBytes]byte
	if _, err := source.ReadAt(header[:], int64(start)); err != nil || !bytes.Equal(header[:4], []byte{'P', 'K', 3, 4}) {
		return launchBoxArchiveLocalRecord{}, false
	}
	nameLength := uint64(binary.LittleEndian.Uint16(header[26:28]))
	extraLength := uint64(binary.LittleEndian.Uint16(header[28:30]))
	bodyOffset, ok := checkedLaunchBoxArchiveAdd(start, uint64(localHeaderFixedBytes))
	if !ok {
		return launchBoxArchiveLocalRecord{}, false
	}
	bodyOffset, ok = checkedLaunchBoxArchiveAdd(bodyOffset, nameLength)
	if !ok {
		return launchBoxArchiveLocalRecord{}, false
	}
	bodyOffset, ok = checkedLaunchBoxArchiveAdd(bodyOffset, extraLength)
	if !ok || bodyOffset > centralLimit || bodyOffset > sourceLimit {
		return launchBoxArchiveLocalRecord{}, false
	}
	name := make([]byte, int(nameLength))
	if _, err := source.ReadAt(name, int64(start+uint64(localHeaderFixedBytes))); err != nil || !bytes.Equal(name, []byte(entry.name)) {
		return launchBoxArchiveLocalRecord{}, false
	}
	extra := make([]byte, int(extraLength))
	if _, err := source.ReadAt(extra, int64(start+uint64(localHeaderFixedBytes)+nameLength)); err != nil {
		return launchBoxArchiveLocalRecord{}, false
	}
	localFlags := binary.LittleEndian.Uint16(header[6:8])
	localMethod := binary.LittleEndian.Uint16(header[8:10])
	if localFlags != entry.flags || localMethod != entry.method {
		return launchBoxArchiveLocalRecord{}, false
	}
	localCompressed, localExpanded, ok := resolveLaunchBoxArchiveLocalSizes(extra, binary.LittleEndian.Uint32(header[18:22]), binary.LittleEndian.Uint32(header[22:26]))
	if !ok {
		return launchBoxArchiveLocalRecord{}, false
	}
	localCRC := binary.LittleEndian.Uint32(header[14:18])
	if localFlags&8 == 0 {
		if localCompressed != entry.compressedSize || localExpanded != entry.uncompressedSize || localCRC != entry.crc {
			return launchBoxArchiveLocalRecord{}, false
		}
	} else if !((localCompressed == 0 && localExpanded == 0 && localCRC == 0) ||
		(localCompressed == entry.compressedSize && localExpanded == entry.uncompressedSize && localCRC == entry.crc)) {
		return launchBoxArchiveLocalRecord{}, false
	}

	dataEnd, ok := checkedLaunchBoxArchiveAdd(bodyOffset, entry.compressedSize)
	if !ok || dataEnd > centralLimit || dataEnd > sourceLimit {
		return launchBoxArchiveLocalRecord{}, false
	}
	end := dataEnd
	if localFlags&8 != 0 {
		descriptorBytes, ok := launchBoxArchiveDataDescriptorBytes(source, sourceSize, centralDirectoryStart, dataEnd, entry)
		if !ok {
			return launchBoxArchiveLocalRecord{}, false
		}
		end, ok = checkedLaunchBoxArchiveAdd(dataEnd, descriptorBytes)
		if !ok || end > centralLimit || end > sourceLimit {
			return launchBoxArchiveLocalRecord{}, false
		}
	}
	memberBodyOffset, err := member.DataOffset()
	if err != nil || memberBodyOffset < 0 || uint64(memberBodyOffset) != bodyOffset {
		return launchBoxArchiveLocalRecord{}, false
	}
	return launchBoxArchiveLocalRecord{start: start, end: end, bodyStart: bodyOffset}, true
}

func resolveLaunchBoxArchiveLocalSizes(extra []byte, compressedRaw, expandedRaw uint32) (uint64, uint64, bool) {
	compressed, expanded := uint64(compressedRaw), uint64(expandedRaw)
	needCompressed, needExpanded := compressedRaw == ^uint32(0), expandedRaw == ^uint32(0)
	if !needCompressed && !needExpanded {
		return compressed, expanded, true
	}
	for len(extra) >= 4 {
		fieldID := binary.LittleEndian.Uint16(extra[:2])
		fieldLength := int(binary.LittleEndian.Uint16(extra[2:4]))
		extra = extra[4:]
		if fieldLength > len(extra) {
			return 0, 0, false
		}
		field := extra[:fieldLength]
		extra = extra[fieldLength:]
		if fieldID != 0x0001 {
			continue
		}
		read := 0
		if needExpanded {
			if len(field)-read < 8 {
				return 0, 0, false
			}
			expanded = binary.LittleEndian.Uint64(field[read : read+8])
			read += 8
		}
		if needCompressed {
			if len(field)-read < 8 {
				return 0, 0, false
			}
			compressed = binary.LittleEndian.Uint64(field[read : read+8])
			read += 8
		}
		return compressed, expanded, true
	}
	return 0, 0, false
}

func launchBoxArchiveDataDescriptorBytes(source io.ReaderAt, sourceSize, centralDirectoryStart int64, dataEnd uint64, entry launchBoxArchiveCentralEntry) (uint64, bool) {
	if source == nil || sourceSize < 0 || centralDirectoryStart < 0 || centralDirectoryStart > sourceSize || dataEnd > uint64(centralDirectoryStart) || dataEnd > uint64(sourceSize) {
		return 0, false
	}
	const (
		descriptor32Bytes = 12
		descriptor64Bytes = 20
		signatureBytes    = 4
	)
	descriptorBytes := uint64(descriptor32Bytes)
	if entry.zip64Sizes {
		descriptorBytes = descriptor64Bytes
	}
	var signature [signatureBytes]byte
	if uint64(centralDirectoryStart)-dataEnd < descriptorBytes || uint64(sourceSize)-dataEnd < descriptorBytes ||
		int64(dataEnd) < 0 {
		return 0, false
	}
	if _, err := source.ReadAt(signature[:], int64(dataEnd)); err != nil {
		return 0, false
	}
	if bytes.Equal(signature[:], []byte{'P', 'K', 7, 8}) {
		descriptorBytes += signatureBytes
	}
	if uint64(centralDirectoryStart)-dataEnd < descriptorBytes || uint64(sourceSize)-dataEnd < descriptorBytes {
		return 0, false
	}
	return descriptorBytes, true
}

func validLaunchBoxArchiveRequiredMemberName(name string) bool {
	switch name {
	case "Metadata.xml", "Platforms.xml":
		return true
	default:
		return false
	}
}

func validLaunchBoxArchiveMemberSizes(compressed, expanded uint64) bool {
	if compressed > launchBoxArchiveMaxCompressedBytes || expanded > launchBoxArchiveMaxMemberBytes {
		return false
	}
	return validLaunchBoxArchiveRatio(expanded, compressed)
}

// validLaunchBoxArchiveRatio implements expanded <= 32*compressed without
// multiplying. It therefore remains correct at uint64 boundaries and accepts
// the exact inclusive boundary.
func validLaunchBoxArchiveRatio(expanded, compressed uint64) bool {
	if expanded == 0 {
		return true
	}
	if compressed == 0 {
		return false
	}
	quotient, remainder := expanded/compressed, expanded%compressed
	return quotient < launchBoxArchiveMaxCompressionRatio ||
		quotient == launchBoxArchiveMaxCompressionRatio && remainder == 0
}

func checkedLaunchBoxArchiveAdd(left, right uint64) (uint64, bool) {
	if right > ^uint64(0)-left {
		return 0, false
	}
	return left + right, true
}

func launchBoxArchiveInvalid() error {
	return newOpError(ErrInvalidResponse, errors.New("LaunchBox archive admission failed"))
}
