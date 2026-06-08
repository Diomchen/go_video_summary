package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go_subtitle_whisper/internal/config"
	"go_subtitle_whisper/internal/service"
	"go_subtitle_whisper/internal/settings"
)

func TestRuntimeClientUsesActiveProviderAndPrompt(t *testing.T) {
	var sawAuthorization bool
	var sawCustomPrompt bool
	var sawModel bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		sawAuthorization = r.Header.Get("Authorization") == "Bearer secret"
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sawModel = body.Model == "model-a"
		for _, message := range body.Messages {
			if message.Role == "system" && strings.Contains(message.Content, "CUSTOM PROMPT") {
				sawCustomPrompt = true
			}
		}
		writeChatResponse(t, w, `<!-- metadata
{"title":"T","source_link":"https://example.com","up_name":"UP","domain":"科技","tags":["OpenAI"]}
-->
# T

body`)
	}))
	defer server.Close()

	store := settings.NewStore(filepath.Join(t.TempDir(), "runtime.json"), config.Config{})
	runtimeSettings := settings.RuntimeSettings{
		LLM: settings.LLMSettings{
			ActiveProviderID: "p1",
			Providers: []settings.Provider{{
				ID: "p1", Name: "Provider 1", BaseURL: server.URL, APIKey: "secret", Model: "model-a", Enabled: true,
			}},
		},
		Prompts: settings.PromptSettings{
			ActivePromptID: "prompt-1",
			Items: []settings.Prompt{{
				ID: "prompt-1", Name: "Custom", Kind: "prompt", Content: "CUSTOM PROMPT", Enabled: true,
			}},
		},
	}
	if err := store.Save(runtimeSettings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	client := NewRuntimeClient(store, 3*time.Second)
	summary, meta, err := client.Summarize(context.Background(), "transcript", service.SummaryOptions{})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if !sawAuthorization || !sawCustomPrompt || !sawModel {
		t.Fatalf("request did not use active runtime config: auth=%v prompt=%v model=%v", sawAuthorization, sawCustomPrompt, sawModel)
	}
	if strings.Contains(summary, "<!-- metadata") {
		t.Fatalf("metadata comment was not removed: %q", summary)
	}
	if meta.Title != "T" || meta.Domain != "科技" || len(meta.Tags) != 1 || meta.Tags[0] != "OpenAI" {
		t.Fatalf("metadata parsed incorrectly: %+v", meta)
	}
}

func writeChatResponse(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"content": content}},
		},
	}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
