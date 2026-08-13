package metadata

import (
	"io"
	"strings"
	"testing"
)

func TestParseLaunchBoxXMLMemberRejectsDuplicateImageTuple(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		`<Game><DatabaseID>42</DatabaseID><Name>Title</Name><Platform>Super Nintendo Entertainment System</Platform></Game>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><Region>World</Region><CRC32>1</CRC32></GameImage>` +
		`<GameImage><DatabaseID>42</DatabaseID><FileName>cover_42.jpg</FileName><Type>Box - Front</Type><Region>World</Region><CRC32>1</CRC32></GameImage>` +
		`</LaunchBox>`
	_, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, &recordSliceSink{})
	if err == nil {
		t.Fatal("duplicate image tuple accepted")
	}
}

func TestFramedXMLReaderRejectsNonXMLSpaceLexicalAttributes(t *testing.T) {
	for _, input := range []string{
		`<?xml version="1.0" standalone="yes"?><LaunchBox><N space="preserve"/></LaunchBox>`,
		`<?xml version="1.0" standalone="yes"?><LaunchBox><N p:space="preserve"/></LaunchBox>`,
		`<?xml version="1.0" standalone="yes"?><LaunchBox><N xmlns:xml="http://www.w3.org/XML/1998/namespace"/></LaunchBox>`,
	} {
		if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(input))); err == nil {
			t.Fatalf("non-lexical XML-space attribute accepted: %q", input)
		}
	}
}

func TestFramedXMLReaderAcceptsEightSyntacticAttributesBeforeSchemaValidation(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox>` +
		strings.Repeat(`<AlternateName xml:space="preserve"/>`, 8) +
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
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><N a0="x" a1="x" a2="x" a3="x" a4="x" a5="x" a6="x" a7="x" a8="x"/></LaunchBox>`
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
