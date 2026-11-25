package metadata

import (
	"bufio"
	"bytes"
	"errors"
	"io"
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

func (b *launchBoxXMLBudget) reserve(startElements, attributes uint64) error {
	if b == nil {
		return errors.New("launchbox XML budget is unavailable")
	}
	if b.startElements > launchBoxXMLMaxAggregateStartElements || b.attributes > launchBoxXMLMaxAggregateAttributes ||
		startElements > launchBoxXMLMaxAggregateStartElements-b.startElements || attributes > launchBoxXMLMaxAggregateAttributes-b.attributes {
		return errors.New("launchbox XML aggregate structure exceeds bound")
	}
	b.startElements += startElements
	b.attributes += attributes
	return nil
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
		r.offendingFrameBytes = len(r.pending)
		r.noteFrameHighWater(len(r.pending))
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
	defer func() { r.noteFrameHighWater(len(frame)) }()
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
			r.rejectFrame(len(frame) + 1)
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
	frame := []byte{first}
	defer func() { r.noteFrameHighWater(len(frame)) }()
	second, err := r.reader.ReadByte()
	if err != nil {
		return nil, errors.New("launchbox XML has an unterminated markup frame")
	}
	frame = append(frame, second)
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
	defer func() { r.noteFrameHighWater(len(frame)) }()
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML processing instruction is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxDeclarationBytes {
			r.rejectFrame(len(frame) + 1)
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
	defer func() { r.noteFrameHighWater(len(frame)) }()
	if r.rootComplete {
		return r.readTrailingComment(frame)
	}
	third, err := r.reader.ReadByte()
	if err != nil {
		return nil, errors.New("launchbox XML bang frame is unterminated")
	}
	if len(frame) >= launchBoxXMLMaxStartTagBytes {
		r.rejectFrame(len(frame) + 1)
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
				r.rejectFrame(len(frame) + 1)
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
		if err != nil {
			return nil, errors.New("launchbox XML comment opener is invalid")
		}
		if len(frame) >= launchBoxXMLMaxCommentFrameBytes {
			r.rejectFrame(len(frame) + 1)
			return nil, errors.New("launchbox XML comment frame exceeds bound")
		}
		frame = append(frame, fourth)
		if fourth != '-' {
			return nil, errors.New("launchbox XML comment opener is invalid")
		}
		return r.readComment(frame)
	}
	return nil, errors.New("launchbox XML directives and DTD are not allowed")
}

func (r *framedXMLReader) readTrailingComment(frame []byte) ([]byte, error) {
	defer func() { r.noteFrameHighWater(len(frame)) }()
	third, err := r.reader.ReadByte()
	if err != nil {
		return nil, errors.New("launchbox XML trailing markup is not a comment")
	}
	frame = append(frame, third)
	if third != '-' {
		return nil, errors.New("launchbox XML trailing markup is not a comment")
	}
	fourth, err := r.reader.ReadByte()
	if err != nil {
		return nil, errors.New("launchbox XML trailing comment opener is invalid")
	}
	frame = append(frame, fourth)
	if fourth != '-' {
		return nil, errors.New("launchbox XML trailing comment opener is invalid")
	}
	return r.readComment(frame)
}

func (r *framedXMLReader) readCDATA(frame []byte) ([]byte, error) {
	defer func() { r.noteFrameHighWater(len(frame)) }()
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML CDATA is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxCDATAFrameBytes {
			r.rejectFrame(len(frame) + 1)
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
	defer func() { r.noteFrameHighWater(len(frame)) }()
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML comment is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxCommentFrameBytes {
			r.rejectFrame(len(frame) + 1)
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
	defer func() { r.noteFrameHighWater(len(frame)) }()
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML end tag is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxEndTagBytes {
			r.rejectFrame(len(frame) + 1)
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
	defer func() { r.noteFrameHighWater(len(frame)) }()
	quote := byte(0)
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, errors.New("launchbox XML start tag is unterminated")
		}
		if len(frame) >= launchBoxXMLMaxStartTagBytes {
			r.rejectFrame(len(frame) + 1)
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
			if err := r.budget.reserve(1, uint64(len(start.attributes))); err != nil {
				return nil, err
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

func (r *framedXMLReader) noteFrameHighWater(size int) {
	if size > r.frameHighWater {
		r.frameHighWater = size
	}
}

func (r *framedXMLReader) rejectFrame(size int) {
	r.offendingFrameBytes = size
	// Lexical callers pass the attempted size (the buffered frame plus the
	// rejected next byte). Keep the actual buffer high-water separate from
	// that observation; the member-byte caller records its complete frame
	// directly because it has no attempted extra byte.
	if size > 0 {
		r.noteFrameHighWater(size - 1)
	}
}
