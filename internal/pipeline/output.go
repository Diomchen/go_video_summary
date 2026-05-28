package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_subtitle_whisper/internal/domain"
)

func saveTaskOutputs(task *domain.Task, outputDir string, saveAll bool) ([]string, string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return nil, "", nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, "", err
	}

	base := fmt.Sprintf("%s-%s-%s", task.ID, slugify(task.Name), time.Now().Format("20060102-150405"))
	var saved []string
	summaryPath := ""

	if saveAll && strings.TrimSpace(task.Transcript) != "" {
		path := filepath.Join(outputDir, base+".transcript.txt")
		if err := os.WriteFile(path, []byte(task.Transcript), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, path)
	}
	if saveAll && strings.TrimSpace(task.TranslatedText) != "" {
		path := filepath.Join(outputDir, base+".translated.txt")
		if err := os.WriteFile(path, []byte(task.TranslatedText), 0o644); err != nil {
			return saved, summaryPath, err
		}
		saved = append(saved, path)
	}
	if strings.TrimSpace(task.Summary) != "" {
		summaryPath = filepath.Join(outputDir, base+".summary.md")
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
	if check(task.AuthorName) || check(task.SourceURL) || check(task.CollectionName) || len(task.DomainTags) > 0 {
		hasMeta = true
	}
	if !hasMeta {
		return task.Summary
	}

	var b strings.Builder
	b.WriteString("---\n")
	if check(task.Name) {
		fmt.Fprintf(&b, "title: %q\n", task.Name)
	}
	if check(task.AuthorName) {
		fmt.Fprintf(&b, "author: %q\n", task.AuthorName)
	}
	if check(task.SourceURL) {
		fmt.Fprintf(&b, "source_url: %q\n", task.SourceURL)
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
	if len(task.DomainTags) > 0 {
		b.WriteString("domain_tags:\n")
		for _, tag := range task.DomainTags {
			fmt.Fprintf(&b, "  - %q\n", tag)
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(task.Summary)
	return b.String()
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "task"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "task"
	}
	return out
}
