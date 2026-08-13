package metadata

import (
	"bytes"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

type launchBoxChunkedReader struct {
	data  []byte
	chunk int
	read  int
}

func (r *launchBoxChunkedReader) Read(dst []byte) (int, error) {
	if r.read == len(r.data) {
		return 0, io.EOF
	}
	limit := len(dst)
	if r.chunk > 0 && limit > r.chunk {
		limit = r.chunk
	}
	if remaining := len(r.data) - r.read; limit > remaining {
		limit = remaining
	}
	n := copy(dst[:limit], r.data[r.read:r.read+limit])
	r.read += n
	return n, nil
}

type launchBoxSplitReader struct {
	data  []byte
	split int
	read  int
}

func (r *launchBoxSplitReader) Read(dst []byte) (int, error) {
	if r.read == len(r.data) {
		return 0, io.EOF
	}
	limit := len(dst)
	if r.read < r.split && limit > r.split-r.read {
		limit = r.split - r.read
	}
	if remaining := len(r.data) - r.read; limit > remaining {
		limit = remaining
	}
	n := copy(dst[:limit], r.data[r.read:r.read+limit])
	r.read += n
	return n, nil
}

func launchBoxDeclarationWithLength(length int) string {
	const prefix = `<?xml version="1.0"`
	const suffix = `?>`
	if length < len(prefix)+1+len(suffix) {
		panic("declaration length is too small")
	}
	return prefix + string(bytes.Repeat([]byte{' '}, length-len(prefix)-len(suffix))) + suffix
}

func launchBoxReadFramedFrom(reader io.Reader) (*framedXMLReader, error) {
	framed := newFramedXMLReader(reader)
	_, err := io.Copy(io.Discard, framed)
	return framed, err
}

func launchBoxReadFramed(input []byte, chunk int) (*framedXMLReader, error) {
	return launchBoxReadFramedFrom(&launchBoxChunkedReader{data: input, chunk: chunk})
}

func launchBoxReadFramedAtSplit(input []byte, split int) (*framedXMLReader, error) {
	return launchBoxReadFramedFrom(&launchBoxSplitReader{data: input, split: split})
}

func launchBoxDelimiterSplits(prefix, frame string) []int {
	positions := map[int]struct{}{
		len(prefix):                  {},
		len(prefix) + 1:              {},
		len(prefix) + len(frame) - 1: {},
		len(prefix) + len(frame):     {},
	}
	for index := range frame {
		if bytes.Contains([]byte("<>/?'\"-"), []byte{frame[index]}) {
			for _, delta := range []int{-1, 0, 1} {
				position := len(prefix) + index + delta
				if position >= 0 && position <= len(prefix)+len(frame) {
					positions[position] = struct{}{}
				}
			}
		}
	}
	out := make([]int, 0, len(positions))
	for position := range positions {
		out = append(out, position)
	}
	sort.Ints(out)
	return out
}

func launchBoxFramingDeclaration() string {
	return `<?xml version="1.0" standalone="yes"?>`
}

func launchBoxStartTagWithLength(length int) string {
	const prefix = `<N`
	const suffix = `/>`
	value := strings.Repeat("x", 256)
	var builder strings.Builder
	builder.WriteString(prefix)
	for index := 0; index < launchBoxXMLMaxAttributes; index++ {
		builder.WriteString(` a`)
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString(`="`)
		builder.WriteString(value)
		builder.WriteByte('"')
	}
	builder.WriteString(suffix)
	base := builder.String()
	if length < len(base) {
		panic("start-tag length is too small")
	}
	padding := length - len(base)
	return prefix + strings.Repeat(" ", padding) + base[len(prefix):]
}

func launchBoxEndTagWithLength(length int) string {
	const name = "LaunchBox"
	const prefix = "</" + name
	const suffix = ">"
	if length < len(prefix)+len(suffix) {
		panic("end-tag length is too small")
	}
	return prefix + strings.Repeat(" ", length-len(prefix)-len(suffix)) + suffix
}

func launchBoxValidMaxEntity(length int) string {
	if length < 4 {
		panic("entity length is too small")
	}
	digits := length - 3
	if digits < 2 {
		panic("entity length cannot encode a value")
	}
	return "&#" + strings.Repeat("0", digits-2) + "65;"
}

func launchBoxOversizedFrameCases() []struct {
	name       string
	limit      int
	exact      []byte
	over       []byte
	frameStart string
} {
	declaration := launchBoxFramingDeclaration()
	declarationMax := launchBoxDeclarationWithLength(launchBoxXMLMaxDeclarationBytes)
	declarationOver := launchBoxDeclarationWithLength(launchBoxXMLMaxDeclarationBytes + 1)
	textPrefix := declaration + `<LaunchBox><Unknown>`
	textSuffix := `</Unknown></LaunchBox>`
	textMax := strings.Repeat("x", launchBoxXMLMaxTextBytes)
	textOver := strings.Repeat("x", launchBoxXMLMaxTextBytes+1)
	cdataPrefix := declaration + `<LaunchBox><Unknown>`
	cdataSuffix := `</Unknown></LaunchBox>`
	cdataPayloadMax := launchBoxXMLMaxCDATAFrameBytes - len(`<![CDATA[`) - len(`]]>`)
	cdataMax := `<![CDATA[` + strings.Repeat("x", cdataPayloadMax) + `]]>`
	cdataOver := `<![CDATA[` + strings.Repeat("x", cdataPayloadMax+1) + `]]>`
	commentPrefix := declaration + `<LaunchBox>`
	commentSuffix := `</LaunchBox>`
	commentPayloadMax := launchBoxXMLMaxCommentFrameBytes - len(`<!--`) - len(`-->`)
	commentMax := `<!--` + strings.Repeat("x", commentPayloadMax) + `-->`
	commentOver := `<!--` + strings.Repeat("x", commentPayloadMax+1) + `-->`
	startPrefix := declaration + `<LaunchBox>`
	startSuffix := `</LaunchBox>`
	startMax := launchBoxStartTagWithLength(launchBoxXMLMaxStartTagBytes)
	startOver := launchBoxStartTagWithLength(launchBoxXMLMaxStartTagBytes + 1)
	endPrefix := declaration + `<LaunchBox>`
	endMax := launchBoxEndTagWithLength(launchBoxXMLMaxEndTagBytes)
	endOver := launchBoxEndTagWithLength(launchBoxXMLMaxEndTagBytes + 1)
	return []struct {
		name       string
		limit      int
		exact      []byte
		over       []byte
		frameStart string
	}{
		{name: "declaration", limit: launchBoxXMLMaxDeclarationBytes, exact: []byte(declarationMax + `<LaunchBox/>`), over: []byte(declarationOver + `<LaunchBox/>`), frameStart: ""},
		{name: "ordinary text", limit: launchBoxXMLMaxTextBytes, exact: []byte(textPrefix + textMax + textSuffix), over: []byte(textPrefix + textOver + textSuffix), frameStart: textPrefix},
		{name: "CDATA", limit: launchBoxXMLMaxCDATAFrameBytes, exact: []byte(cdataPrefix + cdataMax + cdataSuffix), over: []byte(cdataPrefix + cdataOver + cdataSuffix), frameStart: cdataPrefix},
		{name: "comment", limit: launchBoxXMLMaxCommentFrameBytes, exact: []byte(commentPrefix + commentMax + commentSuffix), over: []byte(commentPrefix + commentOver + commentSuffix), frameStart: commentPrefix},
		{name: "start tag", limit: launchBoxXMLMaxStartTagBytes, exact: []byte(startPrefix + startMax + startSuffix), over: []byte(startPrefix + startOver + startSuffix), frameStart: startPrefix},
		{name: "end tag", limit: launchBoxXMLMaxEndTagBytes, exact: []byte(endPrefix + endMax), over: []byte(endPrefix + endOver), frameStart: endPrefix},
	}
}

func TestFramedXMLReaderAcceptsEveryFrameMaximumAndRejectsMaximumPlusOne(t *testing.T) {
	for _, testCase := range launchBoxOversizedFrameCases() {
		t.Run(testCase.name, func(t *testing.T) {
			for _, mode := range []struct {
				name  string
				chunk int
			}{
				{name: "one-byte", chunk: 1},
				{name: "32KiB", chunk: 32 * 1024},
			} {
				t.Run(mode.name, func(t *testing.T) {
					reader, err := launchBoxReadFramed(testCase.exact, mode.chunk)
					if err != nil {
						t.Fatalf("exact maximum rejected: %v", err)
					}
					if reader.frameHighWater != testCase.limit {
						t.Fatalf("exact frame was not measured: high_water=%d limit=%d", reader.frameHighWater, testCase.limit)
					}
					if reader.offendingFrameBytes != 0 {
						t.Fatalf("exact maximum unexpectedly recorded as offending: %d", reader.offendingFrameBytes)
					}
					overReader, err := launchBoxReadFramed(testCase.over, mode.chunk)
					if err == nil {
						t.Fatal("maximum-plus-one frame was accepted")
					}
					if overReader.emittedBytes != int64(len(testCase.frameStart)) {
						t.Fatalf("offending frame bytes were emitted: got=%d want=%d", overReader.emittedBytes, len(testCase.frameStart))
					}
					if overReader.offendingFrameBytes != testCase.limit+1 {
						t.Fatalf("unexpected attempted offending size: got=%d want=%d", overReader.offendingFrameBytes, testCase.limit+1)
					}
					if overReader.frameHighWater != testCase.limit {
						t.Fatalf("actual frame high-water conflated attempted size: got=%d want=%d", overReader.frameHighWater, testCase.limit)
					}
				})
			}
			for _, split := range launchBoxDelimiterSplits(testCase.frameStart, string(testCase.exact[len(testCase.frameStart):])) {
				reader, err := launchBoxReadFramedAtSplit(testCase.exact, split)
				if err != nil {
					t.Fatalf("delimiter-near split %d rejected exact maximum: %v", split, err)
				}
				if reader.frameHighWater != testCase.limit {
					t.Fatalf("split exact frame was not measured: high_water=%d limit=%d split=%d", reader.frameHighWater, testCase.limit, split)
				}
				if reader.offendingFrameBytes != 0 {
					t.Fatalf("split exact maximum unexpectedly recorded as offending: split=%d offending=%d", split, reader.offendingFrameBytes)
				}
				overReader, err := launchBoxReadFramedAtSplit(testCase.over, split)
				if err == nil {
					t.Fatalf("delimiter-near split %d accepted maximum-plus-one", split)
				}
				if overReader.emittedBytes != int64(len(testCase.frameStart)) || overReader.offendingFrameBytes != testCase.limit+1 || overReader.frameHighWater != testCase.limit {
					t.Fatalf("split over-boundary instrumentation mismatch: split=%d emitted=%d offending=%d high_water=%d", split, overReader.emittedBytes, overReader.offendingFrameBytes, overReader.frameHighWater)
				}
			}
		})
	}
}

func TestFramedXMLReaderRejectsNameAndAttributeMaximumPlusOneBeforeEmission(t *testing.T) {
	declaration := launchBoxFramingDeclaration()
	prefix := declaration + `<LaunchBox>`
	for _, testCase := range []struct {
		name  string
		exact string
		over  string
	}{
		{name: "element name bytes", exact: `<` + strings.Repeat("N", launchBoxXMLMaxNameBytes) + `/>`, over: `<` + strings.Repeat("N", launchBoxXMLMaxNameBytes+1) + `/>`},
		{name: "attribute bytes", exact: `<N a="` + strings.Repeat("𐀀", launchBoxXMLMaxAttributeBytes/4) + `"/>`, over: `<N a="` + strings.Repeat("𐀀", launchBoxXMLMaxAttributeBytes/4) + `x"/>`},
		{name: "attribute runes", exact: `<N a="` + strings.Repeat("𐀀", launchBoxXMLMaxAttributeRunes) + `"/>`, over: `<N a="` + strings.Repeat("𐀀", launchBoxXMLMaxAttributeRunes+1) + `"/>`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reader, err := launchBoxReadFramed([]byte(prefix+testCase.exact+`</LaunchBox>`), 1)
			if err != nil {
				t.Fatalf("exact maximum rejected: %v", err)
			}
			if reader.emittedBytes != int64(len(prefix+testCase.exact+`</LaunchBox>`)) {
				t.Fatalf("exact maximum was not fully emitted: %d", reader.emittedBytes)
			}
			overReader, err := launchBoxReadFramed([]byte(prefix+testCase.over+`</LaunchBox>`), 1)
			if err == nil {
				t.Fatal("maximum-plus-one value was accepted")
			}
			if overReader.emittedBytes != int64(len(prefix)) {
				t.Fatalf("offending frame prefix was emitted: got=%d want=%d", overReader.emittedBytes, len(prefix))
			}
			if overReader.frameHighWater < len(testCase.over) {
				t.Fatalf("semantic failure under-reported actual frame bytes: got=%d want-at-least=%d", overReader.frameHighWater, len(testCase.over))
			}
			if overReader.offendingFrameBytes != 0 {
				t.Fatalf("semantic failure conflated an attempted unbuffered byte: %d", overReader.offendingFrameBytes)
			}
		})
	}
}

func TestFramedXMLReaderAcceptsAndRejectsEntityReferenceMaximum(t *testing.T) {
	prefix := launchBoxFramingDeclaration() + `<LaunchBox><Unknown>`
	suffix := `</Unknown></LaunchBox>`
	exactEntity := launchBoxValidMaxEntity(launchBoxXMLMaxEntityBytes)
	overEntity := launchBoxValidMaxEntity(launchBoxXMLMaxEntityBytes + 1)
	if reader, err := launchBoxReadFramed([]byte(prefix+exactEntity+suffix), 1); err != nil {
		t.Fatalf("exact entity maximum rejected: %v", err)
	} else if reader.offendingFrameBytes != 0 {
		t.Fatalf("exact entity unexpectedly recorded as offending: %d", reader.offendingFrameBytes)
	}
	reader, err := launchBoxReadFramed([]byte(prefix+overEntity+suffix), 32*1024)
	if err == nil {
		t.Fatal("entity reference maximum-plus-one accepted")
	}
	if reader.emittedBytes != int64(len(prefix)) {
		t.Fatalf("offending entity frame prefix was emitted: got=%d want=%d", reader.emittedBytes, len(prefix))
	}
	if reader.frameHighWater < len(overEntity) {
		t.Fatalf("entity semantic failure under-reported actual frame bytes: got=%d want-at-least=%d", reader.frameHighWater, len(overEntity))
	}
	if reader.offendingFrameBytes != 0 {
		t.Fatalf("entity semantic failure conflated an attempted unbuffered byte: %d", reader.offendingFrameBytes)
	}
}

func TestFramedXMLReaderRejectsMalformedAndTruncatedHostileFrames(t *testing.T) {
	declaration := launchBoxFramingDeclaration()
	for name, input := range map[string]string{
		"truncated declaration":  declaration[:len(declaration)-1],
		"truncated start tag":    declaration + `<LaunchBox><Game`,
		"truncated end tag":      declaration + `<LaunchBox></LaunchBox`,
		"truncated CDATA":        declaration + `<LaunchBox><![CDATA[x`,
		"truncated comment":      declaration + `<LaunchBox><!--x`,
		"unterminated entity":    declaration + `<LaunchBox><N>&amp</N></LaunchBox>`,
		"invalid numeric entity": declaration + `<LaunchBox><N>&#x110000;</N></LaunchBox>`,
		"invalid UTF-8":          declaration + `<LaunchBox><N>` + string([]byte{0xff}) + `</N></LaunchBox>`,
		"nested DTD":             declaration + `<!DOCTYPE LaunchBox [<!ELEMENT LaunchBox ANY>]><LaunchBox/>`,
		"initial non-XML PI":     `<?pi?><LaunchBox/>`,
		"internal PI":            declaration + `<LaunchBox><?pi?></LaunchBox>`,
		"comment double hyphen":  declaration + `<LaunchBox><!--a--b--></LaunchBox>`,
		"CDATA outside root":     declaration + `<![CDATA[x]]><LaunchBox/>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := launchBoxReadFramed([]byte(input), 1); err == nil {
				t.Fatal("hostile malformed or truncated frame was accepted")
			}
		})
	}
}

func TestFramedXMLReaderTracksRejectedSemanticAndTruncatedFrameBytes(t *testing.T) {
	declaration := launchBoxFramingDeclaration()
	cases := []struct {
		name        string
		prefix      string
		frame       string
		suffix      string
		wantAtLeast int
	}{
		{
			name:        "attribute maximum plus one",
			prefix:      declaration + `<LaunchBox>`,
			frame:       `<N a="` + strings.Repeat("x", launchBoxXMLMaxAttributeBytes+1) + `"/>`,
			suffix:      `</LaunchBox>`,
			wantAtLeast: len(`<N a="`) + launchBoxXMLMaxAttributeBytes + 1 + len(`"/>`),
		},
		{
			name:        "name maximum plus one",
			prefix:      declaration + `<LaunchBox>`,
			frame:       `<` + strings.Repeat("N", launchBoxXMLMaxNameBytes+1) + `/>`,
			suffix:      `</LaunchBox>`,
			wantAtLeast: 1 + launchBoxXMLMaxNameBytes + 1 + len(`/>`),
		},
		{
			name:        "entity maximum plus one",
			prefix:      declaration + `<LaunchBox><N>`,
			frame:       launchBoxValidMaxEntity(launchBoxXMLMaxEntityBytes + 1),
			suffix:      `</N></LaunchBox>`,
			wantAtLeast: len(launchBoxValidMaxEntity(launchBoxXMLMaxEntityBytes + 1)),
		},
		{
			name:        "unterminated entity",
			prefix:      declaration + `<LaunchBox><N>`,
			frame:       `&amp`,
			suffix:      `</N></LaunchBox>`,
			wantAtLeast: len(`&amp`),
		},
		{
			name:        "malformed comment payload",
			prefix:      declaration + `<LaunchBox>`,
			frame:       `<!--a--b-->`,
			suffix:      `</LaunchBox>`,
			wantAtLeast: len(`<!--a--b-->`),
		},
		{
			name:        "truncated comment",
			prefix:      declaration + `<LaunchBox>`,
			frame:       `<!--` + strings.Repeat("x", 4096),
			wantAtLeast: 4100,
		},
		{
			name:        "truncated start tag",
			prefix:      declaration + `<LaunchBox>`,
			frame:       `<N a="` + strings.Repeat("x", 128),
			wantAtLeast: len(`<N a="`) + 128,
		},
		{
			name:        "truncated end tag",
			prefix:      declaration + `<LaunchBox>`,
			frame:       `</LaunchBox`,
			wantAtLeast: len(`</LaunchBox`),
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			for _, mode := range []struct {
				name  string
				chunk int
			}{
				{name: "one-byte", chunk: 1},
				{name: "32KiB", chunk: 32 * 1024},
			} {
				t.Run(mode.name, func(t *testing.T) {
					input := []byte(testCase.prefix + testCase.frame + testCase.suffix)
					reader, err := launchBoxReadFramed(input, mode.chunk)
					if err == nil {
						t.Fatal("hostile frame was accepted")
					}
					if reader.emittedBytes != int64(len(testCase.prefix)) {
						t.Fatalf("offending frame bytes were emitted: got=%d want=%d", reader.emittedBytes, len(testCase.prefix))
					}
					if reader.frameHighWater < testCase.wantAtLeast {
						t.Fatalf("frame high-water under-reports actual buffered bytes: got=%d want-at-least=%d", reader.frameHighWater, testCase.wantAtLeast)
					}
					if reader.offendingFrameBytes != 0 {
						t.Fatalf("semantic/truncation failure conflated an attempted unbuffered byte: %d", reader.offendingFrameBytes)
					}
				})
			}

			for _, split := range launchBoxDelimiterSplits(testCase.prefix, testCase.frame) {
				reader, err := launchBoxReadFramedAtSplit([]byte(testCase.prefix+testCase.frame+testCase.suffix), split)
				if err == nil {
					t.Fatalf("delimiter split %d accepted hostile frame", split)
				}
				if reader.emittedBytes != int64(len(testCase.prefix)) {
					t.Fatalf("delimiter split %d emitted offending frame bytes: got=%d want=%d", split, reader.emittedBytes, len(testCase.prefix))
				}
				if reader.frameHighWater < testCase.wantAtLeast {
					t.Fatalf("delimiter split %d under-reported actual buffered bytes: got=%d want-at-least=%d", split, reader.frameHighWater, testCase.wantAtLeast)
				}
				if reader.offendingFrameBytes != 0 {
					t.Fatalf("delimiter split %d conflated an attempted unbuffered byte: %d", split, reader.offendingFrameBytes)
				}
			}
		})
	}
}

func TestParseLaunchBoxXMLMemberRejectsDuplicateImageTuple(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>42</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><Region>World</Region><CRC32>1</CRC32></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><Region>World</Region><CRC32>1</CRC32></GameImage>` +
		`</LaunchBox>`
	table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
	if _, err := parseLaunchBoxXMLMembers(strings.NewReader(input), strings.NewReader(`<?xml version="1.0" standalone="yes"?><LaunchBox/>`), table); err == nil {
		t.Fatal("duplicate image tuple accepted")
	}
}

func TestParseLaunchBoxXMLMembersRejectsConflictingImageTuple(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>42</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><Region>World</Region><CRC32>1</CRC32></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><Region>World</Region><CRC32>2</CRC32></GameImage>` +
		`</LaunchBox>`
	table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
	if _, err := parseLaunchBoxXMLMembers(strings.NewReader(input), strings.NewReader(`<?xml version="1.0" standalone="yes"?><LaunchBox/>`), table); err == nil {
		t.Fatal("conflicting image tuple was accepted")
	}
	if countTestLaunchBoxEvent(table.batch.events, "abort") != 1 || len(table.batch.committed) != 0 {
		t.Fatalf("conflicting image tuple was not atomic: events=%v committed=%d", table.batch.events, len(table.batch.committed))
	}
}

func TestParseLaunchBoxXMLMembersRejectsUnknownAliasAndImageReferences(t *testing.T) {
	for name, record := range map[string]string{
		"alias": `<GameAlternateName><DatabaseID>99</DatabaseID><AlternateName>Alias</AlternateName></GameAlternateName>`,
		"image": `<GameImage><DatabaseID>99</DatabaseID><FileName>cover_99.jpg</FileName><Type>Box - Front</Type><CRC32>1</CRC32></GameImage>`,
	} {
		t.Run(name, func(t *testing.T) {
			input := launchBoxFramingDeclaration() + `<LaunchBox>` + record + `</LaunchBox>`
			table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
			if _, err := parseLaunchBoxXMLMembers(strings.NewReader(input), strings.NewReader(`<?xml version="1.0" standalone="yes"?><LaunchBox/>`), table); err == nil {
				t.Fatal("unknown record reference was accepted")
			}
			if countTestLaunchBoxEvent(table.batch.events, "abort") != 1 || len(table.batch.committed) != 0 {
				t.Fatalf("unknown reference was not atomic: events=%v committed=%d", table.batch.events, len(table.batch.committed))
			}
		})
	}
}

func TestFramedXMLReaderRejectsNonXMLSpaceLexicalAttributes(t *testing.T) {
	for _, input := range []string{
		`<?xml version="1.0" standalone="yes"?><LaunchBox><Game><DatabaseID>1</DatabaseID><Name space="preserve">Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game></LaunchBox>`,
		`<?xml version="1.0" standalone="yes"?><LaunchBox><Game><DatabaseID>1</DatabaseID><Name p:space="preserve">Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game></LaunchBox>`,
		`<?xml version="1.0" standalone="yes"?><LaunchBox><Game><DatabaseID>1</DatabaseID><Name xmlns:xml="http://www.w3.org/XML/1998/namespace">Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game></LaunchBox>`,
	} {
		if _, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, newTestLaunchBoxRecordBatch()); err == nil {
			t.Fatalf("non-lexical XML-space attribute accepted: %q", input)
		}
	}
}

func TestLaunchBoxXMLSpaceAttributeUsesExpandedNamespaceAndExactPlacement(t *testing.T) {
	base := `<?xml version="1.0" standalone="yes"?><LaunchBox><GameAlternateName><DatabaseID>1</DatabaseID>`
	for name, attribute := range map[string]string{
		"double quote":                     ` xml:space="preserve"`,
		"single quote with XML whitespace": " xml:space	=	'preserve'",
	} {
		t.Run("accepted "+name, func(t *testing.T) {
			input := base + `<AlternateName` + attribute + `>  Alias  </AlternateName></GameAlternateName></LaunchBox>`
			batch := newTestLaunchBoxRecordBatch()
			if _, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, batch); err != nil {
				t.Fatalf("namespace-correct xml:space was rejected: %v", err)
			}
			if got := batch.observed[0].alias.alternateName; got != "  Alias  " {
				t.Fatalf("xml:space preserve changed raw value: %q", got)
			}
		})
	}

	for name, suffix := range map[string]string{
		"default value":            `<AlternateName xml:space="default">Alias</AlternateName>`,
		"wrong lexical namespace":  `<AlternateName space="preserve">Alias</AlternateName>`,
		"wrong prefixed namespace": `<AlternateName p:space="preserve">Alias</AlternateName>`,
		"namespace declaration":    `<AlternateName xmlns:xml="http://www.w3.org/XML/1998/namespace">Alias</AlternateName>`,
		"wrong selected field":     `<Region xml:space="preserve">US</Region><AlternateName>Alias</AlternateName>`,
		"record placement":         `<GameAlternateName xml:space="preserve"><DatabaseID>1</DatabaseID><AlternateName>Alias</AlternateName></GameAlternateName>`,
		"game placement":           `</GameAlternateName><Game xml:space="preserve"><DatabaseID>1</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game><GameAlternateName><DatabaseID>1</DatabaseID><AlternateName>Alias</AlternateName>`,
	} {
		t.Run("rejected "+name, func(t *testing.T) {
			var input string
			if strings.HasPrefix(suffix, `</GameAlternateName>`) {
				input = base + suffix + `</LaunchBox>`
			} else if strings.HasPrefix(suffix, `<GameAlternateName`) {
				input = `<?xml version="1.0" standalone="yes"?><LaunchBox>` + suffix + `</LaunchBox>`
			} else {
				input = base + suffix + `</GameAlternateName></LaunchBox>`
			}
			if _, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, newTestLaunchBoxRecordBatch()); err == nil {
				t.Fatal("xml:space placement or namespace variant was accepted")
			}
		})
	}
}

func TestFramedXMLReaderRejectsMissingDeclarationPseudoAttributeSeparator(t *testing.T) {
	input := `<?xml version="1.0"encoding="utf-8"?><LaunchBox/>`
	if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(input))); err == nil {
		t.Fatal("declaration pseudo-attributes without XML whitespace were accepted")
	}
}

func TestFramedXMLReaderAcceptsDeclarationPolicyVariants(t *testing.T) {
	for name, declaration := range map[string]string{
		"version only":            `<?xml version="1.0"?>`,
		"version single quoted":   `<?xml version='1.0'?>`,
		"encoding":                `<?xml version = "1.0" encoding = 'UTF-8'?>`,
		"standalone":              "<?xml	version='1.0'\nstandalone=\"yes\"?>",
		"encoding and standalone": `<?xml version="1.0" encoding="utf-8" standalone="yes"?>`,
		"mixed XML whitespace":    "<?xml\rversion	=	'1.0'\rencoding = \"uTf-8\"\nstandalone='yes'?>",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := launchBoxReadFramed([]byte(declaration+`<LaunchBox/>`), 1); err != nil {
				t.Fatalf("accepted declaration variant rejected: %v", err)
			}
		})
	}
}

func TestFramedXMLReaderRejectsDeclarationBoundaryAndPolicyVariants(t *testing.T) {
	for name, declaration := range map[string]string{
		"missing initial whitespace":         `<?xmlversion="1.0"?>`,
		"missing pseudo-attribute separator": `<?xml version="1.0"encoding="utf-8"?>`,
		"missing equals":                     `<?xml version "1.0"?>`,
		"unquoted value":                     `<?xml version=1.0?>`,
		"unterminated value":                 `<?xml version="1.0?>`,
		"wrong version":                      `<?xml version="1.1"?>`,
		"wrong encoding":                     `<?xml version="1.0" encoding="utf-16"?>`,
		"standalone no":                      `<?xml version="1.0" standalone="no"?>`,
		"unknown pseudo-attribute":           `<?xml version="1.0" foo="bar"?>`,
		"duplicate version":                  `<?xml version="1.0" version="1.0"?>`,
		"encoding before version":            `<?xml encoding="utf-8" version="1.0"?>`,
		"standalone before encoding":         `<?xml version="1.0" standalone="yes" encoding="utf-8"?>`,
		"quote mismatch":                     `<?xml version="1.0'?>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := launchBoxReadFramed([]byte(declaration+`<LaunchBox/>`), 1); err == nil {
				t.Fatal("malformed declaration was accepted")
			}
		})
	}
}

func TestFramedXMLReaderRejectsBOMAndMisplacedDeclarations(t *testing.T) {
	for name, input := range map[string]string{
		"BOM":                          "\ufeff" + launchBoxFramingDeclaration() + `<LaunchBox/>`,
		"leading whitespace":           " " + launchBoxFramingDeclaration() + `<LaunchBox/>`,
		"leading character data":       "x" + launchBoxFramingDeclaration() + `<LaunchBox/>`,
		"second declaration":           launchBoxFramingDeclaration() + launchBoxFramingDeclaration() + `<LaunchBox/>`,
		"declaration after root":       launchBoxFramingDeclaration() + `<LaunchBox/>` + launchBoxFramingDeclaration(),
		"processing instruction":       launchBoxFramingDeclaration() + `<?pi?><LaunchBox/>`,
		"processing instruction after": launchBoxFramingDeclaration() + `<LaunchBox/><?pi?>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := launchBoxReadFramed([]byte(input), 1); err == nil {
				t.Fatal("misplaced or unsafe declaration was accepted")
			}
		})
	}
}

func TestLaunchBoxSelectedFieldLimitsAcceptExactByteAndRuneBounds(t *testing.T) {
	// This is a direct production-equivalent seam: parseLaunchBoxLeaf uses
	// launchBoxTextBuilder for every selected scalar, so the large Overview and
	// Genres limits can be exercised without allocating millions of records.
	fields := []struct {
		family string
		field  string
	}{
		{family: "Game", field: "DatabaseID"},
		{family: "Game", field: "Name"},
		{family: "Game", field: "Platform"},
		{family: "Game", field: "Overview"},
		{family: "Game", field: "ReleaseYear"},
		{family: "Game", field: "Genres"},
		{family: "Game", field: "Developer"},
		{family: "Game", field: "Publisher"},
		{family: "Game", field: "MaxPlayers"},
		{family: "GameAlternateName", field: "DatabaseID"},
		{family: "GameAlternateName", field: "AlternateName"},
		{family: "GameAlternateName", field: "Region"},
		{family: "GameImage", field: "DatabaseID"},
		{family: "GameImage", field: "FileName"},
		{family: "GameImage", field: "Type"},
		{family: "GameImage", field: "Region"},
		{family: "GameImage", field: "CRC32"},
		{family: "Platform", field: "Name"},
		{family: "PlatformAlternateName", field: "Name"},
		{family: "PlatformAlternateName", field: "Alternate"},
	}
	for _, field := range fields {
		t.Run(field.family+"/"+field.field, func(t *testing.T) {
			selected, maxBytes, maxRunes := launchBoxFieldLimit(field.family, field.field)
			if !selected {
				t.Fatal("field is not selected")
			}

			exactBytes := launchBoxValueWithExactBytes(maxBytes)
			if len(exactBytes) != maxBytes || utf8.RuneCountInString(exactBytes) > maxRunes {
				t.Fatalf("test value cannot reach byte boundary: bytes=%d runes=%d max_bytes=%d max_runes=%d", len(exactBytes), utf8.RuneCountInString(exactBytes), maxBytes, maxRunes)
			}
			builder := launchBoxTextBuilder{maxBytes: maxBytes, maxRunes: maxRunes}
			if err := builder.Append([]byte(exactBytes)); err != nil {
				t.Fatalf("exact byte maximum rejected: %v", err)
			}

			byteOver := launchBoxValueWithExactBytes(maxBytes + 1)
			builder = launchBoxTextBuilder{maxBytes: maxBytes, maxRunes: maxRunes}
			if err := builder.Append([]byte(byteOver)); err == nil {
				t.Fatal("byte maximum plus one accepted")
			}

			exactRunes := strings.Repeat("x", maxRunes)
			if len(exactRunes) > maxBytes {
				t.Fatalf("test value cannot reach rune boundary: bytes=%d runes=%d max_bytes=%d max_runes=%d", len(exactRunes), utf8.RuneCountInString(exactRunes), maxBytes, maxRunes)
			}
			builder = launchBoxTextBuilder{maxBytes: maxBytes, maxRunes: maxRunes}
			if err := builder.Append([]byte(exactRunes)); err != nil {
				t.Fatalf("exact rune maximum rejected: %v", err)
			}

			runeOver := strings.Repeat("x", maxRunes+1)
			builder = launchBoxTextBuilder{maxBytes: maxBytes, maxRunes: maxRunes}
			if err := builder.Append([]byte(runeOver)); err == nil {
				t.Fatal("rune maximum plus one accepted")
			}
		})
	}
}

func launchBoxValueWithExactBytes(size int) string {
	if size < 0 {
		panic("negative value size")
	}
	return strings.Repeat("𐀀", size/4) + strings.Repeat("x", size%4)
}

func launchBoxSingleGameMember(databaseID, fields string) string {
	return launchBoxFramingDeclaration() + `<LaunchBox><Game><DatabaseID>` + databaseID + `</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform>` + fields + `</Game></LaunchBox>`
}

func TestParseLaunchBoxXMLMemberValidatesIDsAndOptionalYearPlayerValues(t *testing.T) {
	for _, databaseID := range []string{"1", "9223372036854775807"} {
		t.Run("valid ID "+databaseID, func(t *testing.T) {
			batch := newTestLaunchBoxRecordBatch()
			if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember(databaseID, ``)), "Metadata.xml", nil, batch); err != nil {
				t.Fatalf("valid ID rejected: %v", err)
			}
		})
	}
	for _, databaseID := range []string{"", "0", "01", "-1", "abc", strings.Repeat("9", 20)} {
		t.Run("invalid ID "+databaseID, func(t *testing.T) {
			if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember(databaseID, ``)), "Metadata.xml", nil, newTestLaunchBoxRecordBatch()); err == nil {
				t.Fatal("invalid ID accepted")
			}
		})
	}

	for _, testCase := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "absent", want: ""},
		{name: "empty", value: `<ReleaseYear></ReleaseYear>`, want: ""},
		{name: "valid", value: `<ReleaseYear>1991</ReleaseYear>`, want: "1991"},
		{name: "malformed", value: `<ReleaseYear>19x1</ReleaseYear>`, want: ""},
		{name: "short", value: `<ReleaseYear>199</ReleaseYear>`, want: ""},
	} {
		t.Run("year "+testCase.name, func(t *testing.T) {
			batch := newTestLaunchBoxRecordBatch()
			if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember("1", testCase.value)), "Metadata.xml", nil, batch); err != nil {
				t.Fatalf("optional year rejected: %v", err)
			}
			if got := batch.observed[0].game.releaseYear; got != testCase.want {
				t.Fatalf("release year = %q, want %q", got, testCase.want)
			}
		})
	}
	if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember("1", `<ReleaseYear>19911</ReleaseYear>`)), "Metadata.xml", nil, newTestLaunchBoxRecordBatch()); err == nil {
		t.Fatal("overlong release year accepted")
	}

	for _, testCase := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "absent", want: ""},
		{name: "empty", value: `<MaxPlayers></MaxPlayers>`, want: ""},
		{name: "one", value: `<MaxPlayers>1</MaxPlayers>`, want: "1"},
		{name: "999", value: `<MaxPlayers>999</MaxPlayers>`, want: "999"},
		{name: "zero", value: `<MaxPlayers>0</MaxPlayers>`, want: ""},
		{name: "too large", value: `<MaxPlayers>1000</MaxPlayers>`, want: ""},
		{name: "malformed", value: `<MaxPlayers>abc</MaxPlayers>`, want: ""},
	} {
		t.Run("players "+testCase.name, func(t *testing.T) {
			batch := newTestLaunchBoxRecordBatch()
			if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember("1", testCase.value)), "Metadata.xml", nil, batch); err != nil {
				t.Fatalf("optional players rejected: %v", err)
			}
			if got := batch.observed[0].game.maxPlayers; got != testCase.want {
				t.Fatalf("max players = %q, want %q", got, testCase.want)
			}
		})
	}
	if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember("1", `<MaxPlayers>`+strings.Repeat("1", 33)+`</MaxPlayers>`)), "Metadata.xml", nil, newTestLaunchBoxRecordBatch()); err == nil {
		t.Fatal("overlong max players accepted")
	}
}

func TestParseLaunchBoxXMLMemberStreamsReleaseDateWithoutUsingIt(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
	}{
		{name: "absent"},
		{name: "empty", value: `<ReleaseDate></ReleaseDate>`},
		{name: "valid-looking", value: `<ReleaseDate>2000-01-01</ReleaseDate>`},
		{name: "timezone-bearing", value: `<ReleaseDate>2000-01-01T12:00:00-07:00</ReleaseDate>`},
		{name: "malformed", value: `<ReleaseDate>not-a-date</ReleaseDate>`},
		{name: "exact ordinary-text frame", value: `<ReleaseDate>` + strings.Repeat("x", launchBoxXMLMaxTextBytes) + `</ReleaseDate>`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			batch := newTestLaunchBoxRecordBatch()
			if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember("1", testCase.value)), "Metadata.xml", nil, batch); err != nil {
				t.Fatalf("ReleaseDate value rejected: %v", err)
			}
			if got := batch.observed[0].game.releaseYear; got != "" {
				t.Fatalf("ReleaseDate affected release year: %q", got)
			}
		})
	}
	if _, err := parseLaunchBoxXMLMember(strings.NewReader(launchBoxSingleGameMember("1", `<ReleaseDate>`+strings.Repeat("x", launchBoxXMLMaxTextBytes+1)+`</ReleaseDate>`)), "Metadata.xml", nil, newTestLaunchBoxRecordBatch()); err == nil {
		t.Fatal("overlong ReleaseDate ordinary-text frame accepted")
	}
}

func TestLaunchBoxMemberCountsEnforceFamilyAndMemberCaps(t *testing.T) {
	familyCases := []struct {
		name   string
		member string
		family string
		limit  int
		set    func(*launchBoxMemberCounts, int)
	}{
		{name: "Metadata Game", member: "Metadata.xml", family: "Game", limit: launchBoxXMLMaxGames, set: func(c *launchBoxMemberCounts, value int) { c.games = value }},
		{name: "Metadata aliases", member: "Metadata.xml", family: "GameAlternateName", limit: launchBoxXMLMaxAliases, set: func(c *launchBoxMemberCounts, value int) { c.aliases = value }},
		{name: "Metadata images", member: "Metadata.xml", family: "GameImage", limit: launchBoxXMLMaxImages, set: func(c *launchBoxMemberCounts, value int) { c.images = value }},
		{name: "Metadata platforms", member: "Metadata.xml", family: "Platform", limit: launchBoxXMLMaxPlatforms, set: func(c *launchBoxMemberCounts, value int) { c.platforms = value }},
		{name: "Metadata platform aliases", member: "Metadata.xml", family: "PlatformAlternateName", limit: launchBoxXMLMaxPlatformAliases, set: func(c *launchBoxMemberCounts, value int) { c.platformAliases = value }},
		{name: "Metadata emulators", member: "Metadata.xml", family: "Emulator", limit: launchBoxXMLMaxEmulators, set: func(c *launchBoxMemberCounts, value int) { c.emulators = value }},
		{name: "Metadata emulator platforms", member: "Metadata.xml", family: "EmulatorPlatform", limit: launchBoxXMLMaxEmulatorPlatforms, set: func(c *launchBoxMemberCounts, value int) { c.emulatorPlatforms = value }},
		{name: "Platforms platforms", member: "Platforms.xml", family: "Platform", limit: launchBoxXMLMaxPlatforms, set: func(c *launchBoxMemberCounts, value int) { c.platforms = value }},
		{name: "Platforms platform aliases", member: "Platforms.xml", family: "PlatformAlternateName", limit: launchBoxXMLMaxPlatformAliases, set: func(c *launchBoxMemberCounts, value int) { c.platformAliases = value }},
	}
	for _, testCase := range familyCases {
		t.Run(testCase.name, func(t *testing.T) {
			exact := launchBoxMemberCounts{}
			testCase.set(&exact, testCase.limit-1)
			if err := exact.allow(testCase.family, testCase.member); err != nil {
				t.Fatalf("inclusive family maximum rejected: %v", err)
			}
			tooMany := launchBoxMemberCounts{}
			testCase.set(&tooMany, testCase.limit)
			if err := tooMany.allow(testCase.family, testCase.member); err == nil {
				t.Fatal("family maximum plus one accepted")
			}
		})
	}

	for _, testCase := range []struct {
		name   string
		member string
		limit  int
		family string
	}{
		{name: "Metadata aggregate records", member: "Metadata.xml", limit: launchBoxXMLMaxMetadataRecords, family: "Emulator"},
		{name: "Platforms aggregate records", member: "Platforms.xml", limit: launchBoxXMLMaxPlatformRecords, family: "Platform"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			exact := launchBoxMemberCounts{records: testCase.limit - 1}
			if err := exact.allow(testCase.family, testCase.member); err != nil {
				t.Fatalf("inclusive member maximum rejected: %v", err)
			}
			tooMany := launchBoxMemberCounts{records: testCase.limit}
			if err := tooMany.allow(testCase.family, testCase.member); err == nil {
				t.Fatal("member maximum plus one accepted")
			}
		})
	}
}

func TestLaunchBoxBudgetReserveEnforcesAggregateStartMaximumThroughFraming(t *testing.T) {
	budget := &launchBoxXMLBudget{startElements: launchBoxXMLMaxAggregateStartElements - 1}
	input := launchBoxFramingDeclaration() + `<LaunchBox/>`
	if _, err := io.Copy(io.Discard, newFramedXMLReaderWithBudget(strings.NewReader(input), budget)); err != nil {
		t.Fatalf("last aggregate start element rejected: %v", err)
	}
	if budget.startElements != launchBoxXMLMaxAggregateStartElements {
		t.Fatalf("last aggregate start element was not counted: %d", budget.startElements)
	}

	budget = &launchBoxXMLBudget{startElements: launchBoxXMLMaxAggregateStartElements}
	if _, err := io.Copy(io.Discard, newFramedXMLReaderWithBudget(strings.NewReader(input), budget)); err == nil {
		t.Fatal("aggregate start maximum plus one accepted through framing")
	}
	if budget.startElements != launchBoxXMLMaxAggregateStartElements {
		t.Fatalf("failed aggregate start reserve mutated budget: %d", budget.startElements)
	}
}

func TestLaunchBoxXMLBudgetReserveIsOverflowSafeAndInclusive(t *testing.T) {
	budget := &launchBoxXMLBudget{}
	if err := budget.reserve(launchBoxXMLMaxAggregateStartElements, launchBoxXMLMaxAggregateAttributes); err != nil {
		t.Fatalf("inclusive aggregate maximum rejected: %v", err)
	}
	if err := budget.reserve(1, 0); err == nil {
		t.Fatal("aggregate start-element maximum plus one accepted")
	}
	if err := budget.reserve(0, 1); err == nil {
		t.Fatal("aggregate attribute maximum plus one accepted")
	}
	if budget.startElements != launchBoxXMLMaxAggregateStartElements || budget.attributes != launchBoxXMLMaxAggregateAttributes {
		t.Fatalf("failed reserve mutated budget: %+v", budget)
	}
	for name, budget := range map[string]*launchBoxXMLBudget{
		"start elements": {startElements: launchBoxXMLMaxAggregateStartElements + 1},
		"attributes":     {attributes: launchBoxXMLMaxAggregateAttributes + 1},
	} {
		t.Run(name, func(t *testing.T) {
			before := *budget
			if err := budget.reserve(1, 1); err == nil {
				t.Fatal("reserve accepted an already-over-limit budget")
			}
			if *budget != before {
				t.Fatalf("failed reserve mutated over-limit budget: before=%+v after=%+v", before, *budget)
			}
		})
	}
}

func TestFramedXMLReaderAcceptsEightSyntacticAttributesBeforeSchemaValidation(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<N a0="x" a1="x" a2="x" a3="x" a4="x" a5="x" a6="x" a7="x"/>` +
		`</LaunchBox>`
	budget := &launchBoxXMLBudget{}
	if _, err := io.Copy(io.Discard, newFramedXMLReaderWithBudget(strings.NewReader(input), budget)); err != nil {
		t.Fatalf("framer rejected syntactically bounded attributes: %v", err)
	}
	if budget.attributes != 8 {
		t.Fatalf("unexpected attribute count: %d", budget.attributes)
	}
}

func TestFramedXMLReaderRejectsNinthSyntacticAttributeBeforeEmission(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><N` +
		` attr0="x" attr1="x" attr2="x" attr3="x" attr4="x" attr5="x" attr6="x" attr7="x" attr8="x"/>` +
		`</LaunchBox>`
	if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(input))); err == nil {
		t.Fatal("framer accepted ninth syntactic attribute")
	}
}

func TestFramedXMLReaderRejectsEmptyElementBeyondMaximumDepth(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><A><B><C><D><E><F><G><H/></G></F></E></D></C></B></A></LaunchBox>`
	if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(input))); err == nil {
		t.Fatal("empty element beyond maximum depth was accepted")
	}
}

func TestFramedXMLReaderAcceptsMaximumDepth(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><A><B><C><D><E><F><G></G></F></E></D></C></B></A></LaunchBox>`
	if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(input))); err != nil {
		t.Fatalf("maximum depth was rejected: %v", err)
	}
}
