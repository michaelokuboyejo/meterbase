-- +goose Up
CREATE TABLE customers (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       UUID NOT NULL REFERENCES organizations(id),
  external_id  TEXT NOT NULL,
  name         TEXT,
  metadata     JSONB NOT NULL DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, external_id)
);

-- +goose Down
DROP TABLE IF EXISTS customers;
