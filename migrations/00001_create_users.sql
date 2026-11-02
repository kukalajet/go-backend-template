-- +goose Up
CREATE TABLE users (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    email        text NOT NULL,
    display_name text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_unique UNIQUE (email)
);

-- +goose Down
DROP TABLE users;
