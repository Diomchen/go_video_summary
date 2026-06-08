package metadata

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"go_subtitle_whisper/internal/domain"
)

type SummaryMetadata struct {
	Title      string   `json:"title"`
	SourceLink string   `json:"source_link"`
	UPName     string   `json:"up_name"`
	Domain     string   `json:"domain"`
	Tags       []string `json:"tags"`
}

var (
	commentPattern       = regexp.MustCompile(`(?s)^\s*<!--\s*metadata\s*(\{.*?\})\s*-->\s*`)
	firstHeadingPattern  = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	domainHeadingPattern = regexp.MustCompile(`(?m)^##\s*领域标签\s*\r?\n\s*(.+)$`)
)

func ExtractFromSummary(summary string) (string, SummaryMetadata) {
	clean := strings.TrimSpace(summary)
	if clean == "" {
		return "", SummaryMetadata{}
	}

	if matches := commentPattern.FindStringSubmatch(clean); len(matches) == 2 {
		var meta SummaryMetadata
		if err := json.Unmarshal([]byte(matches[1]), &meta); err == nil {
			clean = strings.TrimSpace(commentPattern.ReplaceAllString(clean, ""))
			return clean, sanitize(meta)
		}
	}

	return clean, extractMarkdownFallback(clean)
}

func MergeTaskFallbacks(meta SummaryMetadata, task *domain.Task) SummaryMetadata {
	if task == nil {
		return sanitize(meta)
	}
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = firstNonEmpty(task.Title, task.Name)
	}
	if strings.TrimSpace(meta.SourceLink) == "" {
		meta.SourceLink = firstNonEmpty(task.SourceLink, task.SourceURL)
	}
	if strings.TrimSpace(meta.UPName) == "" {
		meta.UPName = firstNonEmpty(task.UPName, task.AuthorName)
	}
	if strings.TrimSpace(meta.Domain) == "" {
		meta.Domain = firstNonEmpty(task.Domain)
		if strings.TrimSpace(meta.Domain) == "" && len(task.DomainTags) > 0 {
			meta.Domain = task.DomainTags[0]
		}
	}
	if len(meta.Tags) == 0 && len(task.Tags) > 0 {
		meta.Tags = append([]string(nil), task.Tags...)
	}
	return sanitize(meta)
}

func extractMarkdownFallback(summary string) SummaryMetadata {
	var meta SummaryMetadata
	if matches := firstHeadingPattern.FindStringSubmatch(summary); len(matches) == 2 {
		meta.Title = matches[1]
	}
	if matches := domainHeadingPattern.FindStringSubmatch(summary); len(matches) == 2 {
		parts := splitPiped(matches[1])
		if len(parts) > 0 {
			meta.Domain = parts[0]
		}
		if len(parts) > 1 {
			meta.Tags = parts[1:]
		}
	}
	return sanitize(meta)
}

func splitPiped(value string) []string {
	raw := strings.Split(value, "|")
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{})
	for _, item := range raw {
		item = CleanLabel(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func sanitize(meta SummaryMetadata) SummaryMetadata {
	meta.Title = strings.TrimSpace(meta.Title)
	meta.SourceLink = strings.TrimSpace(meta.SourceLink)
	meta.UPName = strings.TrimSpace(meta.UPName)
	meta.Domain = CleanLabel(meta.Domain)
	meta.Tags = cleanList(meta.Tags)
	return meta
}

func CleanLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = CleanLabel(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
