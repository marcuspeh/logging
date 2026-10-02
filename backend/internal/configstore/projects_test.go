package configstore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	config "github.com/marcuspeh/config_store/sdk/go"
)

// stubConfigClient returns the supplied raw value for one key and
// config.ErrNotFound for everything else. Mirrors the in-memory test
// helper used by the api package, but lives here so configstore has
// coverage of its own contract.
type stubConfigClient struct {
	values map[string]string
}

func (s *stubConfigClient) Get(_ context.Context, key string) (string, error) {
	v, ok := s.values[key]
	if !ok {
		return "", config.ErrNotFound
	}
	return v, nil
}

func TestProjectsProvider_List_UnmarshalsArray(t *testing.T) {
	client := &stubConfigClient{values: map[string]string{
		"projects": `["a","b"]`,
	}}
	p := NewProjectsProvider(client, nil)

	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestProjectsProvider_List_StripsEmpty(t *testing.T) {
	client := &stubConfigClient{values: map[string]string{
		"projects": `["a","","b","","c"]`,
	}}
	p := NewProjectsProvider(client, nil)

	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestProjectsProvider_List_EmptyArray(t *testing.T) {
	client := &stubConfigClient{values: map[string]string{
		"projects": `[]`,
	}}
	p := NewProjectsProvider(client, nil)

	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want []", got)
	}
}

func TestProjectsProvider_List_MissingKey(t *testing.T) {
	client := &stubConfigClient{values: map[string]string{}}
	p := NewProjectsProvider(client, nil)

	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want []", got)
	}
}

func TestProjectsProvider_List_InvalidJSON(t *testing.T) {
	client := &stubConfigClient{values: map[string]string{
		"projects": "not json",
	}}
	p := NewProjectsProvider(client, nil)

	_, err := p.List(context.Background())
	if err == nil {
		t.Fatal("List: expected error for invalid JSON, got nil")
	}
}

func TestProjectsProvider_List_NonArrayJSON(t *testing.T) {
	client := &stubConfigClient{values: map[string]string{
		"projects": `{"project": "a"}`,
	}}
	p := NewProjectsProvider(client, nil)

	_, err := p.List(context.Background())
	if err == nil {
		t.Fatal("List: expected error for non-array JSON, got nil")
	}
}

func TestProjectsProvider_List_PropagatesOtherErrors(t *testing.T) {
	failing := &failingConfigClient{err: errors.New("boom")}
	p := NewProjectsProvider(failing, nil)

	if _, err := p.List(context.Background()); err == nil {
		t.Fatal("List: expected error when client fails, got nil")
	}
}

type failingConfigClient struct{ err error }

func (f *failingConfigClient) Get(_ context.Context, _ string) (string, error) {
	return "", f.err
}