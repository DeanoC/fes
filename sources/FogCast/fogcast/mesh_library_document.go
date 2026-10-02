package fogcast

import (
	"fmt"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// MeshLibraryDocument is the JSON body of GET /api/v1/library/titles.
// It is a view of ProjectMeshBackendLibrary, not a second inventory.
type MeshLibraryDocument struct {
	Titles []MeshLibraryTitle `json:"titles"`
}

// MeshLibraryTitle is one linked catalog title.
type MeshLibraryTitle struct {
	TitleID        string                     `json:"title_id"`
	System         string                     `json:"system"`
	ContentIDs     []string                   `json:"content_ids"`
	ContentSources []MeshLibraryContentSource `json:"content_sources"`
	Options        []MeshLibraryOption        `json:"options"`
}

// MeshLibraryContentSource is one slot id and the nodes that supply it.
// NodeIDs is empty until #396.
type MeshLibraryContentSource struct {
	ContentID string   `json:"content_id"`
	NodeIDs   []string `json:"node_ids"`
}

// MeshLibraryOption is one backend for a title. host_only is not a value
// of Execution; a host-local emulator option is native_emu with HostLocal.
type MeshLibraryOption struct {
	SourceGameID string              `json:"source_game_id"`
	Execution    string              `json:"execution"`
	HostLocal    bool                `json:"host_local"`
	Available    bool                `json:"available"`
	Reason       string              `json:"reason,omitempty"`
	CoreID       string              `json:"core_id,omitempty"`
	Package      *MeshLibraryPackage `json:"package,omitempty"`
	Nodes        []MeshLibraryNode   `json:"nodes"`
}

// MeshLibraryPackage is the described package id plus ABI id and major.
type MeshLibraryPackage struct {
	PackageID string `json:"package_id"`
	ABI       string `json:"abi"`
	Major     int    `json:"major"`
}

// MeshLibraryNode is one inventory candidate. Reason is omitted when
// the node is available.
type MeshLibraryNode struct {
	NodeID    string `json:"node_id"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// MeshLibraryTitles maps the projection to the library document. A content
// id that ParseContentID rejects fails the call. It is not omitted.
func MeshLibraryTitles(rows []MeshBackendRow) (MeshLibraryDocument, error) {
	doc := MeshLibraryDocument{Titles: make([]MeshLibraryTitle, 0, len(rows))}
	for _, row := range rows {
		if len(row.ContentSources) != len(row.ContentIDs) {
			return MeshLibraryDocument{}, fmt.Errorf("library title %s: content sources do not match content ids", row.TitleID)
		}
		title := MeshLibraryTitle{
			TitleID:        row.TitleID,
			System:         row.System,
			ContentIDs:     make([]string, 0, len(row.ContentIDs)),
			ContentSources: make([]MeshLibraryContentSource, 0, len(row.ContentSources)),
			Options:        make([]MeshLibraryOption, 0, len(row.Options)),
		}
		texts := make([]string, 0, len(row.ContentIDs))
		for _, id := range row.ContentIDs {
			parsed, err := meshcontent.ParseContentID(id.String())
			if err != nil || parsed != id {
				return MeshLibraryDocument{}, fmt.Errorf("library title %s: %s", row.TitleID, MeshSkipMalformedContentID)
			}
			texts = append(texts, parsed.String())
		}
		title.ContentIDs = texts
		for i, source := range row.ContentSources {
			parsed, err := meshcontent.ParseContentID(source.ContentID.String())
			if err != nil || parsed.String() != texts[i] {
				return MeshLibraryDocument{}, fmt.Errorf("library title %s: %s", row.TitleID, MeshSkipMalformedContentID)
			}
			nodeIDs := source.NodeIDs
			if nodeIDs == nil {
				nodeIDs = []string{}
			}
			title.ContentSources = append(title.ContentSources, MeshLibraryContentSource{
				ContentID: texts[i],
				NodeIDs:   nodeIDs,
			})
		}
		for _, option := range row.Options {
			if len(option.Entry.Execute) != 1 {
				return MeshLibraryDocument{}, fmt.Errorf("library title %s: option execution is missing", row.TitleID)
			}
			view := MeshLibraryOption{
				SourceGameID: option.Entry.TitleID,
				Execution:    option.Entry.Execute[0].Kind,
				HostLocal:    option.HostLocal,
				Available:    option.Available(),
				Reason:       option.Reason,
				Nodes:        make([]MeshLibraryNode, 0, len(option.Nodes)),
			}
			if pkg, ok := meshEntryPackage(option.Entry); ok {
				view.CoreID = option.CoreID
				view.Package = &MeshLibraryPackage{PackageID: pkg.PackageID, ABI: pkg.ABI, Major: pkg.Major}
			}
			for _, node := range option.Nodes {
				view.Nodes = append(view.Nodes, MeshLibraryNode{
					NodeID:    node.NodeID,
					Available: node.Available,
					Reason:    node.Reason,
				})
			}
			title.Options = append(title.Options, view)
		}
		doc.Titles = append(doc.Titles, title)
	}
	return doc, nil
}
