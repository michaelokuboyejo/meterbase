-- +goose Up

CREATE TYPE aggregation AS ENUM ('SUM', 'COUNT', 'AVG', 'MIN', 'MAX', 'UNIQUE_COUNT');

CREATE TABLE meters (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        UUID NOT NULL REFERENCES organizations(id),
  slug          TEXT NOT NULL,
  event_type    TEXT NOT NULL,
  agg           aggregation NOT NULL,
  value_prop    TEXT,
  group_by      TEXT[] NOT NULL DEFAULT '{}',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, slug)
);

CREATE TABLE events (
  org_id       UUID NOT NULL,
  id           TEXT NOT NULL,
  type         TEXT NOT NULL,
  source       TEXT,
  subject      TEXT NOT NULL,
  customer_id  UUID,
  time         TIMESTAMPTZ NOT NULL,
  ingested_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  data         JSONB NOT NULL DEFAULT '{}',
  PRIMARY KEY (org_id, id, time)
);

SELECT create_hypertable('events', 'time');
CREATE INDEX ON events (org_id, type, time DESC);
CREATE INDEX ON events (org_id, subject, time DESC);

-- +goose Down
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS meters;
DROP TYPE IF EXISTS aggregation;
