package protocol

import "regexp"

var mediaRecordCoreID = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)

// MediaRecordIdentity names one saved media record on a kit: the namespace a
// library launch of that game and immutable source disk binds. It is named
// without loading a package.
type MediaRecordIdentity struct {
	CoreID      string `json:"core_id"`
	GameID      string `json:"game_id"`
	BaseMediaID string `json:"base_media_id"`
	Unit        uint8  `json:"unit"`
}

func (i MediaRecordIdentity) Valid() bool {
	return mediaRecordCoreID.MatchString(i.CoreID) && ValidMediaGameID(i.GameID) && ValidateDigest(i.BaseMediaID) == nil && i.Unit == 0
}

// MediaRecordDiscard removes a record whose file still has ExpectedRevision.
type MediaRecordDiscard struct {
	MediaRecordIdentity
	ExpectedRevision string `json:"expected_revision"`
}

func (d MediaRecordDiscard) Valid() bool {
	return d.MediaRecordIdentity.Valid() && ValidateDigest(d.ExpectedRevision) == nil
}

// MaxMediaRecordBatch bounds one batched inspection.
const MaxMediaRecordBatch = 256

const (
	MediaRecordAbsent  = "absent"
	MediaRecordPresent = "present"
	MediaRecordCorrupt = "corrupt"
)

// MediaRecord is what a kit holds for a MediaRecordIdentity. Revision is the
// digest of the record file as it stands, or "absent". A corrupt record
// still has a revision, so it can be named and discarded.
type MediaRecord struct {
	State    string `json:"state"`
	Revision string `json:"revision"`
	Bytes    int64  `json:"bytes"`
}

func (r MediaRecord) Valid() bool {
	switch r.State {
	case MediaRecordAbsent:
		return r.Revision == "absent" && r.Bytes == 0
	case MediaRecordPresent, MediaRecordCorrupt:
		return ValidateDigest(r.Revision) == nil && r.Bytes > 0
	}
	return false
}
