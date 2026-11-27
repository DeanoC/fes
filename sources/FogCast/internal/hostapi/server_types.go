package hostapi

import (
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type gameResult struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	System    protocol.System `json:"system"`
	CoreID    string          `json:"core_id,omitempty"`
	PackageID string          `json:"package_id,omitempty"`
	MediaID   string          `json:"media_id,omitempty"`
	MediaRole string          `json:"media_role,omitempty"`
	// MediaName and MoreMedia name disk 1 and list disks 2 to N of a disk set.
	MediaName          string              `json:"media_name,omitempty"`
	MoreMedia          []catalog.SetMedia  `json:"more_media,omitempty"`
	Kind               catalog.SourceKind  `json:"kind"`
	State              catalog.SourceState `json:"state"`
	RootOnline         bool                `json:"root_online"`
	ContentPrepared    bool                `json:"content_prepared"`
	Execution          string              `json:"execution"`
	Genre              string              `json:"genre,omitempty"`
	Year               string              `json:"year,omitempty"`
	Platform           protocol.System     `json:"platform,omitempty"`
	Favorite           bool                `json:"favorite,omitempty"`
	PlayCount          int64               `json:"play_count,omitempty"`
	LastPlayedAt       int64               `json:"last_played_at,omitempty"`
	Collections        []string            `json:"collections,omitempty"`
	Cover              string              `json:"cover,omitempty"`
	Launchable         bool                `json:"launchable"`
	ROMCached          *bool               `json:"rom_cached,omitempty"`
	CanonicalTitle     string              `json:"canonical_title,omitempty"`
	Region             string              `json:"region,omitempty"`
	Revision           string              `json:"revision,omitempty"`
	DumpFlags          string              `json:"dump_flags,omitempty"`
	GroupKey           string              `json:"group_key,omitempty"`
	VariantCount       int                 `json:"variant_count,omitempty"`
	Variants           []gameResult        `json:"variants,omitempty"`
	FirmwareRequired   bool                `json:"firmware_required,omitempty"`
	PackageUnavailable bool                `json:"package_unavailable,omitempty"`
	ExpansionID        string              `json:"expansion_id,omitempty"`
	ROMRequired        bool                `json:"rom_required,omitempty"`
	ROMReady           bool                `json:"rom_ready,omitempty"`
	VideoRequired      bool                `json:"video_required,omitempty"`
	VideoReady         bool                `json:"video_ready,omitempty"`
	ROMID              string              `json:"rom_id,omitempty"`
	ROMMediaID         string              `json:"rom_media_id,omitempty"`
	ROMRole            string              `json:"rom_role,omitempty"`
	ROMSourceSize      int64               `json:"rom_source_size,omitempty"`
	ExpansionReady     bool                `json:"expansion_ready,omitempty"`
	FirmwareReady      bool                `json:"firmware_ready,omitempty"`
	// SlotExpansions lists a multi-socket title's selected cards and readiness.
	SlotExpansions []protocol.SlotExpansionStatus `json:"slot_expansions,omitempty"`
	// ReadyHere is set only when a mesh execute session is installed.
	// Nil omits the field and leaves Phase 0 composition Ready.
	ReadyHere  *bool  `json:"ready_here,omitempty"`
	ReadyBlock string `json:"ready_block,omitempty"`
	NextAction string `json:"next_action,omitempty"`
	// Placement is the host predicate rooms read when this row asked
	// Place. Empty omits the field. Codes are selected, unresolved,
	// and fail_closed. They are not sofa copy.
	Placement string `json:"placement,omitempty"`
}

type gamesResult struct {
	Games         []gameResult `json:"games"`
	NextCursor    string       `json:"next_cursor,omitempty"`
	CoreEntryKind string       `json:"core_entry_kind,omitempty"`
	// Notice is plain shelf copy for an unfiltered local library.
	// Empty omits the field. It is not a shared mesh wire field.
	Notice string `json:"notice,omitempty"`
}

type collectionResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

type editionPreferenceResult struct {
	Query    string `json:"query"`
	Platform string `json:"platform,omitempty"`
	GameID   string `json:"game_id"`
	ChosenAt int64  `json:"chosen_at,omitempty"`
}

type healthResult struct {
	Ready  bool           `json:"ready"`
	Host   hostIdentity   `json:"host"`
	Target targetHealth   `json:"target"`
	Mesh   map[string]int `json:"mesh"`
}

type hostIdentity struct {
	Version  string `json:"version"`
	Revision string `json:"revision,omitempty"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

type statusResult struct {
	Connection *fogcast.TargetConnection `json:"connection,omitempty"`
	State      protocol.State            `json:"state"`
	GameID     *string                   `json:"game_id,omitempty"`
	System     *protocol.System          `json:"system,omitempty"`
	Core       *string                   `json:"core,omitempty"`
	Error      *apiError                 `json:"error,omitempty"`
}

type targetHealth struct {
	Connection *fogcast.TargetConnection `json:"connection,omitempty"`
	Reachable  bool                      `json:"reachable"`
	Ready      bool                      `json:"ready"`
	APIVersion string                    `json:"api_version,omitempty"`
	Artifacts  *protocol.Artifacts       `json:"artifacts,omitempty"`
	Software   *protocol.Identity        `json:"software,omitempty"`
}

type errorResult struct {
	Error apiError `json:"error"`
}

type presentationResult struct {
	GameID       string                   `json:"game_id"`
	State        string                   `json:"state"`
	Presentation *presentationPayload     `json:"presentation,omitempty"`
	Attribution  *presentationAttribution `json:"attribution,omitempty"`
}

type presentationPayload struct {
	Summary               string   `json:"summary"`
	Year                  string   `json:"year"`
	Genre                 string   `json:"genre"`
	Studio                string   `json:"studio"`
	Players               string   `json:"players"`
	Series                string   `json:"series,omitempty"`
	CoverArtworkHandle    string   `json:"cover_artwork_id,omitempty"`
	BackdropArtworkHandle string   `json:"backdrop_artwork_id,omitempty"`
	LogoHandle            string   `json:"logo_id,omitempty"`
	MarqueeHandle         string   `json:"marquee_id,omitempty"`
	Box3DHandle           string   `json:"box3d_id,omitempty"`
	VideoHandle           string   `json:"video_id,omitempty"`
	ScreenshotHandles     []string `json:"screenshot_ids,omitempty"`
}

type presentationAttribution struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

type apiError struct {
	StopStage string `json:"stop_stage,omitempty"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Phase     string `json:"phase,omitempty"`
	NoChange  bool   `json:"no_change,omitempty"`
	Expected  string `json:"expected,omitempty"`
	Observed  string `json:"observed,omitempty"`
}
