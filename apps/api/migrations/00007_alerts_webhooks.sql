-- +goose Up

CREATE TABLE alert_rules (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       UUID NOT NULL REFERENCES organizations(id),
  meter_id     UUID NOT NULL REFERENCES meters(id),
  scope        TEXT NOT NULL CHECK (scope IN ('subject','customer','global')),
  alert_window TEXT NOT NULL,          -- MINUTE|HOUR|DAY|MONTH
  threshold    NUMERIC NOT NULL,
  enabled      BOOLEAN NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON alert_rules (org_id);
CREATE INDEX ON alert_rules (enabled) WHERE enabled = true;

-- De-bounce guard: one row per (rule, scope_key, window_start) crossing.
-- INSERT ON CONFLICT DO NOTHING RETURNING id is the atomic "first crossing" check.
CREATE TABLE alert_firings (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id      UUID NOT NULL REFERENCES alert_rules(id),
  scope_key    TEXT NOT NULL,          -- subject value, customer UUID, or '' for global
  window_start TIMESTAMPTZ NOT NULL,   -- time_bucket(window, now) at evaluation time
  fired_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (rule_id, scope_key, window_start)
);

CREATE TABLE webhook_endpoints (
  id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id   UUID NOT NULL REFERENCES organizations(id),
  url      TEXT NOT NULL,
  secret   TEXT NOT NULL,              -- raw HMAC key; returned once on creation
  enabled  BOOLEAN NOT NULL DEFAULT true
);
CREATE INDEX ON webhook_endpoints (org_id);

CREATE TABLE webhook_deliveries (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  endpoint_id  UUID NOT NULL REFERENCES webhook_endpoints(id),
  event_type   TEXT NOT NULL,
  payload      JSONB NOT NULL,
  status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','succeeded','failed')),
  attempts     INT NOT NULL DEFAULT 0,
  last_attempt TIMESTAMPTZ,
  next_attempt TIMESTAMPTZ,            -- when dispatcher should retry; NULL = try now
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON webhook_deliveries (status, next_attempt) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_endpoints;
DROP TABLE IF EXISTS alert_firings;
DROP TABLE IF EXISTS alert_rules;
