-- +goose Up
CREATE TABLE user_notification_settings_versions (
    user_id bigint PRIMARY KEY REFERENCES users ON DELETE CASCADE,
    version bigint NOT NULL CHECK (version > 0)
);

-- +goose Down
DROP TABLE user_notification_settings_versions;
