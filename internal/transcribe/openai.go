package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"go_subtitle_whisper/internal/service"
)

type OpenAITranscriber struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenAITranscriber(baseURL, apiKey, model string, timeout time.Duration) *OpenAITranscriber {
	return &OpenAITranscriber{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (t *OpenAITranscriber) TranscribeFile(ctx context.Context, filename string, data []byte, language string) (string, error) {
	return t.TranscribeFileWithProgress(ctx, filename, data, language, nil)
}

func (t *OpenAITranscriber) TranscribeFileWithProgress(ctx context.Context, filename string, data []byte, language string, onProgress func(service.ProgressUpdate)) (string, error) {
	if onProgress != nil {
		onProgress(service.ProgressUpdate{Percent: 5, Message: "uploading"})
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fileWriter, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", err
	}
	if _, err := fileWriter.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("model", t.model); err != nil {
		return "", err
	}
	if language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return "", err
		}
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("whisper request failed: %s", strings.TrimSpace(string(respData)))
	}

	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respData, &parsed); err != nil {
		return "", err
	}
	if onProgress != nil {
		onProgress(service.ProgressUpdate{Percent: 100, Message: "transcribing"})
	}
	return strings.TrimSpace(parsed.Text), nil
}
