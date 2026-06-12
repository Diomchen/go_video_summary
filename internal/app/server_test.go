package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"go_subtitle_whisper/internal/config"
	"go_subtitle_whisper/internal/settings"
)

func TestSettingsRoutesLoadSaveAndTestProvider(t *testing.T) {
	var providerTested bool
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected LLM path: %s", r.URL.Path)
		}
		providerTested = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "OK"}},
			},
		})
	}))
	defer llmServer.Close()

	dir := t.TempDir()
	server, err := NewServer(config.Config{
		WhisperBackend:    "local",
		WhisperLocalBin:   "whisper",
		WhisperLocalModel: "model.bin",
		LLMBaseURL:        llmServer.URL,
		LLMAPIKey:         "secret",
		LLMModel:          "model-a",
		LLMTimeout:        0,
		RuntimeConfigPath: filepath.Join(dir, "runtime.json"),
		OutputDir:         filepath.Join(dir, "outputs"),
		CheckpointDir:     filepath.Join(dir, "checkpoints"),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	router := server.Routes()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got settings.RuntimeSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if got.LLM.Providers[0].APIKey != "********" {
		t.Fatalf("expected masked API key, got %q", got.LLM.Providers[0].APIKey)
	}

	got.Obsidian.VaultDir = filepath.Join(dir, "vault")
	got.LLM.Providers[0].APIKey = "********"
	body, _ := json.Marshal(got)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings status = %d body=%s", rec.Code, rec.Body.String())
	}

	reloaded, err := server.settingsStore.Load()
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	if reloaded.Obsidian.VaultDir != filepath.Join(dir, "vault") {
		t.Fatalf("vault dir was not saved: %+v", reloaded.Obsidian)
	}
	provider, ok := reloaded.ActiveProvider()
	if !ok || provider.APIKey != "secret" {
		t.Fatalf("masked API key should preserve existing secret: %+v ok=%v", provider, ok)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/settings/providers/env-default/test", strings.NewReader(`{}`))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test provider status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !providerTested {
		t.Fatalf("expected provider test request")
	}
}

func TestCollectionPreviewRejectsExplicitVideoPageWithoutSeasonLookup(t *testing.T) {
	dir := t.TempDir()
	server, err := NewServer(config.Config{
		WhisperBackend:    "local",
		WhisperLocalBin:   "whisper",
		WhisperLocalModel: "model.bin",
		RuntimeConfigPath: filepath.Join(dir, "runtime.json"),
		OutputDir:         filepath.Join(dir, "outputs"),
		CheckpointDir:     filepath.Join(dir, "checkpoints"),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	body := strings.NewReader(`{"url":"https://www.bilibili.com/video/BV114411Q7Y4/?p=20"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/collection-preview", body)
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/collection-preview status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if strings.Contains(got["error"], "season id") {
		t.Fatalf("explicit page URL should not be parsed as a collection: %q", got["error"])
	}
	if !strings.Contains(got["error"], "not a collection") {
		t.Fatalf("expected not-a-collection error, got %q", got["error"])
	}
}

func TestProviderActivationEnablesSelectedProvider(t *testing.T) {
	dir := t.TempDir()
	server, err := NewServer(config.Config{
		WhisperBackend:    "local",
		WhisperLocalBin:   "whisper",
		WhisperLocalModel: "model.bin",
		LLMBaseURL:        "https://default.example.com",
		LLMAPIKey:         "default-secret",
		LLMModel:          "default-model",
		RuntimeConfigPath: filepath.Join(dir, "runtime.json"),
		OutputDir:         filepath.Join(dir, "outputs"),
		CheckpointDir:     filepath.Join(dir, "checkpoints"),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	current, err := server.settingsStore.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	current.LLM.Providers = append(current.LLM.Providers, settings.Provider{
		ID:      "custom",
		Name:    "Custom",
		BaseURL: "https://custom.example.com",
		APIKey:  "custom-secret",
		Model:   "custom-model",
		Enabled: false,
	})
	if err := server.settingsStore.Save(current); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/settings/providers/custom/activate", nil)
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("activate provider status = %d body=%s", rec.Code, rec.Body.String())
	}
	reloaded, err := server.settingsStore.Load()
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	provider, ok := reloaded.ActiveProvider()
	if !ok {
		t.Fatalf("expected active provider")
	}
	if provider.ID != "custom" || !provider.Enabled {
		t.Fatalf("activation should enable and select custom provider, got %+v", provider)
	}
}
