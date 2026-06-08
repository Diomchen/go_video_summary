package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go_subtitle_whisper/internal/domain"
)

func TestFailTaskKeepsInputPathForRetry(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.wav")
	if err := os.WriteFile(inputPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newRetryTestManager(t, dir)
	manager.tasks["task123"] = &domain.Task{
		ID:            "task123",
		Status:        domain.TaskRunning,
		CheckpointDir: dir,
		InputFilePath: inputPath,
		UpdatedAt:     time.Now(),
	}

	manager.failTask("task123", errors.New("transcribe failed"))

	task, _ := manager.GetTask("task123")
	if task.InputFilePath != inputPath {
		t.Fatalf("input path = %q, want %q", task.InputFilePath, inputPath)
	}
}

func TestRetryTaskRequeuesFailedTask(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.wav")
	if err := os.WriteFile(inputPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newRetryTestManager(t, dir)
	manager.tasks["task123"] = &domain.Task{
		ID:               "task123",
		Name:             "Retry me",
		Mode:             "file",
		Status:           domain.TaskFailed,
		Stage:            "failed",
		ProgressPercent:  42,
		Error:            "transcribe failed",
		SummaryError:     "summary failed",
		CheckpointDir:    dir,
		InputFilePath:    inputPath,
		OriginalFileName: "input.wav",
		UpdatedAt:        time.Now(),
	}

	task, err := manager.RetryTask("task123")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskQueued || task.Stage != "queued" || task.ProgressPercent != 0 {
		t.Fatalf("task was not reset for retry: %+v", task)
	}
	if task.Error != "" || task.SummaryError != "" {
		t.Fatalf("errors were not cleared: %+v", task)
	}

	select {
	case job := <-manager.processJobs:
		if job.id != "task123" {
			t.Fatalf("queued job id = %q, want task123", job.id)
		}
	default:
		t.Fatalf("expected retry to enqueue the task")
	}
}

func newRetryTestManager(t *testing.T, dir string) *Manager {
	t.Helper()
	store := NewTaskStore(filepath.Join(dir, "store"))
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	return &Manager{
		events:      NewBroadcaster(),
		store:       store,
		tasks:       make(map[string]*domain.Task),
		processJobs: make(chan taskJob, 1),
	}
}
