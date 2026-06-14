-- +goose Up
CREATE TABLE organizations (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id     UUID NOT NULL REFERENCES organizations(id),
  name       TEXT NOT NULL,
  key_hash   TEXT NOT NULL,
  key_prefix TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ
);
CREATE INDEX ON api_keys (org_id);
CREATE UNIQUE INDEX ON api_keys (key_hash);

-- +goose Down
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS organizations;
