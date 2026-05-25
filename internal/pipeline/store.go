package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"go_subtitle_whisper/internal/domain"
)

type TaskStore struct {
	root string
}

func NewTaskStore(root string) *TaskStore {
	return &TaskStore{root: root}
}

func (s *TaskStore) Root() string {
	return s.root
}

func (s *TaskStore) Ensure() error {
	return os.MkdirAll(s.root, 0o755)
}

func (s *TaskStore) TaskDir(id string) string {
	return filepath.Join(s.root, id)
}

func (s *TaskStore) TaskJSON(id string) string {
	return filepath.Join(s.TaskDir(id), "task.json")
}

func (s *TaskStore) InputPath(id, filename string) string {
	return filepath.Join(s.TaskDir(id), "input"+filepath.Ext(filename))
}

func (s *TaskStore) SaveTask(task *domain.Task) error {
	if err := os.MkdirAll(s.TaskDir(task.ID), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.TaskJSON(task.ID), data, 0o644)
}

func (s *TaskStore) LoadAll() ([]*domain.Task, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var tasks []*domain.Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, entry.Name(), "task.json"))
		if err != nil {
			continue
		}
		var task domain.Task
		if err := json.Unmarshal(data, &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}
	return tasks, nil
}

func (s *TaskStore) RemoveTask(id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	if err := os.RemoveAll(s.TaskDir(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
