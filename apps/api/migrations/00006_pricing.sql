-- +goose Up
CREATE TYPE pricing_model AS ENUM ('PAYG','FLAT_PLUS_OVERAGE','TIERED');

CREATE TABLE plans (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      UUID NOT NULL REFERENCES organizations(id),
  name        TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE rate_cards (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  plan_id     UUID NOT NULL REFERENCES plans(id),
  meter_id    UUID NOT NULL REFERENCES meters(id),
  model       pricing_model NOT NULL,
  config      JSONB NOT NULL,
  currency    TEXT NOT NULL DEFAULT 'USD'
);

-- +goose Down
DROP TABLE IF EXISTS rate_cards;
DROP TABLE IF EXISTS plans;
DROP TYPE IF EXISTS pricing_model;
