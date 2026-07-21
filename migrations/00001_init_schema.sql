-- +goose Up
CREATE TABLE users (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email         VARCHAR(255)    NOT NULL,
    password_hash VARCHAR(255)    NOT NULL,
    name          VARCHAR(255)    NOT NULL,
    created_at    TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_email (email)
) ENGINE = InnoDB;

CREATE TABLE teams (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name       VARCHAR(255)    NOT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_teams_created_by (created_by),
    CONSTRAINT fk_teams_created_by FOREIGN KEY (created_by) REFERENCES users (id)
) ENGINE = InnoDB;

CREATE TABLE team_members (
    team_id   BIGINT UNSIGNED               NOT NULL,
    user_id   BIGINT UNSIGNED               NOT NULL,
    role      ENUM ('owner','admin','member') NOT NULL DEFAULT 'member',
    joined_at TIMESTAMP                     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (team_id, user_id),
    KEY idx_team_members_user (user_id),
    CONSTRAINT fk_team_members_team FOREIGN KEY (team_id) REFERENCES teams (id),
    CONSTRAINT fk_team_members_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE = InnoDB;

CREATE TABLE tasks (
    id           BIGINT UNSIGNED                    NOT NULL AUTO_INCREMENT,
    team_id      BIGINT UNSIGNED                    NOT NULL,
    title        VARCHAR(500)                       NOT NULL,
    description  TEXT                               NULL,
    status       ENUM ('todo','in_progress','done') NOT NULL DEFAULT 'todo',
    assignee_id  BIGINT UNSIGNED                    NULL,
    created_by   BIGINT UNSIGNED                    NOT NULL,
    created_at   TIMESTAMP                          NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP                          NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    completed_at TIMESTAMP                          NULL,
    PRIMARY KEY (id),
    KEY idx_tasks_team_status_updated (team_id, status, updated_at),
    KEY idx_tasks_assignee (assignee_id),
    KEY idx_tasks_creator_created (created_by, created_at),
    CONSTRAINT fk_tasks_team FOREIGN KEY (team_id) REFERENCES teams (id),
    CONSTRAINT fk_tasks_assignee FOREIGN KEY (assignee_id) REFERENCES users (id),
    CONSTRAINT fk_tasks_created_by FOREIGN KEY (created_by) REFERENCES users (id)
) ENGINE = InnoDB;

CREATE TABLE task_history (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    task_id    BIGINT UNSIGNED NOT NULL,
    changed_by BIGINT UNSIGNED NOT NULL,
    field      VARCHAR(64)     NOT NULL,
    old_value  TEXT            NULL,
    new_value  TEXT            NULL,
    changed_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_task_history_task_time (task_id, changed_at),
    KEY idx_task_history_changed_by (changed_by),
    CONSTRAINT fk_task_history_task FOREIGN KEY (task_id) REFERENCES tasks (id),
    CONSTRAINT fk_task_history_user FOREIGN KEY (changed_by) REFERENCES users (id)
) ENGINE = InnoDB;

CREATE TABLE task_comments (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    task_id    BIGINT UNSIGNED NOT NULL,
    user_id    BIGINT UNSIGNED NOT NULL,
    body       TEXT            NOT NULL,
    created_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_task_comments_task_time (task_id, created_at),
    KEY idx_task_comments_user (user_id),
    CONSTRAINT fk_task_comments_task FOREIGN KEY (task_id) REFERENCES tasks (id),
    CONSTRAINT fk_task_comments_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE = InnoDB;

-- +goose Down
DROP TABLE task_comments;
DROP TABLE task_history;
DROP TABLE tasks;
DROP TABLE team_members;
DROP TABLE teams;
DROP TABLE users;
