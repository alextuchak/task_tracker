-- +goose Up
ALTER TABLE task_comments
    ADD COLUMN updated_at TIMESTAMP NOT NULL
        DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP AFTER created_at,
    ADD INDEX idx_task_comments_task_id (task_id, id),
    DROP INDEX idx_task_comments_task_time;

UPDATE task_comments SET updated_at = created_at;

-- +goose Down
ALTER TABLE task_comments
    ADD INDEX idx_task_comments_task_time (task_id, created_at),
    DROP INDEX idx_task_comments_task_id,
    DROP COLUMN updated_at;
