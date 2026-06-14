-- +goose NO TRANSACTION
-- +goose Up

CREATE MATERIALIZED VIEW events_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
SELECT
  org_id,
  type,
  subject,
  time_bucket('1 hour', time) AS bucket,
  count(*) AS event_count
FROM events
GROUP BY org_id, type, subject, bucket;

SELECT add_continuous_aggregate_policy('events_hourly',
  start_offset      => INTERVAL '30 days',
  end_offset        => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 minute');

CREATE MATERIALIZED VIEW events_daily
WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
SELECT
  org_id,
  type,
  subject,
  time_bucket('1 day', time) AS bucket,
  count(*) AS event_count
FROM events
GROUP BY org_id, type, subject, bucket;

SELECT add_continuous_aggregate_policy('events_daily',
  start_offset      => INTERVAL '90 days',
  end_offset        => INTERVAL '1 day',
  schedule_interval => INTERVAL '1 minute');

-- +goose Down
DROP MATERIALIZED VIEW IF EXISTS events_daily CASCADE;
DROP MATERIALIZED VIEW IF EXISTS events_hourly CASCADE;
