package protocol

import (
	"strings"
	"testing"
)

// A corrupt record the kit could not read (empty, oversized, or not a
// regular file) reports zero bytes. It must still be valid, or the record
// could never be inspected, and so never discarded.
func TestMediaRecordValidity(t *testing.T) {
	rev := strings.Repeat("a", 64)
	for _, tc := range []struct {
		r    MediaRecord
		want bool
	}{
		{MediaRecord{State: MediaRecordAbsent, Revision: "absent"}, true},
		{MediaRecord{State: MediaRecordPresent, Revision: rev, Bytes: 10, Changed: true}, true},
		{MediaRecord{State: MediaRecordPresent, Revision: rev}, false},
		{MediaRecord{State: MediaRecordCorrupt, Revision: rev, Bytes: 10}, true},
		{MediaRecord{State: MediaRecordCorrupt, Revision: rev}, true},
		{MediaRecord{State: MediaRecordCorrupt, Revision: rev, Bytes: -1}, false},
		{MediaRecord{State: MediaRecordCorrupt, Revision: rev, Changed: true}, false},
		{MediaRecord{State: MediaRecordCorrupt, Revision: "absent"}, false},
	} {
		if got := tc.r.Valid(); got != tc.want {
			t.Errorf("%+v: valid %v, want %v", tc.r, got, tc.want)
		}
	}
}
