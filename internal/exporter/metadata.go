package exporter

import (
	"fmt"
	"strings"

	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/source"
)

func buildMetadataMarkdownContent(task *domain.Task, markdown string, obsidianLinks bool) string {
	var b strings.Builder
	b.WriteString("---\n")
	if strings.TrimSpace(task.Name) != "" {
		fmt.Fprintf(&b, "title: %q\n", task.Name)
	}
	if strings.TrimSpace(task.AuthorName) != "" {
		fmt.Fprintf(&b, "author: %q\n", task.AuthorName)
		fmt.Fprintf(&b, "up: %q\n", task.AuthorName)
	}
	if strings.TrimSpace(task.SourceURL) != "" {
		fmt.Fprintf(&b, "source_url: %q\n", task.SourceURL)
		if bvid := source.ExtractBVID(task.SourceURL); bvid != "" {
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
	if len(task.DomainTags) > 0 {
		b.WriteString("domain_tags:\n")
		for _, tag := range task.DomainTags {
			if strings.TrimSpace(tag) != "" {
				fmt.Fprintf(&b, "  - %q\n", tag)
			}
		}
	}
	tags := obsidianTags(task)
	if len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, tag := range tags {
			fmt.Fprintf(&b, "  - %q\n", tag)
		}
	}
	b.WriteString("---\n\n")
	if obsidianLinks {
		b.WriteString(buildObsidianRelations(task))
	}
	b.WriteString(markdown)
	return b.String()
}

func obsidianTags(task *domain.Task) []string {
	var tags []string
	seen := make(map[string]struct{})
	add := func(tag string) {
		tag = strings.Trim(strings.TrimSpace(tag), "#")
		if tag == "" {
			return
		}
		tag = strings.ReplaceAll(tag, " ", "-")
		if _, ok := seen[tag]; ok {
			return
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}

	add("bilibili")
	if task.AuthorName != "" {
		add("up/" + task.AuthorName)
	}
	for _, tag := range task.DomainTags {
		add("domain/" + tag)
	}
	if task.CollectionName != "" {
		add("collection/" + task.CollectionName)
	}
	return tags
}

func buildObsidianRelations(task *domain.Task) string {
	var lines []string
	if strings.TrimSpace(task.AuthorName) != "" {
		lines = append(lines, fmt.Sprintf("- UP主：[[UP/%s]]", task.AuthorName))
	}
	if len(task.DomainTags) > 0 {
		var links []string
		for _, tag := range task.DomainTags {
			if strings.TrimSpace(tag) != "" {
				links = append(links, fmt.Sprintf("[[领域/%s]]", tag))
			}
		}
		if len(links) > 0 {
			lines = append(lines, "- 领域："+strings.Join(links, " "))
		}
	}
	if strings.TrimSpace(task.CollectionName) != "" {
		lines = append(lines, fmt.Sprintf("- 合集：[[合集/%s]]", task.CollectionName))
	}
	if len(lines) == 0 {
		return ""
	}
	return "## 关联\n\n" + strings.Join(lines, "\n") + "\n\n"
}
