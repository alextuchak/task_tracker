-- +goose Up
ALTER TABLE task_history
    ADD COLUMN change_group_id CHAR(32) NOT NULL DEFAULT '' AFTER changed_by;

-- +goose Down
ALTER TABLE task_history
    DROP COLUMN change_group_id;
