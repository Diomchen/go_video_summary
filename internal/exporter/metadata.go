package exporter

import (
	"fmt"
	"strings"

	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/metadata"
	"go_subtitle_whisper/internal/source"
)

func buildMetadataMarkdownContent(task *domain.Task, markdown string, obsidianLinks bool) string {
	title := firstNonEmpty(task.Title, task.Name)
	upName := firstNonEmpty(task.UPName, task.AuthorName)
	sourceLink := firstNonEmpty(task.SourceLink, task.SourceURL)
	domainName := metadata.CleanLabel(firstNonEmpty(task.Domain))
	tags := cleanMetadataLabels(task.Tags)
	if len(tags) == 0 {
		tags = cleanMetadataLabels(task.DomainTags)
	}

	var b strings.Builder
	b.WriteString("---\n")
	if title != "" {
		fmt.Fprintf(&b, "title: %q\n", title)
	}
	if upName != "" {
		fmt.Fprintf(&b, "up_name: %q\n", upName)
		fmt.Fprintf(&b, "author: %q\n", upName)
		fmt.Fprintf(&b, "up: %q\n", upName)
	}
	if sourceLink != "" {
		fmt.Fprintf(&b, "source_link: %q\n", sourceLink)
		fmt.Fprintf(&b, "source_url: %q\n", sourceLink)
		if bvid := source.ExtractBVID(sourceLink); bvid != "" {
			fmt.Fprintf(&b, "bvid: %q\n", bvid)
		}
	}
	if strings.TrimSpace(task.CollectionName) != "" {
		fmt.Fprintf(&b, "collection: %q\n", task.CollectionName)
	}
	if task.CollectionIndex > 0 {
		fmt.Fprintf(&b, "collection_index: %d\n", task.CollectionIndex)
	}
	if strings.TrimSpace(task.CollectionURL) != "" {
		fmt.Fprintf(&b, "collection_url: %q\n", task.CollectionURL)
	}
	if domainName != "" {
		fmt.Fprintf(&b, "domain: %q\n", domainName)
	}
	if len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, tag := range tags {
			fmt.Fprintf(&b, "  - %q\n", tag)
		}
	}
	if len(task.DomainTags) > 0 && task.Domain == "" && len(task.Tags) == 0 {
		domainTags := cleanMetadataLabels(task.DomainTags)
		if len(domainTags) > 0 {
			b.WriteString("domain_tags:\n")
			for _, tag := range domainTags {
				fmt.Fprintf(&b, "  - %q\n", tag)
			}
		}
	}
	b.WriteString("---\n\n")
	if obsidianLinks {
		b.WriteString(buildObsidianRelationsV2(task))
	}
	b.WriteString(markdown)
	return b.String()
}

func cleanMetadataLabels(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = metadata.CleanLabel(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func buildObsidianRelationsV2(task *domain.Task) string {
	var lines []string
	if upName := firstNonEmpty(task.UPName, task.AuthorName); upName != "" {
		lines = append(lines, fmt.Sprintf("- UP主：[[UP/%s]]", upName))
	}
	if domainName := metadata.CleanLabel(firstNonEmpty(task.Domain)); domainName != "" {
		lines = append(lines, fmt.Sprintf("- 领域：[[%s]]", domainName))
	} else if domainTags := cleanMetadataLabels(task.DomainTags); len(domainTags) > 0 {
		links := make([]string, 0, len(domainTags))
		for _, tag := range domainTags {
			links = append(links, fmt.Sprintf("[[%s]]", tag))
		}
		lines = append(lines, "- 领域："+strings.Join(links, " "))
	}

	tags := cleanMetadataLabels(task.Tags)
	if len(tags) == 0 {
		tags = cleanMetadataLabels(task.DomainTags)
	}
	if len(tags) > 0 {
		links := make([]string, 0, len(tags))
		for _, tag := range tags {
			links = append(links, fmt.Sprintf("[[%s]]", tag))
		}
		lines = append(lines, "- 标签："+strings.Join(links, " "))
	}
	if strings.TrimSpace(task.CollectionName) != "" {
		lines = append(lines, fmt.Sprintf("- 合集：[[合集/%s]]", task.CollectionName))
	}
	if len(lines) == 0 {
		return ""
	}
	return "## 关联\n\n" + strings.Join(lines, "\n") + "\n\n"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
