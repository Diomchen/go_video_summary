package service

import (
	"context"

	"go_subtitle_whisper/internal/metadata"
)

type Transcriber interface {
	TranscribeFile(ctx context.Context, filename string, data []byte, language string) (string, error)
}

type ProgressUpdate struct {
	Percent float64
	Message string
}

type ProgressTranscriber interface {
	TranscribeFileWithProgress(ctx context.Context, filename string, data []byte, language string, onProgress func(ProgressUpdate)) (string, error)
}

type Translator interface {
	Translate(ctx context.Context, input, sourceLanguage string) (string, error)
}

type SummaryOptions struct {
	Title           string
	SourceURL       string
	BVID            string
	CollectionName  string
	CollectionIndex int
	AuthorName      string
}

type Summarizer interface {
	Summarize(ctx context.Context, transcript string, options SummaryOptions) (summary string, meta metadata.SummaryMetadata, err error)
}
