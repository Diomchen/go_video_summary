package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go_subtitle_whisper/internal/metadata"
	"go_subtitle_whisper/internal/service"
	"go_subtitle_whisper/internal/settings"
)

type RuntimeClient struct {
	store   *settings.Store
	timeout time.Duration
}

func NewRuntimeClient(store *settings.Store, timeout time.Duration) *RuntimeClient {
	return &RuntimeClient{store: store, timeout: timeout}
}

func (c *RuntimeClient) Translate(ctx context.Context, input, sourceLanguage string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	provider, err := c.activeProvider()
	if err != nil {
		return "", err
	}
	client := NewClient(provider.BaseURL, provider.APIKey, provider.Model, c.timeout)
	return client.Translate(ctx, input, sourceLanguage)
}

func (c *RuntimeClient) Summarize(ctx context.Context, transcript string, options service.SummaryOptions) (string, metadata.SummaryMetadata, error) {
	if strings.TrimSpace(transcript) == "" {
		return "", metadata.SummaryMetadata{}, nil
	}
	provider, err := c.activeProvider()
	if err != nil {
		return "", metadata.SummaryMetadata{}, err
	}
	settingsValue, err := c.store.Load()
	if err != nil {
		return "", metadata.SummaryMetadata{}, err
	}
	if prompt, ok := settingsValue.ActivePrompt(); ok && strings.TrimSpace(prompt.Content) != "" {
		options.PromptOverride = prompt.Content
	}
	client := NewClient(provider.BaseURL, provider.APIKey, provider.Model, c.timeout)
	return client.Summarize(ctx, transcript, options)
}

func (c *RuntimeClient) TestProvider(ctx context.Context, provider settings.Provider) error {
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if strings.TrimSpace(provider.BaseURL) == "" {
		return fmt.Errorf("provider baseURL is required")
	}
	if strings.TrimSpace(provider.Model) == "" {
		return fmt.Errorf("provider model is required")
	}
	client := NewClient(provider.BaseURL, provider.APIKey, provider.Model, c.timeout)
	_, err := client.chat(ctx, "You are a health check responder.", "Reply with OK.")
	return err
}

func (c *RuntimeClient) activeProvider() (settings.Provider, error) {
	if c == nil || c.store == nil {
		return settings.Provider{}, fmt.Errorf("runtime settings store is not configured")
	}
	settingsValue, err := c.store.Load()
	if err != nil {
		return settings.Provider{}, err
	}
	provider, ok := settingsValue.ActiveProvider()
	if !ok {
		return settings.Provider{}, fmt.Errorf("active LLM provider is not configured")
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return settings.Provider{}, fmt.Errorf("active LLM provider baseURL is empty")
	}
	if strings.TrimSpace(provider.Model) == "" {
		return settings.Provider{}, fmt.Errorf("active LLM provider model is empty")
	}
	return provider, nil
}
