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
	var front strings.Builder
	front.WriteString("---\n")
	front.WriteString(fmt.Sprintf("title: %q\n", task.Name))
	if task.AuthorName != "" {
		front.WriteString(fmt.Sprintf("author: %q\n", task.AuthorName))
	}
	if task.SourceURL != "" {
		front.WriteString(fmt.Sprintf("source_url: %q\n", task.SourceURL))
	}
	if task.CollectionName != "" {
		front.WriteString(fmt.Sprintf("collection: %q\n", task.CollectionName))
	}
	if task.CollectionIndex > 0 {
		front.WriteString(fmt.Sprintf("collection_index: %d\n", task.CollectionIndex))
	}
	if task.CollectionURL != "" {
		front.WriteString(fmt.Sprintf("collection_url: %q\n", task.CollectionURL))
	}
	if len(task.DomainTags) > 0 {
		front.WriteString("domain_tags:\n")
		for _, tag := range task.DomainTags {
			front.WriteString(fmt.Sprintf("  - %q\n", tag))
		}
	}
	front.WriteString("---\n\n")
	front.WriteString(markdown)
	return front.String()
}
