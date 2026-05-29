package app

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"go_subtitle_whisper/internal/config"
	"go_subtitle_whisper/internal/exporter"
	"go_subtitle_whisper/internal/llm"
	"go_subtitle_whisper/internal/pipeline"
	"go_subtitle_whisper/internal/service"
	"go_subtitle_whisper/internal/source"
	"go_subtitle_whisper/internal/transcribe"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	cfg     config.Config
	manager *pipeline.Manager
	events  *pipeline.Broadcaster
}

type urlTaskRequest struct {
	Name          string   `json:"name"`
	URLs          []string `json:"urls"`
	URLsText      string   `json:"urlsText"`
	Language      string   `json:"language"`
	Translate     bool     `json:"translate"`
	Summarize     bool     `json:"summarize"`
	ExportTargets []string `json:"exportTargets"`
}

type exportRetryRequest struct {
	Targets []string `json:"targets"`
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

	var translator service.Translator
	var summarizer service.Summarizer
	if cfg.LLMModel != "" {
		client := llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTimeout)
		translator = client
		summarizer = client
	}

	bilibiliClient := source.NewBilibiliClient(cfg.BilibiliUserAgent, cfg.BilibiliTimeout)
	bilibiliClient.UseCookieCache(cfg.BilibiliCookieCache, cfg.BilibiliCookieTTL)
	var exportersList []exporter.MarkdownExporter
	if item := exporter.NewNotionExporter(cfg.NotionToken, cfg.NotionVersion, cfg.NotionParentPage); item != nil {
		exportersList = append(exportersList, item)
	}
	if item := exporter.NewObsidianExporter(cfg.ObsidianVaultDir, cfg.ObsidianSubdir); item != nil {
		exportersList = append(exportersList, item)
	}
	if item := exporter.NewIMAExporter(cfg.IMAOpenAPIClientID, cfg.IMAOpenAPIAPIKey, cfg.IMAOpenAPIFolderID); item != nil {
		exportersList = append(exportersList, item)
	}

	manager := pipeline.NewManager(transcriber, translator, summarizer, bilibiliClient, exportersList, events, cfg.AutoSaveResults, cfg.OutputDir, cfg.CheckpointDir, cfg.ChunkSeconds, cfg.ChunkParallelism, cfg.TaskWorkers, cfg.SummaryWorkers)
	return &Server{cfg: cfg, manager: manager, events: events}, nil
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
			"obsidian": strings.TrimSpace(s.cfg.ObsidianVaultDir) != "",
			"ima":      strings.TrimSpace(s.cfg.IMAOpenAPIClientID) != "" && strings.TrimSpace(s.cfg.IMAOpenAPIAPIKey) != "",
		},
	})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.manager.ListTasks())
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
			task := s.manager.CreateFileTask(name, header.Filename, data, language, translate, summarize, exportTargets)
			tasks = append(tasks, task)
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
	tasks := make([]any, 0, len(urls))
	for _, rawURL := range urls {
		created, err := s.manager.CreateURLTasksFromInput(req.Name, rawURL, language, req.Translate, req.Summarize, normalizeTargets(req.ExportTargets))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		for _, task := range created {
			tasks = append(tasks, task)
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
