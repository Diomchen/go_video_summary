package exporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go_subtitle_whisper/internal/domain"
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

func (e *ObsidianExporter) ExportMarkdown(_ context.Context, task *domain.Task, markdownPath string, markdown string) (domain.ExportResult, error) {
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

	targetDir := filepath.Join(rootDir, safePathPart(exportTask.Domain))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}

	filename := filepath.Base(markdownPath)
	if strings.TrimSpace(filename) == "" || filename == "." {
		filename = safePathPart(firstNonEmpty(exportTask.Title, exportTask.Name))
		if filename == "" {
			filename = fmt.Sprintf("%s.summary", safeSlug(exportTask.Name))
		}
		filename += ".md"
	}
	target := filepath.Join(targetDir, filename)
	target = uniquePath(target)

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

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"<", " ", ">", " ", ":", " ", `"`, " ", "/", " ", "\\", " ",
		"|", " ", "?", " ", "*", " ", "\r", " ", "\n", " ", "\t", " ",
	)
	value = replacer.Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Trim(value, ". ")
	if value == "" {
		return ""
	}
	if len([]rune(value)) > 80 {
		runes := []rune(value)
		value = string(runes[:80])
	}
	return value
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for idx := 2; ; idx++ {
		candidate := fmt.Sprintf("%s-%d%s", base, idx, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
