package tenfoot

import "strings"

// OSKActionKind is one key result from the on-screen keyboard.
type OSKActionKind int

const (
	OSKNone OSKActionKind = iota
	OSKInsert
	OSKBackspace
	OSKClear
	OSKDone
	OSKPage
)

const (
	oskPageLetters = 0
	oskPageSymbols = 1
)

// OSKKey is one focusable cell on the keyboard.
type OSKKey struct {
	ID    string
	Label string
	Kind  OSKActionKind
	Text  string
	Span  int
	Focus bool
}

// OSK is a gamepad-navigable keyboard layout. It has no SDL dependency.
type OSK struct {
	page int
	row  int
	col  int
}

// TextField is a reusable buffer plus OSK for search and later rename fields.
type TextField struct {
	Buffer string
	OSK    OSK
}

// TextFieldResult is the outcome of activating the focused key.
type TextFieldResult struct {
	Done    bool
	Changed bool
}

// OSKSnapshot is the renderer-facing keyboard overlay.
type OSKSnapshot struct {
	Open    bool
	Buffer  string
	Page    int
	FocusID string
	Rows    [][]OSKKey
	Hint    string
	Prompt  string
}

func oskLayouts() [][][]OSKKey {
	return [][][]OSKKey{
		{
			oskChars("qwertyuiop"),
			oskChars("asdfghjkl"),
			oskChars("zxcvbnm"),
			oskActionRow("123"),
		},
		{
			oskChars("1234567890"),
			oskChars("-_.,'\""),
			oskChars("!?@#$%&"),
			oskActionRow("ABC"),
		},
	}
}

func oskChars(s string) []OSKKey {
	keys := make([]OSKKey, 0, len(s))
	for _, r := range s {
		text := string(r)
		keys = append(keys, OSKKey{
			ID:    "char-" + text,
			Label: strings.ToUpper(text),
			Kind:  OSKInsert,
			Text:  text,
			Span:  1,
		})
	}
	return keys
}

func oskActionRow(pageLabel string) []OSKKey {
	return []OSKKey{
		{ID: "page", Label: pageLabel, Kind: OSKPage, Span: 2},
		{ID: "space", Label: "space", Kind: OSKInsert, Text: " ", Span: 4},
		{ID: "bksp", Label: "bksp", Kind: OSKBackspace, Span: 1},
		{ID: "clear", Label: "clear", Kind: OSKClear, Span: 2},
		{ID: "done", Label: "done", Kind: OSKDone, Span: 1},
	}
}

func wrapIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

func (k *OSK) layout() [][]OSKKey {
	pages := oskLayouts()
	if k.page < 0 || k.page >= len(pages) {
		k.page = 0
	}
	return pages[k.page]
}

func (k *OSK) clamp() {
	rows := k.layout()
	if len(rows) == 0 {
		k.row, k.col = 0, 0
		return
	}
	k.row = wrapIndex(k.row, len(rows))
	row := rows[k.row]
	if len(row) == 0 {
		k.col = 0
		return
	}
	if k.col < 0 {
		k.col = 0
	}
	if k.col >= len(row) {
		k.col = len(row) - 1
	}
}

// Reset puts focus on the first letter of the letters page.
func (k *OSK) Reset() {
	k.page = 0
	k.row = 0
	k.col = 0
}

// Move shifts key focus. Horizontal wrap stays in the row; vertical wrap
// clamps the column to the destination row.
func (k *OSK) Move(dx, dy int) {
	if dy != 0 {
		rows := k.layout()
		k.row = wrapIndex(k.row+dy, len(rows))
		k.clamp()
	}
	if dx != 0 {
		k.clamp()
		row := k.layout()[k.row]
		k.col = wrapIndex(k.col+dx, len(row))
	}
}

// CyclePage switches letters ↔ symbols. Shoulders use this while search is open.
func (k *OSK) CyclePage(delta int) {
	k.page = wrapIndex(k.page+delta, len(oskLayouts()))
	k.clamp()
}

// SelectID focuses the key with id, switching page if needed.
func (k *OSK) SelectID(id string) bool {
	if id == "" {
		return false
	}
	for p, rows := range oskLayouts() {
		for r, row := range rows {
			for c, key := range row {
				if key.ID == id {
					k.page, k.row, k.col = p, r, c
					return true
				}
			}
		}
	}
	return false
}

// Focused returns the current key.
func (k *OSK) Focused() OSKKey {
	k.clamp()
	row := k.layout()[k.row]
	if len(row) == 0 {
		return OSKKey{}
	}
	return row[k.col]
}

func sanitizeFieldText(text string) string {
	text = strings.ReplaceAll(text, "\n", "")
	text = strings.ReplaceAll(text, "\r", "")
	return text
}

// Insert appends text into the buffer.
func (f *TextField) Insert(text string) {
	text = sanitizeFieldText(text)
	if text == "" {
		return
	}
	f.Buffer += text
}

// Backspace deletes the last rune.
func (f *TextField) Backspace() {
	if f.Buffer == "" {
		return
	}
	runes := []rune(f.Buffer)
	f.Buffer = string(runes[:len(runes)-1])
}

// Clear empties the buffer.
func (f *TextField) Clear() {
	f.Buffer = ""
}

// Move delegates d-pad motion to the keyboard.
func (f *TextField) Move(dx, dy int) {
	f.OSK.Move(dx, dy)
}

// CyclePage delegates charset paging to the keyboard.
func (f *TextField) CyclePage(delta int) {
	f.OSK.CyclePage(delta)
}

// Activate applies the focused key to the buffer.
func (f *TextField) Activate() TextFieldResult {
	key := f.OSK.Focused()
	switch key.Kind {
	case OSKInsert:
		before := f.Buffer
		f.Insert(key.Text)
		return TextFieldResult{Changed: f.Buffer != before}
	case OSKBackspace:
		before := f.Buffer
		f.Backspace()
		return TextFieldResult{Changed: f.Buffer != before}
	case OSKClear:
		if f.Buffer == "" {
			return TextFieldResult{}
		}
		f.Clear()
		return TextFieldResult{Changed: true}
	case OSKDone:
		return TextFieldResult{Done: true}
	case OSKPage:
		f.OSK.CyclePage(1)
		return TextFieldResult{}
	default:
		return TextFieldResult{}
	}
}

// Snapshot copies keys with the focused cell marked.
func (f *TextField) Snapshot() OSKSnapshot {
	rows := f.OSK.layout()
	f.OSK.clamp()
	out := make([][]OSKKey, len(rows))
	focusID := ""
	for i, row := range rows {
		out[i] = append([]OSKKey(nil), row...)
		if i == f.OSK.row && f.OSK.col >= 0 && f.OSK.col < len(out[i]) {
			out[i][f.OSK.col].Focus = true
			focusID = out[i][f.OSK.col].ID
		}
	}
	return OSKSnapshot{
		Buffer:  f.Buffer,
		Page:    f.OSK.page,
		FocusID: focusID,
		Rows:    out,
		Hint:    oskHint(f.OSK.page),
	}
}

func oskHint(page int) string {
	charset := "LB/RB symbols"
	if page == oskPageSymbols {
		charset = "LB/RB letters"
	}
	return "A type  B clear/close  " + charset + "  Y close  START quit"
}
