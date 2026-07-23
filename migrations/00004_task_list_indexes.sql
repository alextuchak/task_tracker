-- +goose Up
ALTER TABLE tasks
    ADD INDEX idx_tasks_team_id (team_id, id),
    ADD INDEX idx_tasks_team_status_id (team_id, status, id),
    ADD INDEX idx_tasks_created_by (created_by),
    DROP INDEX idx_tasks_team_status_updated,
    DROP INDEX idx_tasks_creator_created;

-- +goose Down
ALTER TABLE tasks
    ADD INDEX idx_tasks_team_status_updated (team_id, status, updated_at),
    ADD INDEX idx_tasks_creator_created (created_by, created_at),
    DROP INDEX idx_tasks_team_id,
    DROP INDEX idx_tasks_team_status_id,
    DROP INDEX idx_tasks_created_by;
