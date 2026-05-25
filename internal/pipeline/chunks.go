package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_subtitle_whisper/internal/domain"
)

const (
	chunkStatusPending = "pending"
	chunkStatusRunning = "running"
	chunkStatusDone    = "done"
	chunkStatusFailed  = "failed"
)

type chunkCheckpoint struct {
	Index     int       `json:"index"`
	Start     string    `json:"start"`
	End       string    `json:"end"`
	Status    string    `json:"status"`
	Text      string    `json:"text,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func chunkCheckpointDirectory(task *domain.Task) string {
	if task == nil || strings.TrimSpace(task.CheckpointDir) == "" {
		return ""
	}
	return filepath.Join(task.CheckpointDir, "chunks")
}

func chunkCheckpointFile(task *domain.Task, idx int) string {
	dir := chunkCheckpointDirectory(task)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("chunk-%04d.json", idx))
}

func loadChunkCheckpoints(task *domain.Task) (map[int]chunkCheckpoint, error) {
	checkpoints := make(map[int]chunkCheckpoint)
	dir := chunkCheckpointDirectory(task)
	if dir == "" {
		return checkpoints, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return checkpoints, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var state chunkCheckpoint
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
		checkpoints[state.Index] = state
	}
	return checkpoints, nil
}

func saveChunkCheckpoint(task *domain.Task, state chunkCheckpoint) error {
	path := chunkCheckpointFile(task, state.Index)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := jsonMarshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func migrateLegacyChunkCheckpoints(task *domain.Task, totalChunks int, chunkSeconds int, segmentsPath string) (map[int]chunkCheckpoint, error) {
	checkpoints := make(map[int]chunkCheckpoint)
	if task == nil || totalChunks <= 0 {
		return checkpoints, nil
	}

	legacyDone := task.CompletedChunks
	if legacyDone <= 0 {
		return checkpoints, nil
	}
	if legacyDone > totalChunks {
		legacyDone = totalChunks
	}

	now := time.Now()
	for idx := 0; idx < legacyDone; idx++ {
		checkpoints[idx] = chunkCheckpoint{
			Index:     idx,
			Start:     formatSeconds(idx * chunkSeconds),
			End:       formatSeconds((idx + 1) * chunkSeconds),
			Status:    chunkStatusDone,
			UpdatedAt: now,
		}
	}

	data, err := os.ReadFile(segmentsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return checkpoints, nil
		}
		return nil, err
	}

	var segments []domain.Segment
	if err := json.Unmarshal(data, &segments); err != nil {
		return nil, err
	}

	for _, segment := range segments {
		state, ok := checkpoints[segment.Index]
		if !ok {
			state = chunkCheckpoint{
				Index:  segment.Index,
				Status: chunkStatusDone,
			}
		}
		if strings.TrimSpace(segment.Start) != "" {
			state.Start = segment.Start
		}
		if strings.TrimSpace(segment.End) != "" {
			state.End = segment.End
		}
		state.Text = strings.TrimSpace(segment.Text)
		if segment.CreatedAt.IsZero() {
			state.UpdatedAt = now
		} else {
			state.UpdatedAt = segment.CreatedAt
		}
		checkpoints[segment.Index] = state
	}

	return checkpoints, nil
}

func aggregateChunkResults(task *domain.Task, checkpoints map[int]chunkCheckpoint, totalChunks int, chunkSeconds int) (string, []domain.Segment, int) {
	if totalChunks <= 0 {
		return "", nil, 0
	}

	segments := make([]domain.Segment, 0, totalChunks)
	var transcriptBuilder strings.Builder
	doneCount := 0

	for idx := 0; idx < totalChunks; idx++ {
		state, ok := checkpoints[idx]
		if !ok || state.Status != chunkStatusDone {
			continue
		}
		doneCount++

		text := strings.TrimSpace(state.Text)
		if text == "" {
			continue
		}
		if transcriptBuilder.Len() > 0 {
			transcriptBuilder.WriteByte('\n')
		}
		transcriptBuilder.WriteString(text)

		start := strings.TrimSpace(state.Start)
		if start == "" {
			start = formatSeconds(idx * chunkSeconds)
		}
		end := strings.TrimSpace(state.End)
		if end == "" {
			end = formatSeconds((idx + 1) * chunkSeconds)
		}
		createdAt := state.UpdatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		segments = append(segments, domain.Segment{
			Index:     idx,
			Start:     start,
			End:       end,
			Text:      text,
			CreatedAt: createdAt,
			Source:    task.Mode,
		})
	}

	return strings.TrimSpace(transcriptBuilder.String()), segments, doneCount
}

func persistAggregatedTranscription(task *domain.Task, transcriptPath, segmentsPath, transcript string, segments []domain.Segment) error {
	if task == nil || strings.TrimSpace(task.CheckpointDir) == "" {
		return nil
	}
	if err := os.WriteFile(transcriptPath, []byte(strings.TrimSpace(transcript)), 0o644); err != nil {
		return err
	}
	encoded, err := jsonMarshal(segments)
	if err != nil {
		return err
	}
	return os.WriteFile(segmentsPath, encoded, 0o644)
}
