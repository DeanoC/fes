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

// CacheIndexEntry is one verified ROM cache object on the target FAT tree.
type CacheIndexEntry struct {
	System    System `json:"system"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Extension string `json:"extension"`
}

// CacheIndex is GET /v2/cache: ROM cache used/free against cache_max_bytes.
// It is lease-free, like a per-object probe. Cover files are not included.
type CacheIndex struct {
	UsedBytes int64             `json:"used_bytes"`
	MaxBytes  int64             `json:"max_bytes"`
	FreeBytes int64             `json:"free_bytes"`
	Entries   []CacheIndexEntry `json:"entries"`
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
