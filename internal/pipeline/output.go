package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/filenames"
)

func saveTaskOutputs(task *domain.Task, outputDir string, saveAll bool) ([]string, string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return nil, "", nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, "", err
	}

	base := filenames.Base(task, time.Now())
	var saved []string
	summaryPath := ""

	if saveAll && strings.TrimSpace(task.Transcript) != "" {
		path := filenames.UniquePath(filepath.Join(outputDir, base+".transcript.txt"))
		if err := os.WriteFile(path, []byte(task.Transcript), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, path)
	}
	if saveAll && strings.TrimSpace(task.TranslatedText) != "" {
		path := filenames.UniquePath(filepath.Join(outputDir, base+".translated.txt"))
		if err := os.WriteFile(path, []byte(task.TranslatedText), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, path)
	}
	if strings.TrimSpace(task.Summary) != "" {
		summaryPath = filenames.UniquePath(filepath.Join(outputDir, base+".summary.md"))
		summaryContent := buildLocalSummaryContent(task)
		if err := os.WriteFile(summaryPath, []byte(summaryContent), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, summaryPath)
	}

	return saved, summaryPath, nil
}

func buildLocalSummaryContent(task *domain.Task) string {
	var hasMeta bool
	check := func(s string) bool { return strings.TrimSpace(s) != "" }
	title := firstNonEmpty(task.Title, task.Name)
	sourceLink := firstNonEmpty(task.SourceLink, task.SourceURL)
	upName := firstNonEmpty(task.UPName, task.AuthorName)
	domain := firstNonEmpty(task.Domain)
	tags := task.Tags
	if len(tags) == 0 {
		tags = task.DomainTags
	}
	if check(title) || check(upName) || check(sourceLink) || check(task.CollectionName) || check(domain) || len(tags) > 0 {
		hasMeta = true
	}
	if !hasMeta {
		return task.Summary
	}

	var b strings.Builder
	b.WriteString("---\n")
	if check(title) {
		fmt.Fprintf(&b, "title: %q\n", title)
	}
	if check(upName) {
		fmt.Fprintf(&b, "up_name: %q\n", upName)
		fmt.Fprintf(&b, "author: %q\n", upName)
	}
	if check(sourceLink) {
		fmt.Fprintf(&b, "source_link: %q\n", sourceLink)
		fmt.Fprintf(&b, "source_url: %q\n", sourceLink)
	}
	if check(task.CollectionName) {
		fmt.Fprintf(&b, "collection: %q\n", task.CollectionName)
	}
	if task.CollectionIndex > 0 {
		fmt.Fprintf(&b, "collection_index: %d\n", task.CollectionIndex)
	}
	if check(task.CollectionURL) {
		fmt.Fprintf(&b, "collection_url: %q\n", task.CollectionURL)
	}
	if check(domain) {
		fmt.Fprintf(&b, "domain: %q\n", domain)
	}
	if len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, tag := range tags {
			fmt.Fprintf(&b, "  - %q\n", tag)
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(task.Summary)
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
