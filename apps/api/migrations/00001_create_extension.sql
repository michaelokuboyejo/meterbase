-- +goose Up
-- The timescaledb image preloads the library; the extension still must be created.
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- +goose Down
DROP EXTENSION IF EXISTS timescaledb;
