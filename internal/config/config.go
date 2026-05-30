package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr            string
	WhisperBackend      string
	WhisperBaseURL      string
	WhisperAPIKey       string
	WhisperModel        string
	WhisperTimeout      time.Duration
	WhisperLanguage     string
	WhisperFasterURL    string
	WhisperFasterModel  string
	WhisperLocalBin     string
	WhisperLocalModel   string
	WhisperLocalArgs    string
	WhisperLocalNoGPU   bool
	LLMBaseURL          string
	LLMAPIKey           string
	LLMModel            string
	LLMTimeout          time.Duration
	AutoSaveResults     bool
	OutputDir           string
	CheckpointDir       string
	ChunkSeconds        int
	ChunkParallelism    int
	TaskWorkers         int
	SummaryWorkers      int
	BilibiliUserAgent   string
	BilibiliTimeout     time.Duration
	BilibiliCookieCache string
	BilibiliCookieTTL   time.Duration
	NotionToken         string
	NotionVersion       string
	NotionParentPage    string
	ObsidianVaultDir    string
	ObsidianSubdir      string
	IMAOpenAPIClientID  string
	IMAOpenAPIAPIKey    string
	IMAOpenAPIFolderID  string
	APISecret           string
	MaxUploadMB         int
	MaxSSESubscribers   int
}

func LoadFromEnv() Config {
	return Config{
		HTTPAddr:            getEnv("HTTP_ADDR", ":18880"),
		WhisperBackend:      getEnv("WHISPER_BACKEND", "openai"),
		WhisperBaseURL:      trimTrailingSlash(getEnv("WHISPER_BASE_URL", "https://api.openai.com")),
		WhisperAPIKey:       os.Getenv("WHISPER_API_KEY"),
		WhisperModel:        getEnv("WHISPER_MODEL", "whisper-1"),
		WhisperTimeout:      getDurationEnv("WHISPER_TIMEOUT", 90*time.Second),
		WhisperLanguage:     os.Getenv("WHISPER_LANGUAGE"),
		WhisperFasterURL:    trimTrailingSlash(getEnv("WHISPER_FASTER_URL", "http://127.0.0.1:19000")),
		WhisperFasterModel:  getEnv("WHISPER_FASTER_MODEL", "turbo"),
		WhisperLocalBin:     getEnv("WHISPER_LOCAL_BIN", "whisper"),
		WhisperLocalModel:   resolveDefaultLocalModel(),
		WhisperLocalArgs:    getEnv("WHISPER_LOCAL_ARGS", "-m \"{{model}}\" -f \"{{input}}\" -otxt -of \"{{output}}\" -nt {{language_flag}}"),
		WhisperLocalNoGPU:   getBoolEnv("WHISPER_LOCAL_NO_GPU", false),
		LLMBaseURL:          trimTrailingSlash(getEnv("LLM_BASE_URL", "https://api.openai.com")),
		LLMAPIKey:           os.Getenv("LLM_API_KEY"),
		LLMModel:            getEnv("LLM_MODEL", "gpt-4o-mini"),
		LLMTimeout:          getDurationEnv("LLM_TIMEOUT", 60*time.Second),
		AutoSaveResults:     getBoolEnv("AUTO_SAVE_RESULTS", true),
		OutputDir:           getEnv("OUTPUT_DIR", "outputs"),
		CheckpointDir:       getEnv("CHECKPOINT_DIR", filepath.Join("outputs", "_checkpoints")),
		ChunkSeconds:        getIntEnv("CHUNK_SECONDS", 45),
		ChunkParallelism:    getIntEnv("CHUNK_PARALLELISM", 1),
		TaskWorkers:         getIntEnv("TASK_WORKERS", 1),
		SummaryWorkers:      getIntEnv("SUMMARY_WORKERS", 1),
		BilibiliUserAgent:   getEnv("BILIBILI_USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"),
		BilibiliTimeout:     getDurationEnv("BILIBILI_TIMEOUT", 2*time.Minute),
		BilibiliCookieCache: getEnv("BILIBILI_COOKIE_CACHE", filepath.Join("outputs", "_checkpoints", "bilibili_cookie.json")),
		BilibiliCookieTTL:   getDurationEnv("BILIBILI_COOKIE_TTL", 720*time.Hour),
		NotionToken:         os.Getenv("NOTION_TOKEN"),
		NotionVersion:       getEnv("NOTION_VERSION", "2022-06-28"),
		NotionParentPage:    os.Getenv("NOTION_PARENT_PAGE_ID"),
		ObsidianVaultDir:    os.Getenv("OBSIDIAN_VAULT_DIR"),
		ObsidianSubdir:      getEnv("OBSIDIAN_SUBDIR", "Whisper Imports"),
		IMAOpenAPIClientID:  os.Getenv("IMA_OPENAPI_CLIENTID"),
		IMAOpenAPIAPIKey:    os.Getenv("IMA_OPENAPI_APIKEY"),
		IMAOpenAPIFolderID:  os.Getenv("IMA_OPENAPI_FOLDER_ID"),
		APISecret:           os.Getenv("API_SECRET"),
		MaxUploadMB:         getIntEnv("MAX_UPLOAD_MB", 512),
		MaxSSESubscribers:   getIntEnv("MAX_SSE_SUBSCRIBERS", 64),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func trimTrailingSlash(value string) string {
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}

func resolveDefaultLocalModel() string {
	if value := os.Getenv("WHISPER_LOCAL_MODEL"); value != "" {
		return value
	}

	candidates := []string{
		filepath.Join("models", "ggml-tiny.bin"),
		filepath.Join("models", "ggml-base.bin"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join("models", "ggml-base.bin")
}
