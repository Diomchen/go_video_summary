package exporter

import (
	"fmt"
	"strings"

	"go_subtitle_whisper/internal/domain"
	"go_subtitle_whisper/internal/source"
)

func buildMetadataMarkdownContent(task *domain.Task, markdown string, obsidianLinks bool) string {
	title := firstNonEmpty(task.Title, task.Name)
	upName := firstNonEmpty(task.UPName, task.AuthorName)
	sourceLink := firstNonEmpty(task.SourceLink, task.SourceURL)
	domainName := firstNonEmpty(task.Domain)
	tags := task.Tags
	if len(tags) == 0 {
		tags = task.DomainTags
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
			if strings.TrimSpace(tag) != "" {
				fmt.Fprintf(&b, "  - %q\n", tag)
			}
		}
	}
	if len(task.DomainTags) > 0 && task.Domain == "" && len(task.Tags) == 0 {
		b.WriteString("domain_tags:\n")
		for _, tag := range task.DomainTags {
			if strings.TrimSpace(tag) != "" {
				fmt.Fprintf(&b, "  - %q\n", tag)
			}
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
	if upName := firstNonEmpty(task.UPName, task.AuthorName); upName != "" {
		add("up/" + upName)
	}
	if domainName := firstNonEmpty(task.Domain); domainName != "" {
		add("domain/" + domainName)
	} else {
		for _, tag := range task.DomainTags {
			add("domain/" + tag)
		}
	}
	if task.CollectionName != "" {
		add("collection/" + task.CollectionName)
	}
	return tags
}

func buildObsidianRelations(task *domain.Task) string {
	var lines []string
	if upName := firstNonEmpty(task.UPName, task.AuthorName); upName != "" {
		lines = append(lines, fmt.Sprintf("- UP主：[[UP/%s]]", upName))
	}
	if domainName := firstNonEmpty(task.Domain); domainName != "" {
		lines = append(lines, fmt.Sprintf("- 领域：[[%s]]", domainName))
	} else if len(task.DomainTags) > 0 {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
