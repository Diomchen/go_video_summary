package exporter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/filenames"
)

type MarkdownFolderExporter struct {
	dir string
}

func NewMarkdownFolderExporter(dir string) *MarkdownFolderExporter {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	return &MarkdownFolderExporter{dir: dir}
}

func (e *MarkdownFolderExporter) Name() string {
	return "markdown"
}

func (e *MarkdownFolderExporter) ExportMarkdown(_ context.Context, task *domain.Task, _ string, markdown string) (domain.ExportResult, error) {
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	target := filepath.Join(e.dir, filenames.WithSuffix(task, time.Now(), ".md"))
	target = filenames.UniquePath(target)
	content := buildPortableMarkdownContent(task, markdown)
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	return domain.ExportResult{Name: e.Name(), Status: "success", Target: target}, nil
}

func buildPortableMarkdownContent(task *domain.Task, markdown string) string {
	return buildMetadataMarkdownContent(task, markdown, false)
}
