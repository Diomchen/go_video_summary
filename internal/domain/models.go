package domain

import "time"

type Segment struct {
	Index      int       `json:"index"`
	Start      string    `json:"start"`
	End        string    `json:"end"`
	Text       string    `json:"text"`
	Translated string    `json:"translated,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	Source     string    `json:"source"`
}

type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
)

type TaskMetrics struct {
	PreLLMProcessingDurationMs int64 `json:"preLlmProcessingDurationMs,omitempty"`
	TranslationDurationMs      int64 `json:"translationDurationMs,omitempty"`
	SummaryDurationMs          int64 `json:"summaryDurationMs,omitempty"`
	TotalTaskDurationMs        int64 `json:"totalTaskDurationMs,omitempty"`
}

type Task struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Mode             string         `json:"mode"`
	SourceURL        string         `json:"sourceUrl,omitempty"`
	Status           TaskStatus     `json:"status"`
	Stage            string         `json:"stage,omitempty"`
	ProgressPercent  float64        `json:"progressPercent,omitempty"`
	Translation      bool           `json:"translation"`
	SummaryRequested bool           `json:"summaryRequested"`
	SourceLanguage   string         `json:"sourceLanguage,omitempty"`
	TotalChunks      int            `json:"totalChunks,omitempty"`
	CompletedChunks  int            `json:"completedChunks,omitempty"`
	Transcript       string         `json:"transcript,omitempty"`
	TranslatedText   string         `json:"translatedText,omitempty"`
	Summary          string         `json:"summary,omitempty"`
	Error            string         `json:"error,omitempty"`
	Segments         []Segment      `json:"segments"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	OriginalFileName string         `json:"originalFileName,omitempty"`
	SavedFiles       []string       `json:"savedFiles,omitempty"`
	ExportTargets     []string       `json:"exportTargets,omitempty"`
	MarkdownExportDir string         `json:"markdownExportDir,omitempty"`
	ObsidianExportDir string         `json:"obsidianExportDir,omitempty"`
	Exports           []ExportResult `json:"exports,omitempty"`
	Metrics          *TaskMetrics   `json:"metrics,omitempty"`
	CheckpointDir    string         `json:"checkpointDir,omitempty"`
	InputFilePath    string         `json:"inputFilePath,omitempty"`
	SummaryError     string         `json:"summaryError,omitempty"`
	PendingSummary   bool           `json:"pendingSummary,omitempty"`
	Title            string         `json:"title,omitempty"`
	SourceLink       string         `json:"sourceLink,omitempty"`
	UPName           string         `json:"upName,omitempty"`
	Domain           string         `json:"domain,omitempty"`
	Tags             []string       `json:"tags,omitempty"`

	// Collection metadata (for Bilibili season/collection videos)
	CollectionName  string   `json:"collectionName,omitempty"`
	CollectionURL   string   `json:"collectionUrl,omitempty"`
	CollectionIndex int      `json:"collectionIndex,omitempty"`
	AuthorName      string   `json:"authorName,omitempty"`
	DomainTags      []string `json:"domainTags,omitempty"`
}

type ExportResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Target string `json:"target,omitempty"`
	Error  string `json:"error,omitempty"`
}

// TaskSummary is a lightweight representation of Task used in SSE events,
// excluding heavy fields like Transcript, TranslatedText, and Segments.
type TaskSummary struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Mode             string         `json:"mode"`
	SourceURL        string         `json:"sourceUrl,omitempty"`
	Status           TaskStatus     `json:"status"`
	Stage            string         `json:"stage,omitempty"`
	ProgressPercent  float64        `json:"progressPercent,omitempty"`
	Translation      bool           `json:"translation"`
	SummaryRequested bool           `json:"summaryRequested"`
	SourceLanguage   string         `json:"sourceLanguage,omitempty"`
	TotalChunks      int            `json:"totalChunks,omitempty"`
	CompletedChunks  int            `json:"completedChunks,omitempty"`
	Summary          string         `json:"summary,omitempty"`
	Error            string         `json:"error,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	OriginalFileName string         `json:"originalFileName,omitempty"`
	SavedFiles       []string       `json:"savedFiles,omitempty"`
	ExportTargets    []string       `json:"exportTargets,omitempty"`
	MarkdownExportDir string        `json:"markdownExportDir,omitempty"`
	ObsidianExportDir string        `json:"obsidianExportDir,omitempty"`
	Exports          []ExportResult `json:"exports,omitempty"`
	Metrics          *TaskMetrics   `json:"metrics,omitempty"`
	SummaryError     string         `json:"summaryError,omitempty"`
	PendingSummary   bool           `json:"pendingSummary,omitempty"`
	Title            string         `json:"title,omitempty"`
	SourceLink       string         `json:"sourceLink,omitempty"`
	UPName           string         `json:"upName,omitempty"`
	Domain           string         `json:"domain,omitempty"`
	Tags             []string       `json:"tags,omitempty"`
	CollectionName   string         `json:"collectionName,omitempty"`
	CollectionURL    string         `json:"collectionUrl,omitempty"`
	CollectionIndex  int            `json:"collectionIndex,omitempty"`
	AuthorName       string         `json:"authorName,omitempty"`
	DomainTags       []string       `json:"domainTags,omitempty"`
	HasTranscript    bool           `json:"hasTranscript,omitempty"`
}

// PaginatedTasks is the response for paginated task listing.
type PaginatedTasks struct {
	Tasks    []*Task `json:"tasks"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
}

type Event struct {
	Type    string `json:"type"`
	TaskID  string `json:"taskId"`
	Payload any    `json:"payload"`
}

type TaskStatusCallback struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Mode            string     `json:"mode"`
	Status          TaskStatus `json:"status"`
	Stage           string     `json:"stage,omitempty"`
	ProgressPercent float64    `json:"progressPercent,omitempty"`
	Error           string     `json:"error,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}
