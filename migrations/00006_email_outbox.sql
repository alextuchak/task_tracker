-- +goose Up
CREATE TABLE email_outbox (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    recipient     VARCHAR(255)    NOT NULL,
    subject       VARCHAR(255)    NOT NULL,
    body          TEXT            NOT NULL,
    status        ENUM ('pending','failed') NOT NULL DEFAULT 'pending',
    attempts      INT UNSIGNED    NOT NULL DEFAULT 0,
    next_retry_at DATETIME(3)     NULL,
    last_error    VARCHAR(1024)   NULL,
    created_at    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_outbox_claim (status, next_retry_at, id)
) ENGINE = InnoDB;

-- +goose Down
DROP TABLE email_outbox;
