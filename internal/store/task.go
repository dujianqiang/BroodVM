package store

import "time"

type Task struct {
	ID        string
	Type      string
	RefID     string
	Status    string
	Progress  int
	Message   string
	CreatedAt time.Time
}

type TaskStore struct{ db *DB }

func NewTaskStore(db *DB) *TaskStore { return &TaskStore{db: db} }

func (s *TaskStore) Create(t *Task) error {
	_, err := s.db.Exec(
		`INSERT INTO tasks (id,type,ref_id,status,progress,message,created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		t.ID, t.Type, t.RefID, t.Status, t.Progress, t.Message,
		t.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *TaskStore) Get(id string) (*Task, error) {
	row := s.db.QueryRow(
		`SELECT id,type,ref_id,status,progress,message,created_at FROM tasks WHERE id=?`, id)
	var t Task
	var createdAt string
	err := row.Scan(&t.ID, &t.Type, &t.RefID, &t.Status, &t.Progress, &t.Message, &createdAt)
	if err != nil {
		return nil, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &t, nil
}

func (s *TaskStore) Update(id, status string, progress int, message string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET status=?, progress=?, message=? WHERE id=?`,
		status, progress, message, id,
	)
	return err
}
