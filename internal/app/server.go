package app

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go_subtitle_whisper/internal/config"
	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/exporter"
	"go_subtitle_whisper/internal/llm"
	"go_subtitle_whisper/internal/pipeline"
	"go_subtitle_whisper/internal/service"
	"go_subtitle_whisper/internal/settings"
	"go_subtitle_whisper/internal/source"
	"go_subtitle_whisper/internal/transcribe"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	cfg           config.Config
	manager       *pipeline.Manager
	events        *pipeline.Broadcaster
	settingsStore *settings.Store
	runtimeLLM    *llm.RuntimeClient
}

type urlTaskRequest struct {
	Name              string   `json:"name"`
	URLs              []string `json:"urls"`
	URLsText          string   `json:"urlsText"`
	Language          string   `json:"language"`
	Translate         bool     `json:"translate"`
	Summarize         bool     `json:"summarize"`
	ExportTargets     []string `json:"exportTargets"`
	MarkdownExportDir string   `json:"markdownExportDir"`
	ObsidianExportDir string   `json:"obsidianExportDir"`
}

type exportRetryRequest struct {
	Targets []string `json:"targets"`
}

type runtimeObsidianExporter struct {
	store *settings.Store
}

func (e runtimeObsidianExporter) Name() string { return "obsidian" }

func (e runtimeObsidianExporter) ExportMarkdown(ctx context.Context, task *domain.Task, markdownPath string, markdown string) (domain.ExportResult, error) {
	current, err := e.store.Load()
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	item := exporter.NewObsidianExporterWithIndex(
		current.Obsidian.VaultDir,
		"",
		current.Obsidian.DomainIndexFile,
		current.Obsidian.TagIndexFile,
		current.Obsidian.SimilarityThreshold,
	)
	if item == nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, errors.New("obsidian vault dir is not configured")
	}
	return item.ExportMarkdown(ctx, task, markdownPath, markdown)
}

func NewServer(cfg config.Config) (*Server, error) {
	events := pipeline.NewBroadcaster()

	var transcriber service.Transcriber
	switch strings.ToLower(strings.TrimSpace(cfg.WhisperBackend)) {
	case "", "openai":
		if cfg.WhisperModel == "" {
			return nil, errors.New("WHISPER_MODEL is required when WHISPER_BACKEND=openai")
		}
		transcriber = transcribe.NewOpenAITranscriber(cfg.WhisperBaseURL, cfg.WhisperAPIKey, cfg.WhisperModel, cfg.WhisperTimeout)
	case "faster-whisper":
		if cfg.WhisperFasterURL == "" {
			return nil, errors.New("WHISPER_FASTER_URL is required when WHISPER_BACKEND=faster-whisper")
		}
		if cfg.WhisperFasterModel == "" {
			return nil, errors.New("WHISPER_FASTER_MODEL is required when WHISPER_BACKEND=faster-whisper")
		}
		transcriber = transcribe.NewOpenAITranscriber(cfg.WhisperFasterURL, "", cfg.WhisperFasterModel, cfg.WhisperTimeout)
	case "local":
		transcriber = transcribe.NewLocalWhisperTranscriber(cfg.WhisperLocalBin, cfg.WhisperLocalModel, cfg.WhisperLocalArgs, cfg.WhisperLocalNoGPU)
	default:
		return nil, fmt.Errorf("unsupported WHISPER_BACKEND: %s", cfg.WhisperBackend)
	}

	settingsStore := settings.NewStore(cfg.RuntimeConfigPath, cfg)
	if _, err := settingsStore.Load(); err != nil {
		return nil, err
	}
	runtimeLLM := llm.NewRuntimeClient(settingsStore, cfg.LLMTimeout)
	var translator service.Translator = runtimeLLM
	var summarizer service.Summarizer = runtimeLLM

	bilibiliClient := source.NewBilibiliClient(cfg.BilibiliUserAgent, cfg.BilibiliTimeout)
	bilibiliClient.UseCookieCache(cfg.BilibiliCookieCache, cfg.BilibiliCookieTTL)
	var exportersList []exporter.MarkdownExporter
	if item := exporter.NewNotionExporter(cfg.NotionToken, cfg.NotionVersion, cfg.NotionParentPage); item != nil {
		exportersList = append(exportersList, item)
	}
	exportersList = append(exportersList, runtimeObsidianExporter{store: settingsStore})
	if item := exporter.NewIMAExporter(cfg.IMAOpenAPIClientID, cfg.IMAOpenAPIAPIKey, cfg.IMAOpenAPIFolderID); item != nil {
		exportersList = append(exportersList, item)
	}

	manager := pipeline.NewManager(transcriber, translator, summarizer, bilibiliClient, exportersList, events, cfg.AutoSaveResults, cfg.OutputDir, cfg.CheckpointDir, cfg.ChunkSeconds, cfg.ChunkParallelism, cfg.TaskWorkers, cfg.SummaryWorkers)
	return &Server{cfg: cfg, manager: manager, events: events, settingsStore: settingsStore, runtimeLLM: runtimeLLM}, nil
}

func (s *Server) Routes() http.Handler {
	staticFS, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/tasks/", s.handleTaskByID)
	mux.HandleFunc("/callback/tasks/", s.handleTaskStatusCallback)
	mux.HandleFunc("/api/url-tasks", s.handleURLTasks)
	mux.HandleFunc("/api/collection-preview", s.handleCollectionPreview)
	mux.HandleFunc("/api/bilibili/login/status", s.handleBilibiliLoginStatus)
	mux.HandleFunc("/api/bilibili/login/qrcode", s.handleBilibiliLoginQRCode)
	mux.HandleFunc("/api/bilibili/login/poll", s.handleBilibiliLoginPoll)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/settings/providers", s.handleSettingsProviders)
	mux.HandleFunc("/api/settings/providers/", s.handleSettingsProviderByID)
	mux.HandleFunc("/api/settings/prompts", s.handleSettingsPrompts)
	mux.HandleFunc("/api/settings/prompts/", s.handleSettingsPromptByID)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	return loggingMiddleware(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"http":               s.cfg.HTTPAddr,
		"whisperBackend":     s.cfg.WhisperBackend,
		"whisper":            s.cfg.WhisperBaseURL,
		"whisperFasterURL":   s.cfg.WhisperFasterURL,
		"whisperFasterModel": s.cfg.WhisperFasterModel,
		"whisperLocalBin":    s.cfg.WhisperLocalBin,
		"whisperLocalModel":  s.cfg.WhisperLocalModel,
		"whisperLocalNoGPU":  s.cfg.WhisperLocalNoGPU,
		"llm":                s.cfg.LLMBaseURL,
		"autoSaveResults":    s.cfg.AutoSaveResults,
		"outputDir":          s.cfg.OutputDir,
		"checkpointDir":      s.cfg.CheckpointDir,
		"chunkSeconds":       s.cfg.ChunkSeconds,
		"chunkParallelism":   s.cfg.ChunkParallelism,
		"taskWorkers":        s.cfg.TaskWorkers,
		"summaryWorkers":     s.cfg.SummaryWorkers,
		"bilibiliLoggedIn":   s.manager.BilibiliLoginStatus()["loggedIn"],
		"notionEnabled":      strings.TrimSpace(s.cfg.NotionToken) != "" && strings.TrimSpace(s.cfg.NotionParentPage) != "",
		"obsidianEnabled":    strings.TrimSpace(s.cfg.ObsidianVaultDir) != "",
		"imaEnabled":         strings.TrimSpace(s.cfg.IMAOpenAPIClientID) != "" && strings.TrimSpace(s.cfg.IMAOpenAPIAPIKey) != "",
		"exportPlatforms": map[string]bool{
			"notion":   strings.TrimSpace(s.cfg.NotionToken) != "" && strings.TrimSpace(s.cfg.NotionParentPage) != "",
			"markdown": true,
			"obsidian": true,
			"ima":      strings.TrimSpace(s.cfg.IMAOpenAPIClientID) != "" && strings.TrimSpace(s.cfg.IMAOpenAPIAPIKey) != "",
		},
	})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		writeJSON(w, http.StatusOK, s.manager.ListTasksPaged(page, size))
	case http.MethodPost:
		if err := r.ParseMultipartForm(1024 << 20); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		files := r.MultipartForm.File["file"]
		if len(files) == 0 {
			writeError(w, http.StatusBadRequest, errors.New("at least one file is required"))
			return
		}

		language := firstNonEmpty(r.FormValue("language"), s.cfg.WhisperLanguage)
		translate := parseBool(r.FormValue("translate"))
		summarize := parseBool(r.FormValue("summarize"))
		namePrefix := strings.TrimSpace(r.FormValue("name"))
		exportTargets := normalizeTargets(r.MultipartForm.Value["exportTarget"])
		exportOptions := pipeline.ExportOptions{
			Targets:           exportTargets,
			MarkdownExportDir: strings.TrimSpace(r.FormValue("markdownExportDir")),
			ObsidianExportDir: strings.TrimSpace(r.FormValue("obsidianExportDir")),
		}

		tasks := make([]any, 0, len(files))
		for _, header := range files {
			file, err := header.Open()
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			data, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}

			name := header.Filename
			if namePrefix != "" {
				name = fmt.Sprintf("%s - %s", namePrefix, header.Filename)
			}
			task := s.manager.CreateFileTask(name, header.Filename, data, language, translate, summarize, exportOptions)
			tasks = append(tasks, pipeline.ToTaskSummary(task))
		}

		writeJSON(w, http.StatusAccepted, tasks)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleURLTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req urlTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	urls := make([]string, 0, len(req.URLs))
	for _, value := range req.URLs {
		extracted := source.ExtractBilibiliInputs(value)
		if len(extracted) > 0 {
			urls = append(urls, extracted...)
			continue
		}
		if strings.TrimSpace(value) != "" {
			urls = append(urls, strings.TrimSpace(value))
		}
	}
	if strings.TrimSpace(req.URLsText) != "" {
		extracted := source.ExtractBilibiliInputs(req.URLsText)
		if len(extracted) > 0 {
			urls = append(urls, extracted...)
		} else {
			urls = append(urls, splitLines(req.URLsText)...)
		}
	}
	if len(urls) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one bilibili url is required"))
		return
	}

	language := firstNonEmpty(req.Language, s.cfg.WhisperLanguage)
	exportOptions := pipeline.ExportOptions{
		Targets:           normalizeTargets(req.ExportTargets),
		MarkdownExportDir: strings.TrimSpace(req.MarkdownExportDir),
		ObsidianExportDir: strings.TrimSpace(req.ObsidianExportDir),
	}
	tasks := make([]any, 0, len(urls))
	for _, rawURL := range urls {
		created, err := s.manager.CreateURLTasksFromInput(req.Name, rawURL, language, req.Translate, req.Summarize, exportOptions)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		for _, task := range created {
			tasks = append(tasks, pipeline.ToTaskSummary(task))
		}
	}
	writeJSON(w, http.StatusAccepted, tasks)
}

func (s *Server) handleCollectionPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, errors.New("url is required"))
		return
	}

	collection, err := s.manager.CollectionPreview(r.Context(), req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (s *Server) handleBilibiliLoginStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.BilibiliLoginStatus())
}

func (s *Server) handleBilibiliLoginQRCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	login, err := s.manager.StartBilibiliLogin(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, login)
}

func (s *Server) handleBilibiliLoginPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		QRCodeKey string `json:"qrcodeKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.manager.PollBilibiliLogin(r.Context(), req.QRCodeKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if strings.HasSuffix(id, "/generate-summary") {
		id = strings.TrimSuffix(id, "/generate-summary")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		task, err := s.manager.GenerateSummary(id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusAccepted, task)
		return
	}
	if strings.HasSuffix(id, "/retry-summary") {
		id = strings.TrimSuffix(id, "/retry-summary")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		task, err := s.manager.RetrySummary(id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusAccepted, task)
		return
	}
	if strings.HasSuffix(id, "/retry-exports") {
		id = strings.TrimSuffix(id, "/retry-exports")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req exportRetryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		task, err := s.manager.RetryExports(id, normalizeTargets(req.Targets))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusAccepted, task)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	task, ok := s.manager.GetTask(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("task %s not found", id))
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.events.Subscribe()
	defer s.events.Unsubscribe(ch)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleTaskStatusCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/callback/tasks/")
	id = strings.TrimSpace(strings.Trim(id, "/"))
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("task id is required"))
		return
	}

	status, ok := s.manager.GetTaskStatusCallback(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("task %s not found", id))
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		current, err := s.settingsStore.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, current.PublicCopy())
	case http.MethodPut:
		var req settings.RuntimeSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		current, err := s.settingsStore.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		req = mergeMaskedSecrets(current, req)
		if err := s.settingsStore.Save(req); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		saved, err := s.settingsStore.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, saved.PublicCopy())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettingsProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var provider settings.Provider
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	provider.ID = firstNonEmpty(provider.ID, nextSettingsID("provider"))
	provider.Enabled = true
	updated, err := s.settingsStore.Update(func(current *settings.RuntimeSettings) {
		current.LLM.Providers = append(current.LLM.Providers, provider)
		if current.LLM.ActiveProviderID == "" {
			current.LLM.ActiveProviderID = provider.ID
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, updated.PublicCopy())
}

func (s *Server) handleSettingsProviderByID(w http.ResponseWriter, r *http.Request) {
	id, action := parseSettingsSubpath(r.URL.Path, "/api/settings/providers/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("provider id is required"))
		return
	}
	if action == "activate" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		updated, err := s.settingsStore.Update(func(current *settings.RuntimeSettings) {
			current.LLM.ActiveProviderID = id
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, updated.PublicCopy())
		return
	}
	if action == "test" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		provider, err := s.providerByID(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err := s.runtimeLLM.TestProvider(r.Context(), provider); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	switch r.Method {
	case http.MethodPut:
		var provider settings.Provider
		if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.updateProvider(id, provider)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, updated.PublicCopy())
	case http.MethodDelete:
		updated, err := s.settingsStore.Update(func(current *settings.RuntimeSettings) {
			var providers []settings.Provider
			for _, item := range current.LLM.Providers {
				if item.ID != id {
					providers = append(providers, item)
				}
			}
			current.LLM.Providers = providers
			if current.LLM.ActiveProviderID == id {
				current.LLM.ActiveProviderID = ""
			}
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, updated.PublicCopy())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettingsPrompts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var prompt settings.Prompt
	if err := json.NewDecoder(r.Body).Decode(&prompt); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	prompt.ID = firstNonEmpty(prompt.ID, nextSettingsID("prompt"))
	if prompt.Kind == "" {
		prompt.Kind = "prompt"
	}
	prompt.Enabled = true
	updated, err := s.settingsStore.Update(func(current *settings.RuntimeSettings) {
		current.Prompts.Items = append(current.Prompts.Items, prompt)
		if current.Prompts.ActivePromptID == "" {
			current.Prompts.ActivePromptID = prompt.ID
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, updated.PublicCopy())
}

func (s *Server) handleSettingsPromptByID(w http.ResponseWriter, r *http.Request) {
	id, action := parseSettingsSubpath(r.URL.Path, "/api/settings/prompts/")
	if id == "load-file" {
		s.handleSettingsPromptLoadFile(w, r)
		return
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("prompt id is required"))
		return
	}
	if action == "activate" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		updated, err := s.settingsStore.Update(func(current *settings.RuntimeSettings) {
			current.Prompts.ActivePromptID = id
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, updated.PublicCopy())
		return
	}
	switch r.Method {
	case http.MethodPut:
		var prompt settings.Prompt
		if err := json.NewDecoder(r.Body).Decode(&prompt); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.updatePrompt(id, prompt)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, updated.PublicCopy())
	case http.MethodDelete:
		updated, err := s.settingsStore.Update(func(current *settings.RuntimeSettings) {
			var prompts []settings.Prompt
			for _, item := range current.Prompts.Items {
				if item.ID != id {
					prompts = append(prompts, item)
				}
			}
			current.Prompts.Items = prompts
			if current.Prompts.ActivePromptID == id {
				current.Prompts.ActivePromptID = ""
			}
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, updated.PublicCopy())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettingsPromptLoadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		writeError(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	kind := "prompt"
	if strings.EqualFold(filepathBase(path), "SKILL.md") {
		kind = "skill"
	}
	prompt := settings.Prompt{
		ID:         nextSettingsID("prompt"),
		Name:       filepathBase(path),
		Kind:       kind,
		Content:    string(data),
		SourcePath: path,
		Enabled:    true,
	}
	updated, err := s.settingsStore.Update(func(current *settings.RuntimeSettings) {
		current.Prompts.Items = append(current.Prompts.Items, prompt)
		current.Prompts.ActivePromptID = prompt.ID
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, updated.PublicCopy())
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func parseBool(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func splitLines(value string) []string {
	raw := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func normalizeTargets(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mergeMaskedSecrets(current, incoming settings.RuntimeSettings) settings.RuntimeSettings {
	for idx := range incoming.LLM.Providers {
		if incoming.LLM.Providers[idx].APIKey != "********" {
			continue
		}
		for _, existing := range current.LLM.Providers {
			if existing.ID == incoming.LLM.Providers[idx].ID {
				incoming.LLM.Providers[idx].APIKey = existing.APIKey
				break
			}
		}
	}
	return incoming
}

func (s *Server) providerByID(id string) (settings.Provider, error) {
	current, err := s.settingsStore.Load()
	if err != nil {
		return settings.Provider{}, err
	}
	for _, provider := range current.LLM.Providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return settings.Provider{}, fmt.Errorf("provider %s not found", id)
}

func (s *Server) updateProvider(id string, provider settings.Provider) (settings.RuntimeSettings, error) {
	current, err := s.settingsStore.Load()
	if err != nil {
		return settings.RuntimeSettings{}, err
	}
	provider.ID = id
	provider.Enabled = true
	provider = mergeMaskedProviderSecret(current, provider)
	found := false
	for idx := range current.LLM.Providers {
		if current.LLM.Providers[idx].ID == id {
			current.LLM.Providers[idx] = provider
			found = true
			break
		}
	}
	if !found {
		return settings.RuntimeSettings{}, fmt.Errorf("provider %s not found", id)
	}
	if err := s.settingsStore.Save(current); err != nil {
		return settings.RuntimeSettings{}, err
	}
	return s.settingsStore.Load()
}

func mergeMaskedProviderSecret(current settings.RuntimeSettings, provider settings.Provider) settings.Provider {
	if provider.APIKey != "********" {
		return provider
	}
	for _, existing := range current.LLM.Providers {
		if existing.ID == provider.ID {
			provider.APIKey = existing.APIKey
			break
		}
	}
	return provider
}

func (s *Server) updatePrompt(id string, prompt settings.Prompt) (settings.RuntimeSettings, error) {
	current, err := s.settingsStore.Load()
	if err != nil {
		return settings.RuntimeSettings{}, err
	}
	prompt.ID = id
	if strings.TrimSpace(prompt.Kind) == "" {
		prompt.Kind = "prompt"
	}
	prompt.Enabled = true
	found := false
	for idx := range current.Prompts.Items {
		if current.Prompts.Items[idx].ID == id {
			current.Prompts.Items[idx] = prompt
			found = true
			break
		}
	}
	if !found {
		return settings.RuntimeSettings{}, fmt.Errorf("prompt %s not found", id)
	}
	if err := s.settingsStore.Save(current); err != nil {
		return settings.RuntimeSettings{}, err
	}
	return s.settingsStore.Load()
}

func parseSettingsSubpath(path, prefix string) (id, action string) {
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	id = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		action = strings.TrimSpace(parts[1])
	}
	return id, action
}

func nextSettingsID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func filepathBase(path string) string {
	base := filepath.Base(path)
	if strings.TrimSpace(base) == "" || base == "." {
		return "prompt"
	}
	return base
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
