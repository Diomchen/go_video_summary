package exporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go_subtitle_whisper/internal/domain"
)

type ObsidianExporter struct {
	vaultDir string
	subdir   string
}

func NewObsidianExporter(vaultDir, subdir string) *ObsidianExporter {
	if strings.TrimSpace(vaultDir) == "" {
		return nil
	}
	return &ObsidianExporter{vaultDir: vaultDir, subdir: subdir}
}

func (e *ObsidianExporter) Name() string {
	return "obsidian"
}

func (e *ObsidianExporter) ExportMarkdown(_ context.Context, task *domain.Task, markdownPath string, markdown string) (domain.ExportResult, error) {
	targetDir := e.vaultDir
	if strings.TrimSpace(e.subdir) != "" {
		targetDir = filepath.Join(targetDir, e.subdir)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}

	filename := filepath.Base(markdownPath)
	if strings.TrimSpace(filename) == "" {
		filename = fmt.Sprintf("%s.summary.md", safeSlug(task.Name))
	}
	target := filepath.Join(targetDir, filename)

	content := buildObsidianContent(task, markdown)
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	return domain.ExportResult{Name: e.Name(), Status: "success", Target: target}, nil
}

func buildObsidianContent(task *domain.Task, markdown string) string {
	return buildMetadataMarkdownContent(task, markdown, true)
}
