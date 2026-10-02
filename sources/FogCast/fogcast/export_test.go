package fogcast

import "net/http"

// NewLibraryRouteTestService is an idle service for the combined-library
// route test. It does not browse and it does not dial.
func NewLibraryRouteTestService() *Service {
	return newService(Config{}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
}

// SetLibraryRouteFixture installs the inventory, the enrolled targets, and
// the HTTP client a library GET will see. It does not dial.
func (s *Service) SetLibraryRouteFixture(nodes []MeshNode, targets []TargetConfig, client *http.Client) {
	if s == nil {
		return
	}
	s.meshMu.Lock()
	s.meshNodes = append([]MeshNode(nil), nodes...)
	s.meshNodesRetained = false
	s.meshHTTP = client
	s.meshMu.Unlock()
	s.targetMu.Lock()
	s.targets = append([]TargetConfig(nil), targets...)
	s.targetMu.Unlock()
}
