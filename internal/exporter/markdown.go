package exporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go_subtitle_whisper/internal/domain"
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

func (e *MarkdownFolderExporter) ExportMarkdown(_ context.Context, task *domain.Task, markdownPath string, markdown string) (domain.ExportResult, error) {
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	filename := filepath.Base(markdownPath)
	if strings.TrimSpace(filename) == "" || filename == "." {
		filename = fmt.Sprintf("%s.summary.md", safeSlug(task.Name))
	}
	target := filepath.Join(e.dir, filename)
	content := buildPortableMarkdownContent(task, markdown)
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	return domain.ExportResult{Name: e.Name(), Status: "success", Target: target}, nil
}

func buildPortableMarkdownContent(task *domain.Task, markdown string) string {
	return buildMetadataMarkdownContent(task, markdown, false)
}
