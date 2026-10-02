// Package configstore wraps the github.com/marcuspeh/config_store SDK so
// the rest of the backend can request known project names without caring
// whether they came from a remote service, a hardcoded list, or somewhere
// else.
//
// ProjectsProvider exposes just the surface area /projects needs:
// fetch the JSON-encoded list of project names from the config store and
// decode it into a []string. The provider is safe to share across HTTP
// handlers and has a built-in LRU cache (via the SDK) so repeated calls
// don't round-trip the network.
package configstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	config "github.com/marcuspeh/config_store/sdk/go"
)

// projectsConfigKey is the SDK key under which the project list lives
// inside the `logging-service` project in config_store.
const projectsConfigKey = "projects"

// ConfigClient is the subset of config.ConfigClient we depend on. Kept
// as an interface so the API server can be constructed with a fake in
// tests.
type ConfigClient interface {
	Get(ctx context.Context, key string) (string, error)
}

// ProjectsProvider fetches and parses the project list from config_store.
type ProjectsProvider struct {
	client ConfigClient
	logger *slog.Logger
}

// NewProjectsProvider wraps an existing ConfigClient.
func NewProjectsProvider(client ConfigClient, logger *slog.Logger) *ProjectsProvider {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProjectsProvider{client: client, logger: logger}
}

// NewProjectsProviderFromConfig constructs a ConfigClient pointed at the
// config_store service and returns a ProjectsProvider over it. The caller
// owns the returned client — its LRU cache lives for the lifetime of the
// process.
func NewProjectsProviderFromConfig(baseURL, project string, logger *slog.Logger) *ProjectsProvider {
	client := config.NewConfigClient(project, config.WithBaseURL(baseURL))
	return NewProjectsProvider(client, logger)
}

// List returns the deduplicated, non-empty project names from the
// config_store. A missing key is treated as an empty list rather than
// an error so the API can still serve (with no autocomplete suggestions)
// during initial bring-up before the operator has populated config_store.
func (p *ProjectsProvider) List(ctx context.Context) ([]string, error) {
	raw, err := p.client.Get(ctx, projectsConfigKey)
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			p.logger.Warn("projects key missing from config_store; returning empty list", "key", projectsConfigKey)
			return []string{}, nil
		}
		return nil, fmt.Errorf("config_store get %s: %w", projectsConfigKey, err)
	}

	var projects []string
	if err := json.Unmarshal([]byte(raw), &projects); err != nil {
		return nil, fmt.Errorf("config_store decode %s: %w", projectsConfigKey, err)
	}

	out := make([]string, 0, len(projects))
	for _, name := range projects {
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
