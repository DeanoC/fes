package expansion

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// oracleROMMapWord is the decode romMapWord replaced: a comma count, then
// json.Unmarshal into the fixed array.
func oracleROMMapWord(data []byte) ([40]uint32, error) {
	var word [40]uint32
	data = bytes.TrimSpace(data)
	if len(data) < 2 || data[0] != '[' || data[len(data)-1] != ']' || bytes.Count(data, []byte(",")) != 39 {
		return word, errors.New("ROM word must have 40 destinations")
	}
	return word, json.Unmarshal(data, &word)
}

func romMapWordCase(elements ...string) []byte {
	return []byte("[" + strings.Join(elements, ",") + "]")
}

func fortyOf(element string) []string {
	out := make([]string, 40)
	for i := range out {
		out[i] = element
	}
	return out
}

func romMapWordSeeds() [][]byte {
	valid := fortyOf("7")
	for i := range valid {
		valid[i] = strings.Repeat("1", 1+i%10)
	}
	seeds := [][]byte{
		romMapWordCase(fortyOf("0")...),
		romMapWordCase(fortyOf("4294967295")...),
		romMapWordCase(fortyOf("null")...),
		romMapWordCase(valid...),
		[]byte(" [ " + strings.Join(fortyOf("12"), " ,\n\t") + " \r] "),
		romMapWordCase(fortyOf("7")[:39]...),
		romMapWordCase(append(fortyOf("7"), "7")...),
		[]byte("[]"),
		[]byte("[7"),
		[]byte("7]"),
		[]byte("{}"),
		[]byte("null"),
	}
	for _, bad := range []string{"4294967296", "99999999999", "-1", "-0", "1.0", "1e2", "1E2", `"7"`, "true", "[7]", "{}", "01", ""} {
		elements := fortyOf("7")
		elements[17] = bad
		seeds = append(seeds, romMapWordCase(elements...))
	}
	return seeds
}

func checkROMMapWordMatchesOracle(t *testing.T, data []byte) {
	t.Helper()
	// The stream decoder only hands UnmarshalJSON syntactically valid JSON.
	if !json.Valid(data) {
		return
	}
	want, wantErr := oracleROMMapWord(data)
	var got romMapWord
	gotErr := got.UnmarshalJSON(data)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("%q: error %v, json.Unmarshal path %v", data, gotErr, wantErr)
	}
	if gotErr == nil && [40]uint32(got) != want {
		t.Fatalf("%q: decoded %v, json.Unmarshal path %v", data, got, want)
	}
}

func TestROMMapWordMatchesJSONUnmarshal(t *testing.T) {
	for _, seed := range romMapWordSeeds() {
		checkROMMapWordMatchesOracle(t, seed)
	}
}

func FuzzROMMapWordMatchesJSONUnmarshal(f *testing.F) {
	for _, seed := range romMapWordSeeds() {
		f.Add(seed)
	}
	f.Fuzz(checkROMMapWordMatchesOracle)
}

// legacyROMMapWord preserves the previous word scanner as an oracle for raw
// inputs too. The real stream decoder normally validates JSON syntax first.
func legacyROMMapWord(data []byte) ([40]uint32, error) {
	var word [40]uint32
	data = bytes.TrimSpace(data)
	if len(data) < 2 || data[0] != '[' || data[len(data)-1] != ']' {
		return word, errors.New("invalid word")
	}
	rest := data[1:]
	for i := range word {
		rest = bytes.TrimLeft(rest, " \t\r\n")
		end := bytes.IndexAny(rest, " \t\r\n,]")
		if end < 0 {
			return word, errors.New("invalid word")
		}
		value, err := romMapDestination(rest[:end])
		if err != nil {
			return word, err
		}
		word[i] = value
		rest = bytes.TrimLeft(rest[end:], " \t\r\n")
		want := byte(',')
		if i == len(word)-1 {
			want = ']'
		}
		if len(rest) == 0 || rest[0] != want {
			return word, errors.New("invalid word")
		}
		rest = rest[1:]
	}
	if len(rest) != 0 {
		return word, errors.New("invalid word")
	}
	return word, nil
}

func checkROMMapWordMatchesLegacy(t *testing.T, data []byte) {
	t.Helper()
	want, wantErr := legacyROMMapWord(data)
	var got romMapWord
	gotErr := got.UnmarshalJSON(data)
	if (gotErr == nil) != (wantErr == nil) || (gotErr == nil && [40]uint32(got) != want) {
		t.Fatalf("%q: got %v, %v; previous parser got %v, %v", data, got, gotErr, want, wantErr)
	}
}

func TestROMMapWordMatchesLegacy(t *testing.T) {
	for _, seed := range romMapWordSeeds() {
		checkROMMapWordMatchesLegacy(t, seed)
	}
}

func FuzzROMMapWordMatchesLegacy(f *testing.F) {
	for _, token := range []string{"0", "null", "4294967295", "4294967296", "01", "-1", "1e3", "", "\x00"} {
		f.Add(token, uint8(17))
	}
	f.Fuzz(func(t *testing.T, token string, index uint8) {
		words := fortyOf("7")
		words[int(index)%len(words)] = token
		checkROMMapWordMatchesLegacy(t, romMapWordCase(words...))
	})
}

func FuzzROMMapWordMatchesLegacyRaw(f *testing.F) {
	for _, seed := range romMapWordSeeds() {
		f.Add(seed)
	}
	f.Fuzz(checkROMMapWordMatchesLegacy)
}
