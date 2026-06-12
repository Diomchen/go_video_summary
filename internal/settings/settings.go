package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go_subtitle_whisper/internal/config"
)

const maskedAPIKey = "********"

type RuntimeSettings struct {
	Obsidian ObsidianSettings `json:"obsidian"`
	LLM      LLMSettings      `json:"llm"`
	Prompts  PromptSettings   `json:"prompts"`
}

type ObsidianSettings struct {
	VaultDir            string  `json:"vaultDir"`
	DomainIndexFile     string  `json:"domainIndexFile"`
	TagIndexFile        string  `json:"tagIndexFile"`
	SimilarityThreshold float64 `json:"similarityThreshold"`
}

type LLMSettings struct {
	ActiveProviderID string     `json:"activeProviderID"`
	Providers        []Provider `json:"providers"`
}

type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey,omitempty"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type PromptSettings struct {
	ActivePromptID string   `json:"activePromptID"`
	Items          []Prompt `json:"items"`
}

type Prompt struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	SourcePath string `json:"sourcePath,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type Store struct {
	path string
	cfg  config.Config
	mu   sync.RWMutex
}

func NewStore(path string, cfg config.Config) *Store {
	return &Store{path: strings.TrimSpace(path), cfg: cfg}
}

func (s *Store) Load() (RuntimeSettings, error) {
	s.mu.RLock()
	path := s.path
	s.mu.RUnlock()

	if strings.TrimSpace(path) == "" {
		return normalizeSettings(seedFromConfig(s.cfg)), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return normalizeSettings(seedFromConfig(s.cfg)), nil
	}
	if err != nil {
		return RuntimeSettings{}, err
	}
	var settings RuntimeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return RuntimeSettings{}, err
	}
	return normalizeSettings(settings), nil
}

func (s *Store) Save(settings RuntimeSettings) error {
	settings = normalizeSettings(settings)
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.WriteFile(s.path, data, 0o644)
}

func (s *Store) Update(mut func(*RuntimeSettings)) (RuntimeSettings, error) {
	settings, err := s.Load()
	if err != nil {
		return RuntimeSettings{}, err
	}
	mut(&settings)
	if err := s.Save(settings); err != nil {
		return RuntimeSettings{}, err
	}
	return settings, nil
}

func (s RuntimeSettings) ActiveProvider() (Provider, bool) {
	for _, provider := range s.LLM.Providers {
		if provider.ID == s.LLM.ActiveProviderID && provider.Enabled {
			return provider, true
		}
	}
	for _, provider := range s.LLM.Providers {
		if provider.Enabled {
			return provider, true
		}
	}
	return Provider{}, false
}

func (s RuntimeSettings) ActivePrompt() (Prompt, bool) {
	for _, prompt := range s.Prompts.Items {
		if prompt.ID == s.Prompts.ActivePromptID && prompt.Enabled {
			return prompt, true
		}
	}
	for _, prompt := range s.Prompts.Items {
		if prompt.Enabled {
			return prompt, true
		}
	}
	return Prompt{}, false
}

func (s RuntimeSettings) PublicCopy() RuntimeSettings {
	out := s
	out.LLM.Providers = append([]Provider(nil), s.LLM.Providers...)
	for idx := range out.LLM.Providers {
		if strings.TrimSpace(out.LLM.Providers[idx].APIKey) != "" {
			out.LLM.Providers[idx].APIKey = maskedAPIKey
		}
	}
	out.Prompts.Items = append([]Prompt(nil), s.Prompts.Items...)
	return out
}

func normalizeSettings(settings RuntimeSettings) RuntimeSettings {
	if strings.TrimSpace(settings.Obsidian.DomainIndexFile) == "" {
		settings.Obsidian.DomainIndexFile = "领域索引.md"
	}
	if strings.TrimSpace(settings.Obsidian.TagIndexFile) == "" {
		settings.Obsidian.TagIndexFile = "标签索引.md"
	}
	if settings.Obsidian.SimilarityThreshold <= 0 {
		settings.Obsidian.SimilarityThreshold = 0.82
	}
	for idx := range settings.LLM.Providers {
		settings.LLM.Providers[idx].BaseURL = trimTrailingSlash(settings.LLM.Providers[idx].BaseURL)
		if settings.LLM.ActiveProviderID != "" && settings.LLM.Providers[idx].ID == settings.LLM.ActiveProviderID {
			settings.LLM.Providers[idx].Enabled = true
		}
	}
	if settings.LLM.ActiveProviderID == "" {
		for _, provider := range settings.LLM.Providers {
			if provider.Enabled {
				settings.LLM.ActiveProviderID = provider.ID
				break
			}
		}
	}
	if settings.Prompts.ActivePromptID == "" {
		for _, prompt := range settings.Prompts.Items {
			if prompt.Enabled {
				settings.Prompts.ActivePromptID = prompt.ID
				break
			}
		}
	}
	return settings
}

func seedFromConfig(cfg config.Config) RuntimeSettings {
	settings := RuntimeSettings{
		Obsidian: ObsidianSettings{
			VaultDir:            strings.TrimSpace(cfg.ObsidianVaultDir),
			DomainIndexFile:     "领域索引.md",
			TagIndexFile:        "标签索引.md",
			SimilarityThreshold: 0.82,
		},
	}
	if strings.TrimSpace(cfg.LLMModel) != "" || strings.TrimSpace(cfg.LLMBaseURL) != "" || strings.TrimSpace(cfg.LLMAPIKey) != "" {
		provider := Provider{
			ID:      "env-default",
			Name:    "Env Default",
			BaseURL: trimTrailingSlash(cfg.LLMBaseURL),
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
			Enabled: true,
		}
		settings.LLM.Providers = []Provider{provider}
		settings.LLM.ActiveProviderID = provider.ID
	}
	return settings
}

func trimTrailingSlash(value string) string {
	value = strings.TrimSpace(value)
	for strings.HasSuffix(value, "/") {
		value = strings.TrimSuffix(value, "/")
	}
	return value
}
