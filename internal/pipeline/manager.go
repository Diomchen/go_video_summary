package pipeline

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go_subtitle_whisper/internal/audio"
	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/exporter"
	"go_subtitle_whisper/internal/service"
	"go_subtitle_whisper/internal/source"
)

type Broadcaster struct {
	mu      sync.Mutex
	clients map[chan domain.Event]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[chan domain.Event]struct{})}
}

func (b *Broadcaster) Subscribe() chan domain.Event {
	ch := make(chan domain.Event, 32)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan domain.Event) {
	b.mu.Lock()
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
}

func (b *Broadcaster) Publish(event domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- event:
		default:
		}
	}
}

type taskJob struct {
	id string
}

type Manager struct {
	transcriber      service.Transcriber
	translator       service.Translator
	summarizer       service.Summarizer
	bilibili         *source.BilibiliClient
	exporters        []exporter.MarkdownExporter
	exporterByName   map[string]exporter.MarkdownExporter
	events           *Broadcaster
	autoSave         bool
	outputDir        string
	store            *TaskStore
	chunkSeconds     int
	chunkParallelism int
	processJobs      chan taskJob
	summaryJobs      chan taskJob

	mu    sync.RWMutex
	tasks map[string]*domain.Task
}

func NewManager(transcriber service.Transcriber, translator service.Translator, summarizer service.Summarizer, bilibili *source.BilibiliClient, exporters []exporter.MarkdownExporter, events *Broadcaster, autoSave bool, outputDir string, checkpointDir string, chunkSeconds int, chunkParallelism int, processWorkers int, summaryWorkers int) *Manager {
	if processWorkers <= 0 {
		processWorkers = 1
	}
	if summaryWorkers <= 0 {
		summaryWorkers = 1
	}
	if chunkSeconds <= 0 {
		chunkSeconds = 45
	}
	if chunkParallelism <= 0 {
		chunkParallelism = 1
	}

	filteredExporters := make([]exporter.MarkdownExporter, 0, len(exporters))
	exporterByName := make(map[string]exporter.MarkdownExporter)
	for _, item := range exporters {
		if !exporter.IsNil(item) {
			filteredExporters = append(filteredExporters, item)
			exporterByName[item.Name()] = item
		}
	}

	m := &Manager{
		transcriber:      transcriber,
		translator:       translator,
		summarizer:       summarizer,
		bilibili:         bilibili,
		exporters:        filteredExporters,
		exporterByName:   exporterByName,
		events:           events,
		autoSave:         autoSave,
		outputDir:        outputDir,
		store:            NewTaskStore(checkpointDir),
		chunkSeconds:     chunkSeconds,
		chunkParallelism: chunkParallelism,
		processJobs:      make(chan taskJob, 256),
		summaryJobs:      make(chan taskJob, 256),
		tasks:            make(map[string]*domain.Task),
	}
	_ = m.store.Ensure()
	_ = m.restoreTasks()

	for i := 0; i < processWorkers; i++ {
		go m.processWorker()
	}
	for i := 0; i < summaryWorkers; i++ {
		go m.summaryWorker()
	}

	return m
}

func (m *Manager) CreateFileTask(name, filename string, data []byte, language string, translate, summarize bool, exportTargets []string) *domain.Task {
	task := m.newTask(name, "file", language, translate, summarize)
	task.OriginalFileName = filepath.Base(filename)
	task.CheckpointDir = m.store.TaskDir(task.ID)
	task.InputFilePath = m.store.InputPath(task.ID, filename)
	task.ExportTargets = m.normalizeExportTargets(exportTargets)
	task.Exports = defaultExports(task.ExportTargets)
	m.saveTask(task)
	if err := os.MkdirAll(task.CheckpointDir, 0o755); err == nil {
		_ = os.WriteFile(task.InputFilePath, data, 0o644)
	}
	m.persistTask(task.ID)
	m.enqueueProcess(task.ID)
	return task
}

func (m *Manager) CreateURLTask(name, rawURL, language string, translate, summarize bool, exportTargets []string) *domain.Task {
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = strings.TrimSpace(rawURL)
	}
	task := m.newTask(displayName, "url", language, translate, summarize)
	task.SourceURL = strings.TrimSpace(rawURL)
	task.CheckpointDir = m.store.TaskDir(task.ID)
	task.ExportTargets = m.normalizeExportTargets(exportTargets)
	task.Exports = defaultExports(task.ExportTargets)
	m.saveTask(task)
	m.persistTask(task.ID)
	m.enqueueProcess(task.ID)
	return task
}

func (m *Manager) CreateURLTasksFromInput(name, rawURL, language string, translate, summarize bool, exportTargets []string) ([]*domain.Task, error) {
	if source.IsCollectionURL(rawURL) {
		return m.CreateCollectionTasks(rawURL, language, translate, summarize, exportTargets)
	}
	return []*domain.Task{m.CreateURLTask(name, rawURL, language, translate, summarize, exportTargets)}, nil
}

func (m *Manager) CreateURLTaskWithMeta(name, rawURL, language string, translate, summarize bool, exportTargets []string, collectionName, collectionURL, authorName string, collectionIndex int) *domain.Task {
	task := m.CreateURLTask(name, rawURL, language, translate, summarize, exportTargets)
	if collectionName != "" || authorName != "" || collectionIndex > 0 {
		m.updateTask(task.ID, func(t *domain.Task) {
			t.CollectionName = collectionName
			t.CollectionURL = collectionURL
			t.AuthorName = authorName
			t.CollectionIndex = collectionIndex
		})
		m.persistTask(task.ID)
	}
	return task
}

func (m *Manager) CreateCollectionTasks(rawURL, language string, translate, summarize bool, exportTargets []string) ([]*domain.Task, error) {
	if m.bilibili == nil {
		return nil, fmt.Errorf("bilibili resolver is not configured")
	}
	collection, err := m.bilibili.ResolveCollection(context.Background(), rawURL)
	if err != nil {
		return nil, err
	}

	tasks := make([]*domain.Task, 0, len(collection.Videos))
	for i, video := range collection.Videos {
		task := m.CreateURLTaskWithMeta(
			video.Title,
			video.PageURL,
			language,
			translate,
			summarize,
			exportTargets,
			collection.Name,
			collection.URL,
			collection.Author,
			i+1, // 1-based index
		)
		tasks = append(tasks, task)
	}
	return tasks, nil
}

type CollectionPreviewResponse struct {
	Name   string                   `json:"name"`
	URL    string                   `json:"url"`
	Author string                   `json:"author"`
	Videos []CollectionPreviewVideo `json:"videos"`
}

type CollectionPreviewVideo struct {
	BVID    string `json:"bvid"`
	Title   string `json:"title"`
	PageURL string `json:"pageURL"`
	Index   int    `json:"index"`
}

func (m *Manager) CollectionPreview(ctx context.Context, rawURL string) (*CollectionPreviewResponse, error) {
	if m.bilibili == nil {
		return nil, fmt.Errorf("bilibili resolver is not configured")
	}
	collection, err := m.bilibili.ResolveCollection(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	videos := make([]CollectionPreviewVideo, len(collection.Videos))
	for i, v := range collection.Videos {
		videos[i] = CollectionPreviewVideo{
			BVID:    v.BVID,
			Title:   v.Title,
			PageURL: v.PageURL,
			Index:   i + 1,
		}
	}
	return &CollectionPreviewResponse{
		Name:   collection.Name,
		URL:    collection.URL,
		Author: collection.Author,
		Videos: videos,
	}, nil
}

func (m *Manager) BilibiliLoginStatus() map[string]bool {
	return map[string]bool{"loggedIn": m.bilibili != nil && m.bilibili.CachedLoginValid()}
}

func (m *Manager) StartBilibiliLogin(ctx context.Context) (*source.BilibiliQRCodeLogin, error) {
	if m.bilibili == nil {
		return nil, fmt.Errorf("bilibili resolver is not configured")
	}
	return m.bilibili.StartQRCodeLogin(ctx)
}

func (m *Manager) PollBilibiliLogin(ctx context.Context, qrcodeKey string) (*source.BilibiliQRCodePollResult, error) {
	if m.bilibili == nil {
		return nil, fmt.Errorf("bilibili resolver is not configured")
	}
	return m.bilibili.PollQRCodeLogin(ctx, qrcodeKey)
}

func (m *Manager) enqueueProcess(id string) {
	m.processJobs <- taskJob{id: id}
}

func (m *Manager) enqueueSummary(id string) {
	m.updateTask(id, func(t *domain.Task) {
		t.Stage = "pending_summary"
		t.PendingSummary = true
	})
	m.publishTask(id)
	m.summaryJobs <- taskJob{id: id}
}

func (m *Manager) processWorker() {
	for job := range m.processJobs {
		task, ok := m.GetTask(job.id)
		if !ok {
			continue
		}
		switch task.Mode {
		case "url":
			m.runURLTask(context.Background(), task.ID)
		default:
			m.runFileTask(context.Background(), task.ID)
		}
	}
}

func (m *Manager) summaryWorker() {
	for job := range m.summaryJobs {
		m.runSummaryPipeline(context.Background(), job.id)
	}
}

func (m *Manager) GetTask(id string) (*domain.Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	cloned := cloneTask(task)
	return &cloned, true
}

func (m *Manager) ListTasks() []*domain.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tasks := make([]*domain.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		cloned := cloneTask(task)
		tasks = append(tasks, &cloned)
	}
	return tasks
}

func (m *Manager) GetTaskStatusCallback(id string) (*domain.TaskStatusCallback, bool) {
	task, ok := m.GetTask(id)
	if !ok {
		return nil, false
	}
	return &domain.TaskStatusCallback{
		ID:              task.ID,
		Name:            task.Name,
		Mode:            task.Mode,
		Status:          task.Status,
		Stage:           task.Stage,
		ProgressPercent: task.ProgressPercent,
		Error:           task.Error,
		CreatedAt:       task.CreatedAt,
		UpdatedAt:       task.UpdatedAt,
	}, true
}

func cloneTask(task *domain.Task) domain.Task {
	cloned := *task
	cloned.Segments = append([]domain.Segment(nil), task.Segments...)
	cloned.SavedFiles = append([]string(nil), task.SavedFiles...)
	cloned.ExportTargets = append([]string(nil), task.ExportTargets...)
	cloned.Exports = append([]domain.ExportResult(nil), task.Exports...)
	if task.Metrics != nil {
		metrics := *task.Metrics
		cloned.Metrics = &metrics
	}
	return cloned
}

func defaultExports(targets []string) []domain.ExportResult {
	results := make([]domain.ExportResult, 0, len(targets))
	for _, item := range targets {
		if strings.TrimSpace(item) == "" {
			continue
		}
		results = append(results, domain.ExportResult{Name: item, Status: "pending"})
	}
	return results
}

func ensureTaskMetrics(task *domain.Task) *domain.TaskMetrics {
	if task.Metrics == nil {
		task.Metrics = &domain.TaskMetrics{}
	}
	return task.Metrics
}

func durationMilliseconds(start time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func durationMillisecondsSince(start time.Time, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func (m *Manager) updateTaskMetrics(id string, mut func(*domain.TaskMetrics)) {
	m.updateTask(id, func(task *domain.Task) {
		mut(ensureTaskMetrics(task))
	})
}

func (m *Manager) markTaskFinished(id string, finishedAt time.Time) {
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	m.updateTask(id, func(task *domain.Task) {
		metrics := ensureTaskMetrics(task)
		metrics.TotalTaskDurationMs = durationMillisecondsSince(task.CreatedAt, finishedAt)
	})
}

func (m *Manager) newTask(name, mode, language string, translate, summarize bool) *domain.Task {
	now := time.Now()
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("%s-%d", mode, now.Unix())
	}
	return &domain.Task{
		ID:               m.nextTaskID(),
		Name:             name,
		Mode:             mode,
		Status:           domain.TaskQueued,
		Stage:            "queued",
		ProgressPercent:  0,
		Translation:      translate,
		SummaryRequested: summarize,
		SourceLanguage:   language,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func (m *Manager) saveTask(task *domain.Task) {
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()
	_ = m.store.SaveTask(task)
	m.events.Publish(domain.Event{Type: "task.created", TaskID: task.ID, Payload: task})
}

func (m *Manager) updateTask(id string, mut func(*domain.Task)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	if task == nil {
		return
	}
	mut(task)
	task.UpdatedAt = time.Now()
	_ = m.store.SaveTask(task)
}

func (m *Manager) publishTask(id string) {
	if task, ok := m.GetTask(id); ok {
		m.events.Publish(domain.Event{Type: "task.updated", TaskID: id, Payload: task})
	}
}

func (m *Manager) failTask(id string, err error) {
	finishedAt := time.Now()
	m.updateTask(id, func(task *domain.Task) {
		task.Status = domain.TaskFailed
		task.Stage = "failed"
		task.Error = err.Error()
	})
	m.markTaskFinished(id, finishedAt)
	m.publishTask(id)
}

func (m *Manager) failSummary(id, transcript, translated string, err error) {
	finishedAt := time.Now()
	m.updateTask(id, func(task *domain.Task) {
		task.Status = domain.TaskFailed
		task.Stage = "summary_failed"
		task.Error = err.Error()
		task.SummaryError = err.Error()
		if transcript != "" {
			task.Transcript = transcript
		}
		if translated != "" {
			task.TranslatedText = translated
		}
	})
	m.markTaskFinished(id, finishedAt)
	m.publishTask(id)
}

func (m *Manager) completeTask(id string, transcript, translated, summary string) {
	finishedAt := time.Now()
	m.updateTask(id, func(task *domain.Task) {
		task.Status = domain.TaskCompleted
		task.Stage = "completed"
		task.ProgressPercent = 100
		task.Transcript = transcript
		task.TranslatedText = translated
		task.Summary = summary
		task.Error = ""
		task.SummaryError = ""
	})
	m.markTaskFinished(id, finishedAt)
	m.publishTask(id)
}

func (m *Manager) setTaskProgress(id string, stage string, percent float64) {
	m.updateTask(id, func(task *domain.Task) {
		task.Stage = stage
		task.ProgressPercent = percent
	})
	m.publishTask(id)
}

func (m *Manager) RetrySummary(id string) (*domain.Task, error) {
	task, ok := m.GetTask(id)
	if !ok {
		return nil, fmt.Errorf("task %s not found", id)
	}
	if strings.TrimSpace(task.Transcript) == "" {
		return nil, fmt.Errorf("task %s has no transcript to summarize", id)
	}
	if m.summarizer == nil {
		return nil, fmt.Errorf("summarizer is not configured")
	}

	m.updateTask(id, func(task *domain.Task) {
		task.SummaryRequested = true
		task.Status = domain.TaskRunning
		task.Stage = "pending_summary"
		task.ProgressPercent = 100
		task.Error = ""
		task.SummaryError = ""
	})
	m.publishTask(id)

	go m.enqueueSummary(id)
	updated, _ := m.GetTask(id)
	return updated, nil
}

func (m *Manager) GenerateSummary(id string) (*domain.Task, error) {
	task, ok := m.GetTask(id)
	if !ok {
		return nil, fmt.Errorf("task %s not found", id)
	}
	if strings.TrimSpace(task.Transcript) == "" {
		return nil, fmt.Errorf("task %s has no transcript to summarize", id)
	}
	if m.summarizer == nil {
		return nil, fmt.Errorf("summarizer is not configured")
	}
	if strings.TrimSpace(task.Summary) != "" {
		return nil, fmt.Errorf("task %s already has a summary", id)
	}

	m.updateTask(id, func(task *domain.Task) {
		task.SummaryRequested = true
		task.Status = domain.TaskRunning
		task.Stage = "pending_summary"
		task.ProgressPercent = 100
		task.Error = ""
		task.SummaryError = ""
	})
	m.publishTask(id)

	go m.enqueueSummary(id)
	updated, _ := m.GetTask(id)
	return updated, nil
}

func (m *Manager) RetryExports(id string, targets []string) (*domain.Task, error) {
	task, ok := m.GetTask(id)
	if !ok {
		return nil, fmt.Errorf("task %s not found", id)
	}
	if strings.TrimSpace(task.Summary) == "" {
		return nil, fmt.Errorf("task %s has no summary to export", id)
	}

	targets = m.normalizeExportTargets(targets)
	if len(targets) == 0 {
		targets = append([]string(nil), task.ExportTargets...)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("task %s has no available export targets", id)
	}

	mergedTargets := mergeStrings(task.ExportTargets, targets)
	m.updateTask(id, func(task *domain.Task) {
		task.ExportTargets = mergedTargets
		if len(task.Exports) == 0 {
			task.Exports = defaultExports(mergedTargets)
		}
	})

	m.updateTask(id, func(task *domain.Task) {
		task.Stage = "uploading"
		task.Error = ""
	})
	m.publishTask(id)

	go func() {
		if err := m.exportSummary(context.Background(), id, targets); err != nil {
			m.failSummary(id, task.Transcript, task.TranslatedText, err)
			return
		}
		m.updateTask(id, func(task *domain.Task) {
			task.Status = domain.TaskCompleted
			task.Stage = "completed"
			task.Error = ""
		})
		m.publishTask(id)
		m.cleanupExpiredArtifacts(24 * time.Hour)
	}()

	updated, _ := m.GetTask(id)
	return updated, nil
}

func (m *Manager) runFileTask(ctx context.Context, id string) {
	task, ok := m.GetTask(id)
	if !ok {
		return
	}
	m.updateTask(id, func(task *domain.Task) {
		task.Status = domain.TaskRunning
		task.Stage = "preparing"
		task.Error = ""
	})
	m.publishTask(id)

	data, err := os.ReadFile(task.InputFilePath)
	if err != nil {
		m.failTask(id, err)
		return
	}

	filename := task.OriginalFileName
	if filename == "" {
		filename = filepath.Base(task.InputFilePath)
	}
	m.runProcessPipeline(ctx, id, filename, data)
}

func (m *Manager) runURLTask(ctx context.Context, id string) {
	if m.bilibili == nil {
		m.failTask(id, fmt.Errorf("bilibili resolver is not configured"))
		return
	}

	task, ok := m.GetTask(id)
	if !ok {
		return
	}
	m.updateTask(id, func(task *domain.Task) {
		task.Status = domain.TaskRunning
		task.Stage = "resolving_url"
		task.Error = ""
	})
	m.publishTask(id)

	filename := task.OriginalFileName
	data := []byte(nil)
	if !transcriptReady(task) {
		if task.InputFilePath != "" {
			if existing, err := os.ReadFile(task.InputFilePath); err == nil {
				data = existing
				filename = filepath.Base(task.InputFilePath)
			}
		}
		if len(data) == 0 {
			media, err := m.bilibili.Resolve(ctx, task.SourceURL)
			if err != nil {
				m.failTask(id, err)
				return
			}
			mediaFilename := "source" + media.Ext
			mediaPath := m.store.InputPath(id, mediaFilename)
			m.updateTask(id, func(task *domain.Task) {
				if strings.TrimSpace(media.Title) != "" {
					task.Name = media.Title
				}
				task.SourceURL = media.PageURL
				task.OriginalFileName = mediaFilename
				task.InputFilePath = mediaPath
			})
			m.publishTask(id)
			m.setTaskProgress(id, "downloading_media", 10)
			if err := m.bilibili.DownloadAudio(ctx, media, mediaPath, func(percent float64) {
				m.setTaskProgress(id, "downloading_media", 10+percent*0.2)
			}); err != nil {
				m.failTask(id, err)
				return
			}
			data, err = os.ReadFile(mediaPath)
			if err != nil {
				m.failTask(id, err)
				return
			}
			filename = mediaFilename
		}
	}
	if filename == "" {
		filename = "source.m4a"
	}
	m.runProcessPipeline(ctx, id, filename, data)
}

func (m *Manager) runProcessPipeline(ctx context.Context, id, filename string, data []byte) {
	task, ok := m.GetTask(id)
	if !ok {
		return
	}

	text, err := m.transcribeWithCheckpoint(ctx, task, filename, data)
	if err != nil {
		m.failTask(id, err)
		return
	}

	if task.Mode == "url" {
		_ = m.cleanupInputFile(id)
	}

	translated := ""
	if task.Translation && m.translator != nil {
		m.setTaskProgress(id, "translating", 100)
		startedAt := time.Now()
		translated, err = m.translator.Translate(ctx, text, task.SourceLanguage)
		elapsed := durationMilliseconds(startedAt)
		m.updateTaskMetrics(id, func(metrics *domain.TaskMetrics) {
			metrics.TranslationDurationMs = elapsed
		})
		if err != nil {
			m.failTask(id, err)
			return
		}
	}

	// Store transcript/translation, then route to summary queue or complete.
	m.updateTask(id, func(task *domain.Task) {
		task.Transcript = text
		task.TranslatedText = translated
		task.PendingSummary = task.SummaryRequested
	})
	m.publishTask(id)

	if task.SummaryRequested {
		m.enqueueSummary(id)
		return
	}

	// No summary needed — complete and auto-save.
	m.completeTask(id, text, translated, "")
	m.setTaskProgress(id, "saving", 100)
	if err := m.autoSaveOutputs(ctx, id); err != nil {
		m.failTask(id, err)
		return
	}
	m.setTaskProgress(id, "completed", 100)
	m.cleanupExpiredArtifacts(24 * time.Hour)
}

func (m *Manager) runSummaryPipeline(ctx context.Context, id string) {
	task, ok := m.GetTask(id)
	if !ok {
		return
	}

	m.updateTask(id, func(task *domain.Task) {
		task.Status = domain.TaskRunning
		task.Stage = "summarizing"
		task.Error = ""
		task.SummaryError = ""
	})
	m.publishTask(id)

	sourceText := task.Transcript
	if strings.TrimSpace(task.TranslatedText) != "" {
		sourceText = task.TranslatedText
	}

	startedAt := time.Now()
	summary, domainTags, err := m.summarizer.Summarize(ctx, sourceText, service.SummaryOptions{
		Title:           task.Name,
		SourceURL:       task.SourceURL,
		BVID:            source.ExtractBVID(task.SourceURL),
		CollectionName:  task.CollectionName,
		CollectionIndex: task.CollectionIndex,
		AuthorName:      task.AuthorName,
	})
	elapsed := durationMilliseconds(startedAt)
	m.updateTaskMetrics(id, func(metrics *domain.TaskMetrics) {
		metrics.SummaryDurationMs = elapsed
	})
	if err != nil {
		m.failSummary(id, task.Transcript, task.TranslatedText, err)
		return
	}

	m.completeTask(id, task.Transcript, task.TranslatedText, summary)
	if len(domainTags) > 0 {
		m.updateTask(id, func(task *domain.Task) {
			task.DomainTags = domainTags
			task.PendingSummary = false
		})
	} else {
		m.updateTask(id, func(task *domain.Task) {
			task.PendingSummary = false
		})
	}
	m.setTaskProgress(id, "saving", 100)
	if err := m.autoSaveOutputs(ctx, id); err != nil {
		m.failSummary(id, task.Transcript, task.TranslatedText, err)
		return
	}
	m.setTaskProgress(id, "completed", 100)
	m.cleanupExpiredArtifacts(24 * time.Hour)
}

func (m *Manager) cleanupInputFile(id string) error {
	task, ok := m.GetTask(id)
	if !ok || strings.TrimSpace(task.InputFilePath) == "" {
		return nil
	}
	if err := os.Remove(task.InputFilePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.updateTask(id, func(task *domain.Task) {
		task.InputFilePath = ""
	})
	m.publishTask(id)
	return nil
}

func (m *Manager) autoSaveOutputs(ctx context.Context, id string) error {
	task, ok := m.GetTask(id)
	if !ok {
		return nil
	}
	saved, _, err := saveTaskOutputs(task, m.outputDir, m.autoSave)
	if err != nil {
		return err
	}
	m.updateTask(id, func(task *domain.Task) {
		task.SavedFiles = append([]string(nil), saved...)
	})
	m.publishTask(id)

	if strings.TrimSpace(task.Summary) == "" || len(task.ExportTargets) == 0 {
		return nil
	}
	return m.exportSummary(ctx, id, task.ExportTargets)
}

func (m *Manager) persistTask(id string) {
	task, ok := m.GetTask(id)
	if !ok {
		return
	}
	_ = m.store.SaveTask(task)
}

func (m *Manager) transcribeWithCheckpoint(ctx context.Context, task *domain.Task, filename string, data []byte) (string, error) {
	transcriptPath := filepath.Join(task.CheckpointDir, "transcript.txt")
	segmentsPath := filepath.Join(task.CheckpointDir, "segments.json")
	if task.CheckpointDir != "" {
		if saved, err := os.ReadFile(transcriptPath); err == nil && task.CompletedChunks == task.TotalChunks && task.TotalChunks > 0 {
			m.updateTask(task.ID, func(t *domain.Task) {
				metrics := ensureTaskMetrics(t)
				if metrics.PreLLMProcessingDurationMs == 0 {
					metrics.PreLLMProcessingDurationMs = durationMillisecondsSince(t.CreatedAt, time.Now())
				}
			})
			return strings.TrimSpace(string(saved)), nil
		}
	}

	wavData, err := convertInputToStandardWAV(ctx, task.InputFilePath, filename, data)
	if err != nil {
		return "", err
	}
	pcm, sampleRate, channels, err := audio.WAVToPCM16(wavData)
	if err != nil {
		return "", err
	}

	chunkBytes := sampleRate * channels * 2 * m.chunkSeconds
	if chunkBytes <= 0 {
		chunkBytes = len(pcm)
	}
	totalChunks := 0
	if chunkBytes > 0 {
		totalChunks = (len(pcm) + chunkBytes - 1) / chunkBytes
	}
	m.updateTask(task.ID, func(t *domain.Task) {
		t.TotalChunks = totalChunks
	})

	checkpoints, err := loadChunkCheckpoints(task)
	if err != nil {
		return "", err
	}
	if len(checkpoints) == 0 && task.CompletedChunks > 0 {
		checkpoints, err = migrateLegacyChunkCheckpoints(task, totalChunks, m.chunkSeconds, segmentsPath)
		if err != nil {
			return "", err
		}
		for _, state := range checkpoints {
			if err := saveChunkCheckpoint(task, state); err != nil {
				return "", err
			}
		}
	}

	var checkpointMu sync.Mutex
	chunkProgress := make(map[int]float64, totalChunks)
	pendingChunks := make([]int, 0, totalChunks)
	for idx := 0; idx < totalChunks; idx++ {
		state, ok := checkpoints[idx]
		if !ok {
			pendingChunks = append(pendingChunks, idx)
			chunkProgress[idx] = 0
			continue
		}
		if state.Status == chunkStatusRunning {
			state.Status = chunkStatusPending
			state.Error = ""
			state.UpdatedAt = time.Now()
			checkpoints[idx] = state
			if err := saveChunkCheckpoint(task, state); err != nil {
				return "", err
			}
		}
		if state.Status == chunkStatusDone {
			chunkProgress[idx] = 100
			continue
		}
		chunkProgress[idx] = 0
		pendingChunks = append(pendingChunks, idx)
	}

	updateTaskCheckpoint := func() error {
		transcript, segments, doneCount := aggregateChunkResults(task, checkpoints, totalChunks, m.chunkSeconds)
		if err := persistAggregatedTranscription(task, transcriptPath, segmentsPath, transcript, segments); err != nil {
			return err
		}
		m.updateTask(task.ID, func(t *domain.Task) {
			t.CompletedChunks = doneCount
			t.Transcript = transcript
			t.Segments = append([]domain.Segment(nil), segments...)
		})
		return nil
	}

	if err := updateTaskCheckpoint(); err != nil {
		return "", err
	}
	if len(pendingChunks) == 0 {
		transcript, _, _ := aggregateChunkResults(task, checkpoints, totalChunks, m.chunkSeconds)
		return transcript, nil
	}

	reportProgress := func(stage string, idx int, percent float64) {
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		checkpointMu.Lock()
		if percent > chunkProgress[idx] {
			chunkProgress[idx] = percent
		}
		totalProgress := 0.0
		for chunkIdx := 0; chunkIdx < totalChunks; chunkIdx++ {
			totalProgress += chunkProgress[chunkIdx]
		}
		checkpointMu.Unlock()

		if strings.TrimSpace(stage) == "" {
			stage = "transcribing"
		}
		overall := 0.0
		if totalChunks > 0 {
			overall = totalProgress / float64(totalChunks)
		}
		m.setTaskProgress(task.ID, stage, overall)
	}

	workerCount := m.chunkParallelism
	if workerCount > len(pendingChunks) {
		workerCount = len(pendingChunks)
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	progressTranscriber, hasProgress := m.transcriber.(service.ProgressTranscriber)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int, len(pendingChunks))
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	failChunk := func(idx int, runErr error) {
		state := chunkCheckpoint{
			Index:     idx,
			Start:     formatSeconds(idx * m.chunkSeconds),
			End:       formatSeconds((idx + 1) * m.chunkSeconds),
			Status:    chunkStatusFailed,
			Error:     runErr.Error(),
			UpdatedAt: time.Now(),
		}
		checkpointMu.Lock()
		checkpoints[idx] = state
		checkpointMu.Unlock()
		_ = saveChunkCheckpoint(task, state)
		select {
		case errCh <- runErr:
			cancel()
		default:
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				select {
				case <-workerCtx.Done():
					return
				default:
				}

				state := chunkCheckpoint{
					Index:     idx,
					Start:     formatSeconds(idx * m.chunkSeconds),
					End:       formatSeconds((idx + 1) * m.chunkSeconds),
					Status:    chunkStatusRunning,
					UpdatedAt: time.Now(),
				}
				checkpointMu.Lock()
				checkpoints[idx] = state
				checkpointMu.Unlock()
				if err := saveChunkCheckpoint(task, state); err != nil {
					failChunk(idx, err)
					return
				}

				start := idx * chunkBytes
				end := start + chunkBytes
				if end > len(pcm) {
					end = len(pcm)
				}
				chunkWAV, err := audio.PCM16ToWAV(pcm[start:end], sampleRate, channels)
				if err != nil {
					failChunk(idx, err)
					return
				}

				chunkFilename := fmt.Sprintf("%s.chunk.%03d.wav", filename, idx)
				var chunkText string
				if hasProgress {
					chunkText, err = progressTranscriber.TranscribeFileWithProgress(workerCtx, chunkFilename, chunkWAV, task.SourceLanguage, func(update service.ProgressUpdate) {
						stage := update.Message
						if strings.TrimSpace(stage) == "" {
							stage = "transcribing"
						}
						reportProgress(stage, idx, update.Percent)
					})
				} else {
					chunkText, err = m.transcriber.TranscribeFile(workerCtx, chunkFilename, chunkWAV, task.SourceLanguage)
					reportProgress("transcribing", idx, 100)
				}
				if err != nil {
					failChunk(idx, err)
					return
				}

				state.Status = chunkStatusDone
				state.Text = strings.TrimSpace(chunkText)
				state.Error = ""
				state.UpdatedAt = time.Now()
				checkpointMu.Lock()
				checkpoints[idx] = state
				checkpointMu.Unlock()
				if err := saveChunkCheckpoint(task, state); err != nil {
					failChunk(idx, err)
					return
				}
				if err := updateTaskCheckpoint(); err != nil {
					failChunk(idx, err)
					return
				}
				reportProgress("transcribing", idx, 100)
			}
		}()
	}

	for _, idx := range pendingChunks {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return "", err
	default:
	}

	transcript, _, _ := aggregateChunkResults(task, checkpoints, totalChunks, m.chunkSeconds)
	m.updateTask(task.ID, func(t *domain.Task) {
		metrics := ensureTaskMetrics(t)
		if metrics.PreLLMProcessingDurationMs == 0 {
			metrics.PreLLMProcessingDurationMs = durationMillisecondsSince(t.CreatedAt, time.Now())
		}
	})
	return transcript, nil
}

func (m *Manager) restoreTasks() error {
	tasks, err := m.store.LoadAll()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		task.ExportTargets = m.normalizeExportTargets(task.ExportTargets)
		if task.Exports == nil {
			task.Exports = defaultExports(task.ExportTargets)
		}
		m.tasks[task.ID] = task
		if task.Status == domain.TaskQueued || task.Status == domain.TaskRunning {
			if task.Stage == "pending_summary" {
				task.Status = domain.TaskRunning
				m.enqueueSummary(task.ID)
			} else {
				task.Status = domain.TaskQueued
				task.Stage = "queued"
				m.enqueueProcess(task.ID)
			}
		}
	}
	return nil
}

func (m *Manager) nextTaskID() string {
	for {
		id, err := randomTaskID()
		if err != nil {
			now := time.Now().UnixNano()
			id = fmt.Sprintf("task%x", now)
		}

		m.mu.RLock()
		_, exists := m.tasks[id]
		m.mu.RUnlock()
		if !exists {
			return id
		}
	}
}

func randomTaskID() (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const digits = "0123456789"
	const all = letters + digits
	const size = 12

	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	hasLetter := false
	hasDigit := false
	for i := range buf {
		ch := all[int(buf[i])%len(all)]
		if strings.ContainsRune(letters, rune(ch)) {
			hasLetter = true
		}
		if strings.ContainsRune(digits, rune(ch)) {
			hasDigit = true
		}
		buf[i] = ch
	}
	if !hasLetter {
		buf[0] = letters[int(buf[0])%len(letters)]
	}
	if !hasDigit {
		buf[len(buf)-1] = digits[int(buf[len(buf)-1])%len(digits)]
	}
	return "task" + string(buf), nil
}

func (m *Manager) cleanupExpiredArtifacts(retention time.Duration) {
	if retention <= 0 {
		return
	}

	cutoff := time.Now().Add(-retention)
	m.cleanupExpiredOutputFiles(cutoff)
	m.cleanupExpiredTasks(cutoff)
}

func (m *Manager) cleanupExpiredOutputFiles(cutoff time.Time) {
	entries, err := os.ReadDir(m.outputDir)
	if err != nil {
		return
	}

	checkpointRoot, _ := filepath.Abs(m.store.Root())
	for _, entry := range entries {
		path := filepath.Join(m.outputDir, entry.Name())
		absPath, err := filepath.Abs(path)
		if err == nil && absPath == checkpointRoot {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			continue
		}
	}
}

func (m *Manager) cleanupExpiredTasks(cutoff time.Time) {
	ids := make([]string, 0)

	m.mu.RLock()
	for id, task := range m.tasks {
		if task == nil {
			continue
		}
		if task.Status != domain.TaskCompleted && task.Status != domain.TaskFailed {
			continue
		}
		if task.UpdatedAt.After(cutoff) {
			continue
		}
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		if err := m.store.RemoveTask(id); err != nil {
			continue
		}
		m.mu.Lock()
		if _, ok := m.tasks[id]; ok {
			delete(m.tasks, id)
		}
		m.mu.Unlock()
		m.events.Publish(domain.Event{Type: "task.deleted", TaskID: id, Payload: map[string]string{"id": id}})
	}
}

func (m *Manager) exportSummary(ctx context.Context, id string, targets []string) error {
	task, ok := m.GetTask(id)
	if !ok {
		return nil
	}

	summaryPath := findSummaryPath(task.SavedFiles)
	if summaryPath == "" {
		_, generated, err := saveTaskOutputs(task, m.outputDir, m.autoSave)
		if err != nil {
			return err
		}
		summaryPath = generated
	}

	results := append([]domain.ExportResult(nil), task.Exports...)
	for _, target := range targets {
		item, ok := m.exporterByName[target]
		if !ok || exporter.IsNil(item) {
			results = upsertExportResult(results, domain.ExportResult{Name: target, Status: "failed", Error: "exporter not configured"})
			continue
		}
		result, exportErr := item.ExportMarkdown(ctx, task, summaryPath, task.Summary)
		if exportErr != nil {
			result.Name = target
			result.Status = "failed"
			result.Error = exportErr.Error()
		}
		results = upsertExportResult(results, result)
	}

	m.updateTask(id, func(task *domain.Task) {
		task.Exports = results
	})
	m.publishTask(id)
	return nil
}

func (m *Manager) normalizeExportTargets(targets []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		target = strings.ToLower(strings.TrimSpace(target))
		if target == "" {
			continue
		}
		if _, ok := m.exporterByName[target]; !ok {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

func upsertExportResult(results []domain.ExportResult, result domain.ExportResult) []domain.ExportResult {
	for idx := range results {
		if results[idx].Name == result.Name {
			results[idx] = result
			return results
		}
	}
	return append(results, result)
}

func findSummaryPath(paths []string) string {
	for _, path := range paths {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".summary.md") {
			return path
		}
	}
	return ""
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mergeStrings(base []string, extra []string) []string {
	out := append([]string(nil), base...)
	for _, item := range extra {
		if containsString(out, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func transcriptReady(task *domain.Task) bool {
	return task.TotalChunks > 0 && task.CompletedChunks == task.TotalChunks
}

func convertInputToStandardWAV(ctx context.Context, inputPath, filename string, data []byte) ([]byte, error) {
	if inputPath == "" && len(data) == 0 {
		return nil, fmt.Errorf("missing input file path")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".wav" {
		if len(data) > 0 {
			return data, nil
		}
		return os.ReadFile(inputPath)
	}
	ffmpegBin := resolveFFmpegBin()
	if ffmpegBin == "" {
		return nil, fmt.Errorf("ffmpeg is required for resumable transcription of non-wav files")
	}
	if inputPath == "" {
		return nil, fmt.Errorf("missing input file path")
	}
	outputPath := filepath.Join(filepath.Dir(inputPath), "input.standard.wav")
	cmd := exec.CommandContext(ctx, ffmpegBin, "-hide_banner", "-loglevel", "error", "-y", "-i", inputPath, "-ar", "16000", "-ac", "1", outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg convert failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return os.ReadFile(outputPath)
}

func formatSeconds(total int) string {
	hours := total / 3600
	minutes := (total % 3600) / 60
	seconds := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func jsonMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return bytes.TrimSpace(buf.Bytes()), err
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func resolveFFmpegBin() string {
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}

	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "ffmpeg.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "ffmpeg.exe"),
	}
	patterns := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", "*", "*", "bin", "ffmpeg.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", "*", "*", "ffmpeg.exe"),
	}
	for _, pattern := range patterns {
		if matches, err := filepath.Glob(pattern); err == nil {
			candidates = append(candidates, matches...)
		}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
