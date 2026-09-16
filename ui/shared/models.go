package shared

import "github.com/DeanoC/FogCast/hostclient"

// These aliases keep the neutral UI helpers tied to the public host contract.
// The hostclient package owns the wire models; shared only adds presentation
// and interaction behavior around them.
type Game = hostclient.Game
type Presentation = hostclient.Presentation
type PresentationInfo = hostclient.PresentationInfo
type PresentationAttribution = hostclient.PresentationAttribution
type Platform = hostclient.Platform
type GameListQuery = hostclient.GameListQuery
type FacetValues = hostclient.FacetValues
type AttractItem = hostclient.AttractItem
type AttractPlaylist = hostclient.AttractPlaylist
type LibraryCache = hostclient.LibraryCache
type CoreAvailability = hostclient.CoreAvailability
