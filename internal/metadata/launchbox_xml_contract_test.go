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
	table := &testLaunchBoxRecordTable{batch: newTestLaunchBoxRecordBatch()}
	if _, err := parseLaunchBoxXMLMembers(strings.NewReader(input), strings.NewReader(`<?xml version="1.0" standalone="yes"?><LaunchBox/>`), table); err == nil {
		t.Fatal("duplicate image tuple accepted")
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

func TestFramedXMLReaderRejectsMissingDeclarationPseudoAttributeSeparator(t *testing.T) {
	input := `<?xml version="1.0"encoding="utf-8"?><LaunchBox/>`
	if _, err := io.Copy(io.Discard, newFramedXMLReader(strings.NewReader(input))); err == nil {
		t.Fatal("declaration pseudo-attributes without XML whitespace were accepted")
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
