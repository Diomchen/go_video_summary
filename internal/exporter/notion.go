package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go_subtitle_whisper/internal/domain"
)

type NotionExporter struct {
	token      string
	version    string
	parentPage string
	httpClient *http.Client
}

func NewNotionExporter(token, version, parentPage string) *NotionExporter {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(parentPage) == "" {
		return nil
	}
	if strings.TrimSpace(version) == "" {
		version = "2022-06-28"
	}
	return &NotionExporter{
		token:      token,
		version:    version,
		parentPage: parentPage,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (e *NotionExporter) Name() string {
	return "notion"
}

func (e *NotionExporter) ExportMarkdown(ctx context.Context, task *domain.Task, _ string, markdown string) (domain.ExportResult, error) {
	title := strings.TrimSpace(task.Name)
	if title == "" {
		title = task.ID
	}

	children := buildNotionMetadataBlocks(task)
	children = append(children, markdownToNotionBlocks(markdown)...)

	body := map[string]any{
		"parent": map[string]any{
			"type":    "page_id",
			"page_id": e.parentPage,
		},
		"properties": map[string]any{
			"title": map[string]any{
				"title": []map[string]any{
					{
						"type": "text",
						"text": map[string]string{"content": title},
					},
				},
			},
		},
		"children": children,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.notion.com/v1/pages", bytes.NewReader(payload))
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Notion-Version", e.version)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	if resp.StatusCode >= 300 {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, fmt.Errorf("notion request failed: %s", strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	target := parsed.URL
	if target == "" {
		target = parsed.ID
	}
	return domain.ExportResult{Name: e.Name(), Status: "success", Target: target}, nil
}

func markdownToNotionBlocks(markdown string) []map[string]any {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	blocks := make([]map[string]any, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		blockType := "paragraph"
		text := line
		switch {
		case strings.HasPrefix(line, "# "):
			blockType = "heading_1"
			text = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		case strings.HasPrefix(line, "## "):
			blockType = "heading_2"
			text = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		case strings.HasPrefix(line, "### "):
			blockType = "heading_3"
			text = strings.TrimSpace(strings.TrimPrefix(line, "### "))
		case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "):
			blockType = "bulleted_list_item"
			text = strings.TrimSpace(line[2:])
		case orderedListLine(line):
			blockType = "numbered_list_item"
			text = strings.TrimSpace(line[3:])
		}

		for _, chunk := range splitRunes(text, 1800) {
			blocks = append(blocks, map[string]any{
				"object": "block",
				"type":   blockType,
				blockType: map[string]any{
					"rich_text": []map[string]any{
						{
							"type": "text",
							"text": map[string]string{"content": chunk},
						},
					},
				},
			})
			if len(blocks) >= 100 {
				return blocks
			}
		}
	}
	return blocks
}

func orderedListLine(line string) bool {
	return len(line) > 3 && line[0] >= '0' && line[0] <= '9' && line[1] == '.' && line[2] == ' '
}

func buildNotionMetadataBlocks(task *domain.Task) []map[string]any {
	var lines []string
	if task.AuthorName != "" {
		lines = append(lines, "UP主："+task.AuthorName)
	}
	if task.SourceURL != "" {
		lines = append(lines, "视频链接："+task.SourceURL)
	}
	if task.CollectionName != "" {
		coll := task.CollectionName
		if task.CollectionIndex > 0 {
			coll = fmt.Sprintf("%s（第 %d 集）", coll, task.CollectionIndex)
		}
		lines = append(lines, "合集："+coll)
	}
	if task.CollectionURL != "" {
		lines = append(lines, "合集链接："+task.CollectionURL)
	}
	if len(task.DomainTags) > 0 {
		lines = append(lines, "领域标签："+strings.Join(task.DomainTags, " | "))
	}

	if len(lines) == 0 {
		return nil
	}

	blocks := make([]map[string]any, 0, len(lines)+1)
	blocks = append(blocks, map[string]any{
		"object": "block",
		"type":   "divider",
		"divider": map[string]any{},
	})
	for _, line := range lines {
		blocks = append(blocks, map[string]any{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]any{
				"rich_text": []map[string]any{
					{
						"type": "text",
						"text": map[string]string{"content": line},
					},
				},
			},
		})
	}
	blocks = append(blocks, map[string]any{
		"object": "block",
		"type":   "divider",
		"divider": map[string]any{},
	})
	return blocks
}
