-- +goose Up
ALTER TABLE instance ALTER COLUMN registration_open SET DEFAULT false;
UPDATE instance SET registration_open = false WHERE NOT EXISTS (SELECT 1 FROM users);

-- +goose Down
ALTER TABLE instance ALTER COLUMN registration_open SET DEFAULT true;
