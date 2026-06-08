package exporter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/filenames"
	"go_subtitle_whisper/internal/knowledge"
	metapkg "go_subtitle_whisper/internal/metadata"
)

type ObsidianExporter struct {
	vaultDir            string
	subdir              string
	domainIndexFile     string
	tagIndexFile        string
	similarityThreshold float64
}

func NewObsidianExporter(vaultDir, subdir string) *ObsidianExporter {
	if strings.TrimSpace(vaultDir) == "" {
		return nil
	}
	return &ObsidianExporter{
		vaultDir:            vaultDir,
		subdir:              subdir,
		domainIndexFile:     "领域索引.md",
		tagIndexFile:        "标签索引.md",
		similarityThreshold: 0.82,
	}
}

func NewObsidianExporterWithIndex(vaultDir, subdir, domainIndexFile, tagIndexFile string, similarityThreshold float64) *ObsidianExporter {
	exporter := NewObsidianExporter(vaultDir, subdir)
	if exporter == nil {
		return nil
	}
	if strings.TrimSpace(domainIndexFile) != "" {
		exporter.domainIndexFile = strings.TrimSpace(domainIndexFile)
	}
	if strings.TrimSpace(tagIndexFile) != "" {
		exporter.tagIndexFile = strings.TrimSpace(tagIndexFile)
	}
	if similarityThreshold > 0 {
		exporter.similarityThreshold = similarityThreshold
	}
	return exporter
}

func (e *ObsidianExporter) Name() string {
	return "obsidian"
}

func (e *ObsidianExporter) ExportMarkdown(_ context.Context, task *domain.Task, _ string, markdown string) (domain.ExportResult, error) {
	rootDir := e.vaultDir
	if strings.TrimSpace(e.subdir) != "" {
		rootDir = filepath.Join(rootDir, e.subdir)
	}

	exportTask := cloneExportTask(task)
	meta := metapkg.MergeTaskFallbacks(metapkg.SummaryMetadata{
		Title:      exportTask.Title,
		SourceLink: exportTask.SourceLink,
		UPName:     exportTask.UPName,
		Domain:     exportTask.Domain,
		Tags:       append([]string(nil), exportTask.Tags...),
	}, exportTask)
	index := knowledge.NewObsidianIndex(rootDir, e.domainIndexFile, e.tagIndexFile, e.similarityThreshold)
	normalized, err := index.Normalize(meta)
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	exportTask.Title = normalized.Title
	exportTask.SourceLink = normalized.SourceLink
	exportTask.UPName = normalized.UPName
	exportTask.Domain = normalized.Domain
	exportTask.Tags = append([]string(nil), normalized.Tags...)
	exportTask.DomainTags = nil

	targetDir := filepath.Join(rootDir, filenames.SafeDir(exportTask.Domain))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}

	target := filepath.Join(targetDir, filenames.WithSuffix(exportTask, time.Now(), ".md"))
	target = filenames.UniquePath(target)

	content := buildObsidianContent(exportTask, markdown)
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	return domain.ExportResult{Name: e.Name(), Status: "success", Target: target}, nil
}

func buildObsidianContent(task *domain.Task, markdown string) string {
	return buildMetadataMarkdownContent(task, markdown, true)
}

func cloneExportTask(task *domain.Task) *domain.Task {
	if task == nil {
		return &domain.Task{}
	}
	cloned := *task
	cloned.DomainTags = append([]string(nil), task.DomainTags...)
	cloned.Tags = append([]string(nil), task.Tags...)
	return &cloned
}
