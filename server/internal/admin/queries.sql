-- name: RegistrationOpen :one
SELECT registration_open FROM instance WHERE id;

-- name: SetRegistrationOpen :one
UPDATE instance SET registration_open = @registration_open WHERE id RETURNING registration_open;
