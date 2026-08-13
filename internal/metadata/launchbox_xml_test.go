package metadata

import (
	"io"
	"strings"
	"testing"
)

func TestFramedXMLReaderRejectsOversizedOrdinaryTextBeforeEmission(t *testing.T) {
	input := `<?xml version="1.0" standalone="yes"?><LaunchBox><Game><Overview>` + strings.Repeat("x", launchBoxXMLMaxTextBytes+1) + `</Overview></Game></LaunchBox>`
	reader := newFramedXMLReader(strings.NewReader(input))
	if _, err := io.Copy(io.Discard, reader); err == nil {
		t.Fatal("oversized ordinary text frame was accepted")
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
	sink := &recordSliceSink{}
	counts, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, sink)
	if err != nil {
		t.Fatalf("parse member: %v", err)
	}
	if counts.games != 1 || counts.aliases != 1 || counts.images != 1 || len(sink.records) != 3 {
		t.Fatalf("unexpected counts=%+v records=%d", counts, len(sink.records))
	}
	if got := sink.records[0].game; got.databaseID != "42" || got.name != "  Super Game  " || got.platform == "" || got.releaseYear != "1991" {
		t.Fatalf("unexpected game record: %+v", got)
	}
	if sink.records[1].alias.alternateName != "Super  Game" {
		t.Fatalf("canonical alias was not retained: %+v", sink.records[1].alias)
	}
}

func TestParseLaunchBoxXMLMemberRejectsDTDAndEntityReferences(t *testing.T) {
	cases := []string{
		`<?xml version="1.0" standalone="yes"?><!DOCTYPE LaunchBox><LaunchBox/>`,
		`<?xml version="1.0" standalone="yes"?><LaunchBox><Game><Name>&custom;</Name></Game></LaunchBox>`,
	}
	for _, input := range cases {
		t.Run(input[:minIntLaunchBoxXML(len(input), 20)], func(t *testing.T) {
			_, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, &recordSliceSink{})
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
			_, err := parseLaunchBoxXMLMember(strings.NewReader(input), "Metadata.xml", nil, &recordSliceSink{})
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

type recordSliceSink struct{ records []launchBoxRecord }

func (s *recordSliceSink) putLaunchBoxRecord(record launchBoxRecord) error {
	s.records = append(s.records, record)
	return nil
}

func minIntLaunchBoxXML(left, right int) int {
	if left < right {
		return left
	}
	return right
}
