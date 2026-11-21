package protocol

// BundledFirmware is one firmware image the host ships beside FES as a
// separate, replaceable core-media object. The host selects it by default for
// a title of CoreID whose package names ROMID at exactly Size bytes; a title
// with its own selection keeps it. MediaID is the image's SHA-256 and its
// core-media identity.
type BundledFirmware struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	CoreID      string `json:"core_id"`
	ROMID       string `json:"rom_id"`
	Size        int64  `json:"size"`
	MediaID     string `json:"media_id"`
	Licence     string `json:"licence"`
	LicenceName string `json:"licence_name"`
	LicenceText string `json:"licence_text,omitempty"`
	Notice      string `json:"notice"`
	OriginURL   string `json:"origin_url"`
	// SourceURL and SourceSHA256 name the corresponding source code of a
	// free software image.
	SourceURL    string `json:"source_url,omitempty"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
}

// BundledLocation is a pinned download: an archive an image came from, or
// its corresponding source.
type BundledLocation struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// BundledDisk is one disk image the host ships beside FES for CoreID: the
// PC/XT's FreeDOS boot disk. The host starts that core from it while a title
// has no disk, and copies it for each new disk that starts FreeDOS. MediaID
// is the image's SHA-256 and its core-media identity.
type BundledDisk struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	CoreID      string            `json:"core_id"`
	Size        int64             `json:"size"`
	MediaID     string            `json:"media_id"`
	Licence     string            `json:"licence"`
	LicenceName string            `json:"licence_name"`
	LicenceText string            `json:"licence_text,omitempty"`
	Notice      string            `json:"notice"`
	Origins     []BundledLocation `json:"origins"`
	Sources     []BundledLocation `json:"sources,omitempty"`
}

// BundledFirmwareList is GET /api/v1/library/firmware/bundled. A host with
// no bundled firmware returns an empty list; an older host has no route, and
// a host older than bundled disks leaves Disks out.
type BundledFirmwareList struct {
	Firmware []BundledFirmware `json:"firmware"`
	Disks    []BundledDisk     `json:"disks,omitempty"`
}
