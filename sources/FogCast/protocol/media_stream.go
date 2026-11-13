package protocol

import "github.com/DeanoC/FogCast/protocol/internal/generated"

const MaxDeclaredMediaStreamBytes int64 = int64(generated.FesSimpleComputerMediaStreamGuaranteedMaxBytes)

func MediaStreamInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesSimpleComputerInterfaceMediaBlobStreamID, Major: generated.FesSimpleComputerInterfaceMediaBlobStreamMajor, Minor: generated.FesSimpleComputerInterfaceMediaBlobStreamMinor}
}

type MediaStreamCapability struct {
	Interface  RuntimeContract `json:"interface"`
	MinBytes   uint32          `json:"min_bytes"`
	MaxBytes   uint32          `json:"max_bytes"`
	ChunkBytes uint32          `json:"chunk_bytes"`
}

func (c *MediaStreamCapability) Valid() bool {
	return c != nil && c.Interface == MediaStreamInterface() &&
		c.MinBytes == generated.FesSimpleComputerMediaStreamMinBytes &&
		c.MaxBytes >= generated.FesSimpleComputerMediaStreamGuaranteedMaxBytes && c.MaxBytes <= generated.FesSimpleComputerMediaStreamMaxBytes &&
		c.ChunkBytes == generated.FesSimpleComputerMediaStreamChunkMaxBytes
}

func (b DevelopmentMediaBinding) AcceptsSize(s Status, size int64) bool {
	if !b.Matches(s) || size < 1 {
		return false
	}
	if !b.Stream {
		return size <= MaxDevelopmentMediaBytes
	}
	c := s.CorePackage.MediaStream
	return size <= MaxDeclaredMediaStreamBytes && size >= int64(c.MinBytes) && size <= int64(c.MaxBytes)
}

func MediaStreamCapable(p *CorePackageStatus) bool {
	if !DevelopmentMediaCapable(p) || !p.MediaStream.Valid() {
		return false
	}
	for _, i := range p.ActiveInterfaces {
		if (RuntimeContract{ID: i.ID, Major: i.Major, Minor: i.Minor}) == MediaStreamInterface() {
			return true
		}
	}
	return false
}
