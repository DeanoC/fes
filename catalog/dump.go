package catalog

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/DeanoC/FogCast-POC/protocol"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const dumpKeySeparator = "\x1f"

var DefaultPreferredRegions = []string{"usa", "world", "europe", "japan"}

var dumpFold = cases.Fold()

var revisionPattern = regexp.MustCompile(`^rev(?:ision)?[\s._-]*([a-z0-9]+)$`)

type Dump struct {
	CanonicalTitle string
	Region         string
	Revision       string
	Flags          []string
}

func (d Dump) FlagString() string {
	if len(d.Flags) == 0 {
		return ""
	}
	return strings.Join(d.Flags, ",")
}

func (d Dump) HasPrerelease() bool {
	for _, flag := range d.Flags {
		switch flag {
		case "beta", "proto", "sample", "demo":
			return true
		}
	}
	return false
}

func (d Dump) HasHack() bool {
	for _, flag := range d.Flags {
		switch flag {
		case "hack", "unl":
			return true
		}
	}
	return false
}

func ParseDump(title string) Dump {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return Dump{CanonicalTitle: trimmed}
	}
	remaining := trimmed
	var region, revision string
	flagSet := map[string]struct{}{}
	for {
		tag, next, ok := popDumpTag(remaining)
		if !ok {
			break
		}
		remaining = next
		for _, token := range splitDumpTokens(tag) {
			if mapped := mapDumpRegion(token); mapped != "" {
				if region == "" {
					region = mapped
				}
				continue
			}
			if mapped := mapDumpRevision(token); mapped != "" {
				if revision == "" {
					revision = mapped
				}
				continue
			}
			if mapped := mapDumpFlag(token); mapped != "" {
				flagSet[mapped] = struct{}{}
			}
		}
	}
	canonical := strings.TrimSpace(remaining)
	if canonical == "" {
		canonical = trimmed
	}
	flags := make([]string, 0, len(flagSet))
	for flag := range flagSet {
		flags = append(flags, flag)
	}
	sort.Strings(flags)
	return Dump{CanonicalTitle: canonical, Region: region, Revision: revision, Flags: flags}
}

func GroupKey(system protocol.System, canonicalTitle string) string {
	folded := foldSearchText(canonicalTitle)
	if folded == "" {
		folded = foldSearchText(string(system))
	}
	return string(system) + dumpKeySeparator + folded
}

func DumpPenalty(flags string) int {
	dump := Dump{Flags: splitStoredFlags(flags)}
	if dump.HasPrerelease() {
		return 2
	}
	if dump.HasHack() {
		return 1
	}
	return 0
}

func PreferredDump(games []Game, regions []string) Game {
	if len(games) == 0 {
		return Game{}
	}
	best := games[0]
	for _, game := range games[1:] {
		if comparePreferredDump(game, best, regions) < 0 {
			best = game
		}
	}
	best.VariantCount = len(games)
	return best
}

func comparePreferredDump(a, b Game, regions []string) int {
	if rankA, rankB := RegionRank(a.Region, regions), RegionRank(b.Region, regions); rankA != rankB {
		return rankA - rankB
	}
	if penaltyA, penaltyB := DumpPenalty(a.DumpFlags), DumpPenalty(b.DumpFlags); penaltyA != penaltyB {
		return penaltyA - penaltyB
	}
	if a.Revision != b.Revision {
		return strings.Compare(b.Revision, a.Revision)
	}
	return strings.Compare(a.ID, b.ID)
}

func RegionRank(region string, preferred []string) int {
	if len(preferred) == 0 {
		preferred = DefaultPreferredRegions
	}
	normalized := mapDumpRegion(region)
	if normalized == "" {
		normalized = strings.TrimSpace(strings.ToLower(region))
	}
	for index, candidate := range preferred {
		if mapDumpRegion(candidate) == normalized || strings.EqualFold(strings.TrimSpace(candidate), normalized) {
			return index
		}
	}
	if normalized == "" {
		return 200
	}
	return 100
}

func popDumpTag(title string) (tag, remaining string, ok bool) {
	title = strings.TrimSpace(title)
	if len(title) < 3 {
		return "", title, false
	}
	close := title[len(title)-1]
	open := byte(0)
	switch close {
	case ')':
		open = '('
	case ']':
		open = '['
	default:
		return "", title, false
	}
	depth := 0
	openIndex := -1
	for index := len(title) - 1; index >= 0; index-- {
		switch title[index] {
		case close:
			depth++
		case open:
			depth--
			if depth == 0 {
				openIndex = index
				index = -1
			}
		}
	}
	if openIndex <= 0 {
		return "", title, false
	}
	inside := strings.TrimSpace(title[openIndex+1 : len(title)-1])
	if inside == "" {
		return "", title, false
	}
	return inside, strings.TrimSpace(title[:openIndex]), true
}

func splitDumpTokens(tag string) []string {
	parts := strings.FieldsFunc(tag, func(r rune) bool {
		return r == ',' || r == '/' || r == '+'
	})
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens = append(tokens, foldDumpToken(part))
	}
	if len(tokens) == 0 {
		return []string{foldDumpToken(tag)}
	}
	return tokens
}

func foldDumpToken(value string) string {
	text := dumpFold.String(norm.NFC.String(strings.TrimSpace(value)))
	var builder strings.Builder
	space := false
	for _, r := range text {
		if unicode.IsSpace(r) || r == '_' {
			space = builder.Len() != 0
			continue
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

func mapDumpRegion(token string) string {
	switch token {
	case "usa", "us", "u", "america", "ntsc u", "ntsc-u":
		return "usa"
	case "europe", "eu", "eur", "pal", "ntsc pal":
		return "europe"
	case "japan", "jp", "j", "ntsc j", "ntsc-j":
		return "japan"
	case "world", "w":
		return "world"
	case "brazil", "br":
		return "brazil"
	case "korea", "kr":
		return "korea"
	case "asia", "as":
		return "asia"
	case "australia", "au", "oceania":
		return "australia"
	case "france", "fr":
		return "france"
	case "germany", "de":
		return "germany"
	case "spain", "es":
		return "spain"
	case "italy", "it":
		return "italy"
	case "canada", "ca":
		return "canada"
	case "other":
		return "other"
	default:
		return ""
	}
}

func mapDumpRevision(token string) string {
	match := revisionPattern.FindStringSubmatch(token)
	if match == nil {
		return ""
	}
	return match[1]
}

func mapDumpFlag(token string) string {
	switch token {
	case "beta", "b":
		return "beta"
	case "proto", "prototype":
		return "proto"
	case "sample":
		return "sample"
	case "demo":
		return "demo"
	case "hack":
		return "hack"
	case "unl", "unlicensed", "pirate":
		return "unl"
	default:
		return ""
	}
}

func dumpSearchDocument(id, title, canonical, aliases string, system protocol.System) string {
	return strings.TrimSpace(strings.Join([]string{
		foldSearchText(title),
		foldSearchText(canonical),
		foldSearchText(id),
		foldSearchText(string(system)),
		foldSearchText(aliases),
	}, " "))
}

func dumpPenaltySQL(column string) string {
	wrapped := "',' || " + column + " || ','"
	return `CASE WHEN ` + wrapped + ` LIKE '%,beta,%' OR ` + wrapped + ` LIKE '%,proto,%' OR ` + wrapped + ` LIKE '%,sample,%' OR ` + wrapped + ` LIKE '%,demo,%' THEN 2 WHEN ` + wrapped + ` LIKE '%,hack,%' OR ` + wrapped + ` LIKE '%,unl,%' THEN 1 ELSE 0 END`
}

func regionCaseSQL(preferred []string, column string) (string, []any) {
	if len(preferred) == 0 {
		preferred = DefaultPreferredRegions
	}
	var builder strings.Builder
	args := make([]any, 0, len(preferred)+1)
	builder.WriteString("CASE")
	for index, region := range preferred {
		mapped := mapDumpRegion(region)
		if mapped == "" {
			mapped = foldDumpToken(region)
		}
		builder.WriteString(" WHEN ")
		builder.WriteString(column)
		builder.WriteString(" = ? THEN ?")
		args = append(args, mapped, index)
	}
	builder.WriteString(fmt.Sprintf(" WHEN %s = '' THEN 200 ELSE 100 END", column))
	return builder.String(), args
}
