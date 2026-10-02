package api

import (
	"context"
	"sync"

	config "github.com/marcuspeh/config_store/sdk/go"
	"github.com/marcuspeh/logging-backend/internal/configstore"
)

// fakeConfigClient is an in-memory config_store stub used by API tests.
// It returns whatever (key, value) pairs were pre-loaded via Set, and
// returns the SDK's ErrNotFound for anything else.
type fakeConfigClient struct {
	mu   sync.RWMutex
	vals map[string]string
}

func newFakeConfigClient(vals map[string]string) *fakeConfigClient {
	return &fakeConfigClient{vals: vals}
}

func (f *fakeConfigClient) Get(_ context.Context, key string) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	v, ok := f.vals[key]
	if !ok {
		return "", config.ErrNotFound
	}
	return v, nil
}

// projectsFromJSON wires a ProjectsProvider over a fake config_store
// stub that returns the supplied JSON for the "projects" key.
func projectsFromJSON(projectsJSON string) *configstore.ProjectsProvider {
	vals := map[string]string{}
	if projectsJSON != "" {
		vals["projects"] = projectsJSON
	}
	return configstore.NewProjectsProvider(newFakeConfigClient(vals), nil)
}
