package protocol

const MaxContentBytes int64 = 32 << 20

type ContentKey struct {
	SHA256    string
	Extension string
}

type ContentIdentity struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Extension string `json:"extension"`
}

func (c ContentIdentity) Key() ContentKey {
	return ContentKey{SHA256: c.SHA256, Extension: c.Extension}
}

type CacheProbeResponse struct {
	Present bool             `json:"present"`
	System  *System          `json:"system,omitempty"`
	Content *ContentIdentity `json:"content,omitempty"`
}

type CacheUploadResult string

const (
	CacheUploadPresent CacheUploadResult = "present"
	CacheUploadCreated CacheUploadResult = "created"
)

type CacheUploadResponse struct {
	Result  CacheUploadResult `json:"result"`
	System  System            `json:"system"`
	Content ContentIdentity   `json:"content"`
}

type CachedLaunchRequest struct {
	GameID  string          `json:"game_id"`
	System  System          `json:"system"`
	Content ContentIdentity `json:"content"`
}

type CachedLaunchResponse struct {
	Status  Status          `json:"status"`
	Content ContentIdentity `json:"content"`
}

// CachedIdentityResponse is a lease-free lookup of a previously launched
// verified cache entry. Absent responses omit identity fields.
type CachedIdentityResponse struct {
	Present bool             `json:"present"`
	GameID  string           `json:"game_id,omitempty"`
	System  *System          `json:"system,omitempty"`
	Content *ContentIdentity `json:"content,omitempty"`
}
