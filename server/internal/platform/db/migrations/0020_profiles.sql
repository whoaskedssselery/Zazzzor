-- "About me", the chosen status and a custom status line, owned by auth.

-- +goose Up
ALTER TABLE users
    ADD COLUMN bio text NOT NULL DEFAULT '' CHECK (char_length(bio) <= 190),
    -- What the user chose; "invisible" shows as offline to everyone else.
    ADD COLUMN status text NOT NULL DEFAULT 'online'
        CHECK (status IN ('online', 'idle', 'dnd', 'invisible')),
    ADD COLUMN custom_status text CHECK (char_length(custom_status) BETWEEN 1 AND 128);

-- +goose Down
ALTER TABLE users DROP COLUMN custom_status, DROP COLUMN status, DROP COLUMN bio;
