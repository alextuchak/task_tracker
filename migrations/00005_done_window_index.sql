-- +goose Up
ALTER TABLE tasks
    ADD INDEX idx_tasks_done_window (status, completed_at, team_id);

-- +goose Down
ALTER TABLE tasks
    DROP INDEX idx_tasks_done_window;
