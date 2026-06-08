package settings

import (
	"path/filepath"
	"testing"
	"time"

	"go_subtitle_whisper/internal/config"
)

func TestStoreSeedsFromConfigWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "runtime.json"), config.Config{
		LLMBaseURL:       "https://api.example.com",
		LLMAPIKey:        "secret",
		LLMModel:         "gpt-test",
		LLMTimeout:       3 * time.Second,
		ObsidianVaultDir: filepath.Join(dir, "vault"),
	})

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	provider, ok := got.ActiveProvider()
	if !ok {
		t.Fatalf("expected active provider")
	}
	if provider.BaseURL != "https://api.example.com" || provider.APIKey != "secret" || provider.Model != "gpt-test" {
		t.Fatalf("provider seeded incorrectly: %+v", provider)
	}
	if got.Obsidian.VaultDir != filepath.Join(dir, "vault") {
		t.Fatalf("vault dir = %q", got.Obsidian.VaultDir)
	}
	if got.Obsidian.DomainIndexFile != "领域索引.md" || got.Obsidian.TagIndexFile != "标签索引.md" {
		t.Fatalf("unexpected index names: %+v", got.Obsidian)
	}
}

func TestStoreSavesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	store := NewStore(path, config.Config{})
	settings, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	settings.LLM.Providers = append(settings.LLM.Providers, Provider{
		ID:      "p1",
		Name:    "Provider 1",
		BaseURL: "https://p1.example.com",
		APIKey:  "k1",
		Model:   "m1",
		Enabled: true,
	})
	settings.LLM.ActiveProviderID = "p1"
	if err := store.Save(settings); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}
	provider, ok := reloaded.ActiveProvider()
	if !ok || provider.ID != "p1" || provider.APIKey != "k1" {
		t.Fatalf("active provider not reloaded: %+v ok=%v", provider, ok)
	}
}

func TestPublicCopyMasksAPIKeys(t *testing.T) {
	settings := RuntimeSettings{
		LLM: LLMSettings{
			Providers: []Provider{{ID: "p1", APIKey: "secret"}},
		},
	}
	public := settings.PublicCopy()
	if public.LLM.Providers[0].APIKey != "********" {
		t.Fatalf("API key was not masked: %q", public.LLM.Providers[0].APIKey)
	}
}
