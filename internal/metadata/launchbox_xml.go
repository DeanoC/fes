package metadata

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	launchBoxXMLMaxDeclarationBytes       = 256
	launchBoxXMLMaxTextBytes              = 131072
	launchBoxXMLMaxCDATAFrameBytes        = 131084
	launchBoxXMLMaxCommentFrameBytes      = 131079
	launchBoxXMLMaxStartTagBytes          = 16384
	launchBoxXMLMaxEndTagBytes            = 128
	launchBoxXMLMaxNameBytes              = 64
	launchBoxXMLMaxNameRunes              = 64
	launchBoxXMLMaxAttributeBytes         = 1024
	launchBoxXMLMaxAttributeRunes         = 256
	launchBoxXMLMaxEntityBytes            = 32
	launchBoxXMLMaxMemberBytes            = 768 << 20
	launchBoxXMLMaxDepth                  = 8
	launchBoxXMLMaxAttributes             = 8
	launchBoxXMLMaxAggregateAttributes    = 4096
	launchBoxXMLMaxAggregateStartElements = 16000000

	launchBoxXMLMaxGames             = 250000
	launchBoxXMLMaxAliases           = 250000
	launchBoxXMLMaxImages            = 2000000
	launchBoxXMLMaxPlatforms         = 512
	launchBoxXMLMaxPlatformAliases   = 1024
	launchBoxXMLMaxEmulators         = 128
	launchBoxXMLMaxEmulatorPlatforms = 1024
	launchBoxXMLMaxAliasesPerGame    = 64
	launchBoxXMLMaxImagesPerGame     = 512
	launchBoxXMLMaxMetadataRecords   = launchBoxXMLMaxGames + launchBoxXMLMaxAliases + launchBoxXMLMaxImages + launchBoxXMLMaxPlatforms + launchBoxXMLMaxPlatformAliases + launchBoxXMLMaxEmulators + launchBoxXMLMaxEmulatorPlatforms
	launchBoxXMLMaxPlatformRecords   = launchBoxXMLMaxPlatforms + launchBoxXMLMaxPlatformAliases
)

type launchBoxXMLBudget struct {
	startElements uint64
	attributes    uint64
}

type launchBoxXMLAttribute struct {
	name  string
	value string
}

type launchBoxXMLStart struct {
	name       string
	attributes []launchBoxXMLAttribute
}

// framedXMLReader is a lexical guard placed in front of encoding/xml.Decoder.
// It emits one complete, validated lexical frame at a time. In particular, it
// never lets the decoder see a prefix of a frame that later exceeds a limit.
type framedXMLReader struct {
	reader  *bufio.Reader
	pending []byte

	emittedBytes        int64
	offendingFrameBytes int
	frameHighWater      int
	memberBytes         int64
	depth               int
	stack               []string
	declarationSeen     bool
	rootSeen            bool
	rootComplete        bool
	budget              *launchBoxXMLBudget
}

func newFramedXMLReader(reader io.Reader) *framedXMLReader {
	return newFramedXMLReaderWithBudget(reader, nil)
}

func newFramedXMLReaderWithBudget(reader io.Reader, budget *launchBoxXMLBudget) *framedXMLReader {
	if reader == nil {
		reader = strings.NewReader("")
	}
	return &framedXMLReader{
		reader: bufio.NewReaderSize(reader, 32*1024),
		budget: budget,
	}
}

func (r *framedXMLReader) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		frame, err := r.nextFrame()
		if err != nil {
			return 0, err
		}
		if frame == nil {
			return 0, io.EOF
		}
		r.pending = frame
	}
	if int64(len(r.pending)) > int64(launchBoxXMLMaxMemberBytes)-r.memberBytes {
		return 0, errors.New("launchbox XML member bytes exceed bound")
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	r.emittedBytes += int64(n)
	r.memberBytes += int64(n)
	return n, nil
}

func (r *framedXMLReader) ReadByte() (byte, error) {
	var one [1]byte
	n, err := r.Read(one[:])
	if n == 1 {
		return one[0], nil
	}
	return 0, err
}

func (r *framedXMLReader) nextFrame() ([]byte, error) {
	first, err := r.reader.ReadByte()
	if err == io.EOF {
		if !r.declarationSeen || !r.rootComplete {
			return nil, errors.New("launchbox XML ended before a complete document")
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if first == '<' {
		return r.readMarkup(first)
	}
	return r.readText(first)
}

func (r *framedXMLReader) readText(first byte) ([]byte, error) {
	frame := []byte{first}
	for {
		b, err := r.reader.ReadByte()
		if err == io.EOF {
			if r.rootComplete && !xmlWhitespaceOnly(frame) {
				return nil, errors.New("launchbox XML has non-whitespace after its root")
			}
			if err := r.validateTextFrame(frame); err != nil {
				return nil, err
			}
			return r.finishFrame(frame), nil
		}
		if err != nil {
			return nil, err
		}
		if b == '<' {
			if err := r.reader.UnreadByte(); err != nil {
				return nil, err
			}
			if err := r.validateTextFrame(frame); err != nil {
				return nil, err
			}
			return r.finishFrame(frame), nil
		}
		if len(frame) >= launchBoxXMLMaxTextBytes {
			r.offendingFrameBytes = 0
			return nil, errors.New("launchbox XML text frame exceeds bound")
		}
		frame = append(frame, b)
	}
}

func (r *framedXMLReader) validateTextFrame(frame []byte) error {
	if !utf8.Valid(frame) || !xmlCharactersValid(frame) {
		return errors.New("launchbox XML text frame contains invalid characters")
	}
	if !r.declarationSeen {
		return errors.New("launchbox XML has bytes before its declaration")
	}
	if r.rootComplete && !xmlWhitespaceOnly(frame) {
		return errors.New("launchbox XML has non-whitespace after its root")
	}
	return validateXMLReferences(frame)
}

func (r *framedXMLReader) readMarkup(first byte) ([]byte, error) {
	second, err := r.reader.ReadByte()
	if err != nil {
		return nil, errors.New("launchbox XML has an unterminated markup frame")
	}
	frame := []byte{first, second}
	if !r.declarationSeen && second != '?' {
		return nil, errors.New("launchbox XML has bytes before its declaration")
	}
	switch second {
	case '?':
		if r.rootComplete {
			return nil, errors.New("launchbox XML processing instruction is not allowed")
		}
		if r.declarationSeen {
			return nil, errors.New("launchbox XML processing instruction is not allowed")
		}
		return r.readProcessingInstruction(frame)
	case '!':
		// XML permits comments in the trailing Misc portion after the root;
		// readBang still rejects DTD/directive/CDATA forms.
		return r.readBang(frame)
	case '/':
		if r.rootComplete {
			return nil, errors.New("launchbox XML end tag underflow")
		}
		return r.readEndTag(frame)
	default:
		if r.rootComplete {
			return nil, errors.New("launchbox XML contains more than one root")
		}
		return r.readStartTag(frame)
	}
}

func (r *framedXMLReader) readProcessingInstruction(frame []byte) ([]byte, error) {
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML processing instruction is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxDeclarationBytes {
			return nil, errors.New("launchbox XML processing instruction exceeds bound")
		}
		frame = append(frame, b)
		if len(frame) < 2 || frame[len(frame)-2] != '?' || frame[len(frame)-1] != '>' {
			continue
		}
		if !bytes.HasPrefix(frame, []byte("<?xml")) {
			return nil, errors.New("launchbox XML processing instruction is not the initial declaration")
		}
		if err := validateXMLDeclaration(frame); err != nil {
			return nil, err
		}
		r.declarationSeen = true
		return r.finishFrame(frame), nil
	}
}

func (r *framedXMLReader) readBang(frame []byte) ([]byte, error) {
	if r.rootComplete {
		return r.readTrailingComment(frame)
	}
	third, err := r.reader.ReadByte()
	if err != nil {
		return nil, errors.New("launchbox XML bang frame is unterminated")
	}
	if len(frame) >= launchBoxXMLMaxStartTagBytes {
		return nil, errors.New("launchbox XML bang frame exceeds bound")
	}
	frame = append(frame, third)
	if third == '[' {
		if r.depth == 0 {
			return nil, errors.New("launchbox XML CDATA is outside an element")
		}
		prefix := []byte("<![CDATA[")
		for len(frame) < len(prefix) {
			b, err := r.reader.ReadByte()
			if err != nil {
				return nil, errors.New("launchbox XML CDATA opener is unterminated")
			}
			if len(frame) >= launchBoxXMLMaxCDATAFrameBytes {
				return nil, errors.New("launchbox XML CDATA frame exceeds bound")
			}
			frame = append(frame, b)
		}
		if !bytes.Equal(frame[:len(prefix)], prefix) {
			return nil, errors.New("launchbox XML directive is not CDATA")
		}
		return r.readCDATA(frame)
	}
	if third == '-' {
		fourth, err := r.reader.ReadByte()
		if err != nil || fourth != '-' {
			return nil, errors.New("launchbox XML comment opener is invalid")
		}
		if len(frame) >= launchBoxXMLMaxCommentFrameBytes {
			return nil, errors.New("launchbox XML comment frame exceeds bound")
		}
		frame = append(frame, fourth)
		return r.readComment(frame)
	}
	return nil, errors.New("launchbox XML directives and DTD are not allowed")
}

func (r *framedXMLReader) readTrailingComment(frame []byte) ([]byte, error) {
	third, err := r.reader.ReadByte()
	if err != nil || third != '-' {
		return nil, errors.New("launchbox XML trailing markup is not a comment")
	}
	fourth, err := r.reader.ReadByte()
	if err != nil || fourth != '-' {
		return nil, errors.New("launchbox XML trailing comment opener is invalid")
	}
	frame = append(frame, third, fourth)
	return r.readComment(frame)
}

func (r *framedXMLReader) readCDATA(frame []byte) ([]byte, error) {
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML CDATA is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxCDATAFrameBytes {
			return nil, errors.New("launchbox XML CDATA frame exceeds bound")
		}
		frame = append(frame, b)
		if len(frame) >= 3 && bytes.Equal(frame[len(frame)-3:], []byte("]]>")) {
			payload := frame[len("<![CDATA[") : len(frame)-3]
			if !utf8.Valid(payload) || !xmlCharactersValid(payload) {
				return nil, errors.New("launchbox XML CDATA payload is invalid")
			}
			return r.finishFrame(frame), nil
		}
	}
}

func (r *framedXMLReader) readComment(frame []byte) ([]byte, error) {
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML comment is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxCommentFrameBytes {
			return nil, errors.New("launchbox XML comment frame exceeds bound")
		}
		frame = append(frame, b)
		if len(frame) >= 3 && bytes.Equal(frame[len(frame)-3:], []byte("-->")) {
			body := frame[4 : len(frame)-3]
			if bytes.Contains(body, []byte("--")) || (len(body) > 0 && body[len(body)-1] == '-') || !utf8.Valid(body) || !xmlCharactersValid(body) {
				return nil, errors.New("launchbox XML comment payload is invalid")
			}
			return r.finishFrame(frame), nil
		}
	}
}

func (r *framedXMLReader) readEndTag(frame []byte) ([]byte, error) {
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML end tag is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxEndTagBytes {
			return nil, errors.New("launchbox XML end tag exceeds bound")
		}
		frame = append(frame, b)
		if b != '>' {
			continue
		}
		name := frame[2 : len(frame)-1]
		if len(name) == 0 || xmlSpace(name[0]) {
			return nil, errors.New("launchbox XML end tag has invalid whitespace")
		}
		name = trimXMLSpaceBytes(name)
		if err := validateXMLNameToken(name); err != nil {
			return nil, err
		}
		if len(r.stack) == 0 || r.rootComplete {
			return nil, errors.New("launchbox XML end tag underflow")
		}
		if string(name) != r.stack[len(r.stack)-1] {
			return nil, errors.New("launchbox XML end tag does not match")
		}
		r.stack = r.stack[:len(r.stack)-1]
		r.depth = len(r.stack)
		if len(r.stack) == 0 {
			r.rootComplete = true
		}
		return r.finishFrame(frame), nil
	}
}

func (r *framedXMLReader) readStartTag(frame []byte) ([]byte, error) {
	quote := byte(0)
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML start tag is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxStartTagBytes {
			return nil, errors.New("launchbox XML start tag exceeds bound")
		}
		frame = append(frame, b)
		if quote != 0 {
			if b == quote {
				quote = 0
			}
			continue
		}
		if b == '\'' || b == '"' {
			quote = b
			continue
		}
		if b != '>' {
			continue
		}
		start, empty, err := parseXMLStartTag(frame)
		if err != nil {
			return nil, err
		}
		if r.budget != nil {
			r.budget.startElements++
			r.budget.attributes += uint64(len(start.attributes))
			if r.budget.startElements > launchBoxXMLMaxAggregateStartElements || r.budget.attributes > launchBoxXMLMaxAggregateAttributes {
				return nil, errors.New("launchbox XML aggregate structure exceeds bound")
			}
		}
		if r.rootComplete {
			return nil, errors.New("launchbox XML contains more than one root")
		}
		if len(r.stack) == 0 && !r.rootSeen {
			if start.name != "LaunchBox" || len(start.attributes) != 0 {
				return nil, errors.New("launchbox XML root is invalid")
			}
		}
		if len(r.stack) >= launchBoxXMLMaxDepth {
			return nil, errors.New("launchbox XML depth exceeds bound")
		}
		if !empty {
			if len(r.stack) == 0 && r.rootSeen {
				return nil, errors.New("launchbox XML contains more than one root")
			}
			r.stack = append(r.stack, start.name)
			r.depth = len(r.stack)
			if len(r.stack) == 1 {
				r.rootSeen = true
			}
		} else if len(r.stack) == 0 {
			if r.rootSeen {
				return nil, errors.New("launchbox XML contains more than one root")
			}
			r.rootSeen = true
			r.rootComplete = true
		}
		return r.finishFrame(frame), nil
	}
}

func parseXMLStartTag(frame []byte) (launchBoxXMLStart, bool, error) {
	if len(frame) < 3 || frame[0] != '<' || frame[len(frame)-1] != '>' {
		return launchBoxXMLStart{}, false, errors.New("launchbox XML start tag is malformed")
	}
	rawBody := frame[1 : len(frame)-1]
	if len(rawBody) > 0 && xmlSpace(rawBody[0]) {
		return launchBoxXMLStart{}, false, errors.New("launchbox XML start tag has leading whitespace")
	}
	body := trimXMLSpaceBytes(rawBody)
	empty := false
	if len(body) > 0 && body[len(body)-1] == '/' {
		empty = true
		if len(rawBody) > 0 && xmlSpace(rawBody[len(rawBody)-1]) {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML empty element slash is not adjacent to terminator")
		}
		body = trimXMLSpaceBytes(body[:len(body)-1])
	}
	if len(body) == 0 {
		return launchBoxXMLStart{}, false, errors.New("launchbox XML start tag has no name")
	}

	nameEnd, err := scanXMLName(body)
	if err != nil {
		return launchBoxXMLStart{}, false, err
	}
	name := body[:nameEnd]
	if err := validateXMLNameToken(name); err != nil {
		return launchBoxXMLStart{}, false, err
	}
	pos := nameEnd
	attributes := make([]launchBoxXMLAttribute, 0, 1)
	for pos < len(body) {
		if !xmlSpace(body[pos]) {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML start tag has malformed attribute separation")
		}
		for pos < len(body) && xmlSpace(body[pos]) {
			pos++
		}
		if pos == len(body) {
			break
		}
		attrStart := pos
		attrEnd, err := scanXMLName(body[pos:])
		if err != nil {
			return launchBoxXMLStart{}, false, err
		}
		pos += attrEnd
		attrName := body[attrStart:pos]
		if err := validateXMLNameToken(attrName); err != nil {
			return launchBoxXMLStart{}, false, err
		}
		if string(attrName) != "xml:space" {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute is not the permitted xml:space exception")
		}
		if len(attributes) != 0 {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute is duplicated")
		}
		if pos < len(body) && !xmlSpace(body[pos]) && body[pos] != '=' {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute has malformed equals separation")
		}
		for pos < len(body) && xmlSpace(body[pos]) {
			pos++
		}
		if pos >= len(body) || body[pos] != '=' {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute has no equals sign")
		}
		pos++
		for pos < len(body) && xmlSpace(body[pos]) {
			pos++
		}
		if pos >= len(body) || (body[pos] != '\'' && body[pos] != '"') {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute is not quoted")
		}
		quote := body[pos]
		pos++
		valueStart := pos
		for pos < len(body) && body[pos] != quote {
			pos++
		}
		if pos >= len(body) {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute is unterminated")
		}
		rawValue := body[valueStart:pos]
		if len(rawValue) > launchBoxXMLMaxAttributeBytes || !utf8.Valid(rawValue) || !xmlCharactersValid(rawValue) || bytes.IndexByte(rawValue, '<') >= 0 {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute value exceeds bound")
		}
		decoded, err := decodeXMLReferences(rawValue)
		if err != nil || utf8.RuneCountInString(decoded) > launchBoxXMLMaxAttributeRunes || !xmlCharactersValid([]byte(decoded)) {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attribute value is invalid")
		}
		if string(attrName) == "xml:space" && decoded != "preserve" {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML xml:space value is invalid")
		}
		pos++
		attributes = append(attributes, launchBoxXMLAttribute{name: string(attrName), value: decoded})
		if len(attributes) > launchBoxXMLMaxAttributes {
			return launchBoxXMLStart{}, false, errors.New("launchbox XML attributes exceed bound")
		}
	}
	return launchBoxXMLStart{name: string(name), attributes: attributes}, empty, nil
}

func (r *framedXMLReader) finishFrame(frame []byte) []byte {
	if len(frame) > r.frameHighWater {
		r.frameHighWater = len(frame)
	}
	return frame
}

func validateXMLNameFrame(name []byte) error {
	if len(name) == 0 || len(name) > launchBoxXMLMaxNameBytes || !utf8.Valid(name) || utf8.RuneCount(name) > launchBoxXMLMaxNameRunes {
		return errors.New("launchbox XML name exceeds bound")
	}
	return nil
}

func validateXMLNameToken(name []byte) error {
	if err := validateXMLNameFrame(name); err != nil {
		return err
	}
	end, err := scanXMLName(name)
	if err != nil || end != len(name) {
		return errors.New("launchbox XML name is malformed")
	}
	colonCount := bytes.Count(name, []byte{':'})
	if colonCount > 1 || (colonCount == 1 && (name[0] == ':' || name[len(name)-1] == ':')) {
		return errors.New("launchbox XML qualified name is malformed")
	}
	return nil
}

func scanXMLName(value []byte) (int, error) {
	if len(value) == 0 || !utf8.Valid(value) {
		return 0, errors.New("launchbox XML name is malformed")
	}
	position := 0
	first := true
	for position < len(value) {
		runeValue, size := utf8.DecodeRune(value[position:])
		if runeValue == utf8.RuneError && size == 1 {
			return 0, errors.New("launchbox XML name is malformed")
		}
		if first {
			if !xmlNameStartRune(runeValue) {
				return 0, errors.New("launchbox XML name is malformed")
			}
			first = false
		} else if !xmlNameCharRune(runeValue) {
			break
		}
		position += size
	}
	if first {
		return 0, errors.New("launchbox XML name is empty")
	}
	return position, nil
}

func xmlNameStartRune(value rune) bool {
	return value == ':' || value == '_' ||
		(value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') ||
		(value >= 0xC0 && value <= 0xD6) || (value >= 0xD8 && value <= 0xF6) ||
		(value >= 0xF8 && value <= 0x2FF) || (value >= 0x370 && value <= 0x37D) ||
		(value >= 0x37F && value <= 0x1FFF) || (value >= 0x200C && value <= 0x200D) ||
		(value >= 0x2070 && value <= 0x218F) || (value >= 0x2C00 && value <= 0x2FEF) ||
		(value >= 0x3001 && value <= 0xD7FF) || (value >= 0xF900 && value <= 0xFDCF) ||
		(value >= 0xFDF0 && value <= 0xFFFD) || (value >= 0x10000 && value <= 0xEFFFF)
}

func xmlNameCharRune(value rune) bool {
	return xmlNameStartRune(value) || value == '-' || value == '.' ||
		(value >= '0' && value <= '9') || value == 0xB7 ||
		(value >= 0x300 && value <= 0x36F) || (value >= 0x203F && value <= 0x2040)
}

func validateXMLDeclaration(frame []byte) error {
	if len(frame) > launchBoxXMLMaxDeclarationBytes || !bytes.HasPrefix(frame, []byte("<?xml")) || !bytes.HasSuffix(frame, []byte("?>")) {
		return errors.New("launchbox XML declaration is invalid")
	}
	body := frame[len("<?xml") : len(frame)-2]
	if len(body) == 0 || !xmlSpace(body[0]) {
		return errors.New("launchbox XML declaration has no separating whitespace")
	}
	body = trimXMLSpaceBytes(body)

	position := 0
	seenVersion, seenEncoding, seenStandalone := false, false, false
	order := 0
	for position < len(body) {
		for position < len(body) && xmlSpace(body[position]) {
			position++
		}
		if position == len(body) {
			break
		}
		start := position
		for position < len(body) && body[position] >= 'a' && body[position] <= 'z' {
			position++
		}
		if start == position {
			return errors.New("launchbox XML declaration pseudo-attribute is malformed")
		}
		name := string(body[start:position])
		for position < len(body) && xmlSpace(body[position]) {
			position++
		}
		if position >= len(body) || body[position] != '=' {
			return errors.New("launchbox XML declaration pseudo-attribute has no equals sign")
		}
		position++
		for position < len(body) && xmlSpace(body[position]) {
			position++
		}
		if position >= len(body) || (body[position] != '\'' && body[position] != '"') {
			return errors.New("launchbox XML declaration value is not quoted")
		}
		quote := body[position]
		position++
		valueStart := position
		for position < len(body) && body[position] != quote {
			position++
		}
		if position >= len(body) {
			return errors.New("launchbox XML declaration value is unterminated")
		}
		value := string(body[valueStart:position])
		position++

		switch name {
		case "version":
			if seenVersion || order != 0 || value != "1.0" {
				return errors.New("launchbox XML version policy rejected")
			}
			seenVersion = true
			order = 1
		case "encoding":
			if !seenVersion || seenEncoding || order > 2 || !asciiEqualFold(value, "utf-8") {
				return errors.New("launchbox XML encoding policy rejected")
			}
			seenEncoding = true
			order = 2
		case "standalone":
			if !seenVersion || seenStandalone || order > 2 || value != "yes" {
				return errors.New("launchbox XML standalone policy rejected")
			}
			seenStandalone = true
			order = 3
		default:
			return errors.New("launchbox XML declaration pseudo-attribute is unsupported")
		}
	}
	if !seenVersion {
		return errors.New("launchbox XML declaration has no version")
	}
	return nil
}

func validateXMLReferences(value []byte) error {
	for position := 0; position < len(value); position++ {
		if value[position] != '&' {
			continue
		}
		relativeEnd := bytes.IndexByte(value[position+1:], ';')
		if relativeEnd < 0 {
			return errors.New("launchbox XML entity reference is unterminated")
		}
		length := relativeEnd + 2
		if length > launchBoxXMLMaxEntityBytes {
			return errors.New("launchbox XML entity reference exceeds bound")
		}
		if _, err := decodeXMLReference(value[position : position+length]); err != nil {
			return err
		}
		position += length - 1
	}
	return nil
}

func decodeXMLReferences(value []byte) (string, error) {
	if err := validateXMLReferences(value); err != nil {
		return "", err
	}
	var output strings.Builder
	output.Grow(len(value))
	for position := 0; position < len(value); {
		if value[position] != '&' {
			runeValue, size := utf8.DecodeRune(value[position:])
			if runeValue == utf8.RuneError && size == 1 {
				return "", errors.New("launchbox XML reference value is not UTF-8")
			}
			output.WriteRune(runeValue)
			position += size
			continue
		}
		relativeEnd := bytes.IndexByte(value[position+1:], ';')
		runeValue, err := decodeXMLReference(value[position : position+relativeEnd+2])
		if err != nil {
			return "", err
		}
		output.WriteRune(runeValue)
		position += relativeEnd + 2
	}
	return output.String(), nil
}

func decodeXMLReference(reference []byte) (rune, error) {
	if len(reference) < 3 || reference[0] != '&' || reference[len(reference)-1] != ';' {
		return 0, errors.New("launchbox XML entity reference is malformed")
	}
	switch string(reference) {
	case "&amp;":
		return '&', nil
	case "&apos;":
		return '\'', nil
	case "&gt;":
		return '>', nil
	case "&lt;":
		return '<', nil
	case "&quot;":
		return '"', nil
	}
	body := string(reference[1 : len(reference)-1])
	if len(body) < 2 || body[0] != '#' {
		return 0, errors.New("launchbox XML custom entity is not allowed")
	}
	base := 10
	digits := body[1:]
	if digits[0] == 'x' || digits[0] == 'X' {
		base = 16
		digits = digits[1:]
	}
	if digits == "" {
		return 0, errors.New("launchbox XML numeric entity is empty")
	}
	value, err := strconv.ParseUint(digits, base, 32)
	if err != nil || !xmlRuneValid(rune(value)) {
		return 0, errors.New("launchbox XML numeric entity is invalid")
	}
	return rune(value), nil
}

func xmlRuneValid(value rune) bool {
	return value == 0x9 || value == 0xA || value == 0xD ||
		(value >= 0x20 && value <= 0xD7FF) ||
		(value >= 0xE000 && value <= 0xFFFD) ||
		(value >= 0x10000 && value <= 0x10FFFF)
}

func xmlCharactersValid(value []byte) bool {
	for len(value) > 0 {
		runeValue, size := utf8.DecodeRune(value)
		if (runeValue == utf8.RuneError && size == 1) || !xmlRuneValid(runeValue) {
			return false
		}
		value = value[size:]
	}
	return true
}

func xmlSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func xmlSpaceRune(value rune) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func trimXMLSpaceBytes(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && xmlSpace(value[start]) {
		start++
	}
	for end > start && xmlSpace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func asciiEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		l, r := left[index], right[index]
		if l >= 'A' && l <= 'Z' {
			l += 'a' - 'A'
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}

type launchBoxRecordSink interface {
	putLaunchBoxRecord(launchBoxRecord) error
}

type launchBoxRecord struct {
	member        string
	family        string
	persist       bool
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

type launchBoxAliasKey struct {
	databaseID    string
	alternateName string
	region        string
}

type launchBoxImageKey struct {
	databaseID string
	fileName   string
	typeName   string
	region     string
	crc32      string
}

// launchBoxImageIdentity is the schema identity of an image row. CRC32 is
// integrity metadata, not part of the row identity: a repeated identity with
// a different CRC is a deterministic conflict rather than a second candidate.
type launchBoxImageIdentity struct {
	databaseID string
	fileName   string
	typeName   string
	region     string
}

type launchBoxMemberCounts struct {
	games, aliases, images, platforms, platformAliases, emulators, emulatorPlatforms int
	platformKeys                                                                     map[string]struct{}
	platformAliasKeys                                                                map[string]struct{}

	records              int
	gameIDs              map[string]struct{}
	aliasRawKeys         map[launchBoxAliasKey]struct{}
	aliasNormalizedKeys  map[launchBoxAliasKey]struct{}
	aliasPerGame         map[string]int
	imagePerGame         map[string]int
	platformAliasNames   map[string]struct{}
	imageKeys            map[launchBoxImageKey]struct{}
	imageIdentities      map[launchBoxImageIdentity]string
	blankAliases         int
	normalizedAliasDupes int
	persistedAliases     int
	persistedImages      int
}

func (c *launchBoxMemberCounts) initialize() {
	if c.platformKeys == nil {
		c.platformKeys = make(map[string]struct{}, 8)
	}
	if c.platformAliasKeys == nil {
		c.platformAliasKeys = make(map[string]struct{}, 8)
	}
	if c.gameIDs == nil {
		c.gameIDs = make(map[string]struct{}, 8)
	}
	if c.aliasRawKeys == nil {
		c.aliasRawKeys = make(map[launchBoxAliasKey]struct{}, 8)
	}
	if c.aliasNormalizedKeys == nil {
		c.aliasNormalizedKeys = make(map[launchBoxAliasKey]struct{}, 8)
	}
	if c.aliasPerGame == nil {
		c.aliasPerGame = make(map[string]int)
	}
	if c.imagePerGame == nil {
		c.imagePerGame = make(map[string]int)
	}
	if c.platformAliasNames == nil {
		c.platformAliasNames = make(map[string]struct{}, 8)
	}
	if c.imageKeys == nil {
		c.imageKeys = make(map[launchBoxImageKey]struct{}, 8)
	}
	if c.imageIdentities == nil {
		c.imageIdentities = make(map[launchBoxImageIdentity]string, 8)
	}
}

func (c *launchBoxMemberCounts) validateRecord(record *launchBoxRecord) error {
	c.initialize()
	record.persist = false
	switch record.family {
	case "Game":
		if !validLaunchBoxID(record.game.databaseID) {
			return errors.New("launchbox Game.DatabaseID is invalid")
		}
		if xmlBoundaryTrim(record.game.name) == "" || xmlBoundaryTrim(record.game.platform) == "" {
			return errors.New("launchbox Game required field is empty")
		}
		if _, exists := c.gameIDs[record.game.databaseID]; exists {
			return errors.New("launchbox Game.DatabaseID is duplicated")
		}
		c.gameIDs[record.game.databaseID] = struct{}{}
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
		record.persist = true
	case "GameAlternateName":
		if !validLaunchBoxID(record.alias.databaseID) {
			return errors.New("launchbox alias DatabaseID is invalid")
		}
		if c.aliasPerGame[record.alias.databaseID] >= launchBoxXMLMaxAliasesPerGame {
			return errors.New("launchbox alias per-game limit exceeded")
		}
		c.aliasPerGame[record.alias.databaseID]++
		rawKey := launchBoxAliasKey{record.alias.databaseID, record.alias.alternateName, record.alias.region}
		if _, exists := c.aliasRawKeys[rawKey]; exists {
			return errors.New("launchbox alias raw tuple is duplicated")
		}
		c.aliasRawKeys[rawKey] = struct{}{}
		canonicalKey := launchBoxAliasKey{
			databaseID:    record.alias.databaseID,
			alternateName: xmlBoundaryTrim(record.alias.alternateName),
			region:        xmlBoundaryTrim(record.alias.region),
		}
		record.alias.alternateName = canonicalKey.alternateName
		record.alias.region = canonicalKey.region
		if _, exists := c.aliasNormalizedKeys[canonicalKey]; exists {
			c.normalizedAliasDupes++
			record.persist = false
		} else {
			c.aliasNormalizedKeys[canonicalKey] = struct{}{}
			if canonicalKey.alternateName == "" {
				c.blankAliases++
			}
			record.persist = canonicalKey.alternateName != ""
		}
		if record.persist {
			c.persistedAliases++
		}
	case "GameImage":
		if !validLaunchBoxID(record.image.databaseID) {
			return errors.New("launchbox image DatabaseID is invalid")
		}
		if c.imagePerGame[record.image.databaseID] >= launchBoxXMLMaxImagesPerGame {
			return errors.New("launchbox image per-game limit exceeded")
		}
		if !validLaunchBoxImageFileName(record.image.fileName) || !validLaunchBoxImageType(record.image.typeName) || !validLaunchBoxCRC32(record.image.crc32) {
			return errors.New("launchbox image field is invalid")
		}
		c.imagePerGame[record.image.databaseID]++
		identity := launchBoxImageIdentity{
			databaseID: record.image.databaseID,
			fileName:   record.image.fileName,
			typeName:   record.image.typeName,
			region:     xmlBoundaryTrim(record.image.region),
		}
		record.image.region = identity.region
		if previousCRC, exists := c.imageIdentities[identity]; exists {
			if previousCRC != record.image.crc32 {
				return errors.New("launchbox image tuple has conflicting CRC32")
			}
			return errors.New("launchbox image tuple is duplicated")
		}
		c.imageIdentities[identity] = record.image.crc32
		c.imageKeys[launchBoxImageKey{
			databaseID: record.image.databaseID,
			fileName:   record.image.fileName,
			typeName:   record.image.typeName,
			region:     record.image.region,
			crc32:      record.image.crc32,
		}] = struct{}{}
		record.persist = true
		c.persistedImages++
	case "Platform":
		key := xmlBoundaryTrim(record.platform.name)
		if key == "" {
			return errors.New("launchbox platform name is empty")
		}
		if _, exists := c.platformKeys[key]; exists {
			return errors.New("launchbox platform key is duplicated")
		}
		c.platformKeys[key] = struct{}{}
		record.persist = record.member == "Platforms.xml"
	case "PlatformAlternateName":
		name := xmlBoundaryTrim(record.platformAlias.name)
		alternate := xmlBoundaryTrim(record.platformAlias.alternate)
		if name == "" || alternate == "" {
			return errors.New("launchbox platform alternate is empty")
		}
		key := name + "\x00" + alternate
		if _, exists := c.platformAliasKeys[key]; exists {
			return errors.New("launchbox platform alternate key is duplicated")
		}
		c.platformAliasKeys[key] = struct{}{}
		c.platformAliasNames[name] = struct{}{}
		record.persist = record.member == "Platforms.xml"
	case "Emulator", "EmulatorPlatform":
		// Intentionally unsupported families have no selected fields. Their
		// bounded leaf stream is still parsed and their record cap is enforced.
	default:
		return errors.New("launchbox XML record family is unsupported")
	}
	return nil
}

func (c *launchBoxMemberCounts) validateReferences(member string) error {
	for databaseID := range c.aliasPerGame {
		if _, exists := c.gameIDs[databaseID]; !exists && member == "Metadata.xml" {
			return errors.New("launchbox alias references an unknown Game")
		}
	}
	for databaseID := range c.imagePerGame {
		if _, exists := c.gameIDs[databaseID]; !exists && member == "Metadata.xml" {
			return errors.New("launchbox image references an unknown Game")
		}
	}
	for name := range c.platformAliasNames {
		if _, exists := c.platformKeys[name]; !exists {
			return errors.New("launchbox platform alternate references an unknown Platform")
		}
	}
	return nil
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
		"Fanart - Background", "Screenshot - Gameplay", "Screenshot - Game Title":
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
	}
	return -1
}

func parseLaunchBoxXMLMember(reader io.Reader, member string, budget *launchBoxXMLBudget, sink launchBoxRecordSink) (launchBoxMemberCounts, error) {
	if reader == nil || sink == nil || (member != "Metadata.xml" && member != "Platforms.xml") {
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
				if err := counts.validateReferences(member); err != nil {
					return counts, err
				}
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
			if err := counts.validateRecord(&record); err != nil {
				return counts, err
			}
			if err := sink.putLaunchBoxRecord(record); err != nil {
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

func launchBoxSelectedField(family, field string) bool {
	return launchBoxSelectedFieldBit(family, field) != 0
}

func launchBoxSelectedFieldBit(family, field string) uint64 {
	fields := []string{}
	switch family {
	case "Game":
		fields = []string{"DatabaseID", "Name", "Platform", "Overview", "ReleaseYear", "Genres", "Developer", "Publisher", "MaxPlayers"}
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

type launchBoxTextBuilder struct {
	bytes    []byte
	maxBytes int
	maxRunes int
	runes    int
}

func (b *launchBoxTextBuilder) Append(value []byte) error {
	if !utf8.Valid(value) || !xmlCharactersValid(value) {
		return errors.New("launchbox XML selected value is not valid XML text")
	}
	if len(value) > b.maxBytes-len(b.bytes) {
		return errors.New("launchbox XML selected value exceeds byte bound")
	}
	runeCount := utf8.RuneCount(value)
	if runeCount > b.maxRunes-b.runes {
		return errors.New("launchbox XML selected value exceeds rune bound")
	}
	b.bytes = append(b.bytes, value...)
	b.runes += runeCount
	return nil
}

func (b *launchBoxTextBuilder) String() string { return string(b.bytes) }

func xmlWhitespaceOnly(value []byte) bool {
	for _, character := range value {
		if !xmlSpace(character) {
			return false
		}
	}
	return true
}

func xmlBoundaryTrim(value string) string {
	start, end := 0, len(value)
	for start < end {
		runeValue, size := utf8.DecodeRuneInString(value[start:])
		if !xmlSpaceRune(runeValue) {
			break
		}
		start += size
	}
	for end > start {
		runeValue, size := utf8.DecodeLastRuneInString(value[:end])
		if !xmlSpaceRune(runeValue) {
			break
		}
		end -= size
	}
	return value[start:end]
}
