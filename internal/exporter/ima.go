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

type IMAExporter struct {
	clientID string
	apiKey   string
	folderID string
	baseURL  string
	client   *http.Client
}

func NewIMAExporter(clientID, apiKey, folderID string) *IMAExporter {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(apiKey) == "" {
		return nil
	}
	return &IMAExporter{
		clientID: strings.TrimSpace(clientID),
		apiKey:   strings.TrimSpace(apiKey),
		folderID: strings.TrimSpace(folderID),
		baseURL:  "https://ima.qq.com/openapi/note/v1",
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (e *IMAExporter) Name() string {
	return "ima"
}

func (e *IMAExporter) ExportMarkdown(ctx context.Context, task *domain.Task, _ string, markdown string) (domain.ExportResult, error) {
	content := strings.TrimSpace(markdown)
	if content == "" {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, fmt.Errorf("ima markdown content is empty")
	}

	content = buildIMAMetadataPrefix(task) + content

	body := map[string]any{
		"content_format": 1,
		"content":        content,
	}
	if e.folderID != "" {
		body["folder_id"] = e.folderID
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/import_doc", bytes.NewReader(payload))
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	req.Header.Set("ima-openapi-clientid", e.clientID)
	req.Header.Set("ima-openapi-apikey", e.apiKey)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := e.client.Do(req)
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	if resp.StatusCode >= 300 {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, fmt.Errorf("ima request failed: %s", strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			DocID string `json:"doc_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, err
	}
	if parsed.Code != 0 {
		message := strings.TrimSpace(parsed.Msg)
		if message == "" {
			message = fmt.Sprintf("ima error code %d", parsed.Code)
		}
		return domain.ExportResult{Name: e.Name(), Status: "failed"}, fmt.Errorf("%s", message)
	}

	target := strings.TrimSpace(parsed.Data.DocID)
	return domain.ExportResult{Name: e.Name(), Status: "success", Target: target}, nil
}

func buildIMAMetadataPrefix(task *domain.Task) string {
	var lines []string
	if upName := firstNonEmpty(task.UPName, task.AuthorName); upName != "" {
		lines = append(lines, "**UP主：** "+upName)
	}
	if sourceLink := firstNonEmpty(task.SourceLink, task.SourceURL); sourceLink != "" {
		lines = append(lines, fmt.Sprintf("**视频链接：** [%s](%s)", sourceLink, sourceLink))
	}
	if task.CollectionName != "" {
		coll := task.CollectionName
		if task.CollectionIndex > 0 {
			coll = fmt.Sprintf("%s（第 %d 集）", coll, task.CollectionIndex)
		}
		lines = append(lines, "**合集：** "+coll)
	}
	if task.CollectionURL != "" {
		lines = append(lines, fmt.Sprintf("**合集链接：** [%s](%s)", task.CollectionURL, task.CollectionURL))
	}
	if strings.TrimSpace(task.Domain) != "" {
		lines = append(lines, "**领域：** "+task.Domain)
	}
	tags := task.Tags
	if len(tags) == 0 {
		tags = task.DomainTags
	}
	if len(tags) > 0 {
		lines = append(lines, "**标签：** "+strings.Join(tags, " | "))
	}
	if len(lines) == 0 {
		return ""
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n\n"
}
