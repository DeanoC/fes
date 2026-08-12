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
	if !ok || declaredMembers == 0 || declaredMembers > launchBoxArchiveMaxMembers ||
		!validLaunchBoxArchiveCentralDirectory(source, centralDirectoryStart, centralDirectoryEnd, declaredMembers) {
		return nil, launchBoxArchiveInvalid()
	}

	reader, err := zip.NewReader(source, sourceSize)
	if err != nil || len(reader.File) != int(declaredMembers) {
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
		if !validLaunchBoxArchiveLocalEntry(source, sourceSize, centralDirectoryStart, member) {
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

func validLaunchBoxArchiveCentralDirectory(source io.ReaderAt, start, end int64, declaredMembers uint64) bool {
	const fixedHeaderBytes = 46
	if source == nil || start < 0 || end < start || declaredMembers == 0 {
		return false
	}
	position := start
	var header [fixedHeaderBytes]byte
	for index := uint64(0); index < declaredMembers; index++ {
		if end-position < fixedHeaderBytes {
			return false
		}
		if _, err := source.ReadAt(header[:], position); err != nil || !bytes.Equal(header[:4], []byte{'P', 'K', 1, 2}) {
			return false
		}
		recordBytes := uint64(fixedHeaderBytes) + uint64(binary.LittleEndian.Uint16(header[28:30])) +
			uint64(binary.LittleEndian.Uint16(header[30:32])) + uint64(binary.LittleEndian.Uint16(header[32:34]))
		if recordBytes > uint64(end-position) {
			return false
		}
		position += int64(recordBytes)
	}
	return position == end
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

func validLaunchBoxArchiveLocalEntry(source io.ReaderAt, sourceSize, centralDirectoryStart int64, member *zip.File) bool {
	const localHeaderFixedBytes = 30
	const localHeaderMaximumBytes = localHeaderFixedBytes + 2*(1<<16-1)
	if source == nil || member == nil || sourceSize < 0 || centralDirectoryStart < 0 || centralDirectoryStart > sourceSize {
		return false
	}
	bodyOffset, err := member.DataOffset()
	if err != nil || bodyOffset < 0 || bodyOffset > centralDirectoryStart {
		return false
	}
	if member.CompressedSize64 > uint64(centralDirectoryStart-bodyOffset) {
		return false
	}

	windowStart := bodyOffset - localHeaderMaximumBytes
	if windowStart < 0 {
		windowStart = 0
	}
	window := make([]byte, int(bodyOffset-windowStart))
	if _, err := source.ReadAt(window, windowStart); err != nil {
		return false
	}
	for offset := len(window) - localHeaderFixedBytes; offset >= 0; offset-- {
		if !bytes.Equal(window[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			continue
		}
		nameLength := int(binary.LittleEndian.Uint16(window[offset+26 : offset+28]))
		extraLength := int(binary.LittleEndian.Uint16(window[offset+28 : offset+30]))
		if offset+localHeaderFixedBytes+nameLength+extraLength != len(window) {
			continue
		}
		if !bytes.Equal(window[offset+localHeaderFixedBytes:offset+localHeaderFixedBytes+nameLength], []byte(member.Name)) {
			continue
		}
		localFlags := binary.LittleEndian.Uint16(window[offset+6 : offset+8])
		localMethod := binary.LittleEndian.Uint16(window[offset+8 : offset+10])
		if localFlags != member.Flags || localMethod != member.Method {
			return false
		}
		localCompressed := binary.LittleEndian.Uint32(window[offset+18 : offset+22])
		localExpanded := binary.LittleEndian.Uint32(window[offset+22 : offset+26])
		localCRC := binary.LittleEndian.Uint32(window[offset+14 : offset+18])
		if localFlags&8 == 0 {
			return uint64(localCompressed) == member.CompressedSize64 &&
				uint64(localExpanded) == member.UncompressedSize64 && localCRC == member.CRC32
		}
		return (localCompressed == 0 && localExpanded == 0 && localCRC == 0) ||
			(uint64(localCompressed) == member.CompressedSize64 && uint64(localExpanded) == member.UncompressedSize64 && localCRC == member.CRC32)
	}
	return false
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
