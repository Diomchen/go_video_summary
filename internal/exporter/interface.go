package exporter

import (
	"context"
	"reflect"

	"go_subtitle_whisper/internal/domain"
)

type MarkdownExporter interface {
	Name() string
	ExportMarkdown(ctx context.Context, task *domain.Task, markdownPath string, markdown string) (domain.ExportResult, error)
}

func IsNil(item MarkdownExporter) bool {
	if item == nil {
		return true
	}
	value := reflect.ValueOf(item)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}
