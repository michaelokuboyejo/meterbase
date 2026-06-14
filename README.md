# MeterBase

Developer-first **usage metering & monetization infrastructure**. Ingest usage
events, define meters, query time-bucketed aggregates, compute cost from flexible
pricing models, and fire usage-threshold alerts via signed webhooks — behind one
API contract, with a slick dashboard on top.

Think "Stripe for metering": drop-in integration for any product with
consumption-based pricing (APIs, AI/LLM tokens, infra, agentic outcomes).

## Monorepo layout

```
meterbase/
├── apps/
│   ├── api/                  # Go service: metering core (Postgres + TimescaleDB)
│   │   ├── Makefile          #   go targets: migrate/server/test/lint
│   │   ├── Dockerfile, .air.toml
│   │   └── migrations/       #   goose SQL (00001 creates the timescaledb extension)
│   └── web/                  # Next.js + Tailwind v4 + shadcn/ui dashboard
│       ├── app/globals.css   #   the theme (design tokens)
│       └── components/ui/    #   shadcn primitives
├── packages/
│   └── contract/openapi.yaml # REST contract — SINGLE SOURCE OF TRUTH
├── Makefile                  # root orchestration (make help)
├── docker-compose.yaml       # db + api + web
├── pnpm-workspace.yaml · turbo.json · package.json   # JS workspace
└── .env.example
```

## Architecture

A single Go service (HTTP API + background workers) backed by **PostgreSQL +
TimescaleDB** — raw events in a hypertable, time-bucketed reads from continuous
aggregates. No Kafka ... just yet. That'll come later, don't worry. The frontend is a **Next.js** app using **shadcn/ui** that talks to the API only through the contract.

## Prerequisites

- **Go 1.22+**, **Node 20+**, **pnpm** (`corepack enable`), **Docker**
- Go tools: **goose**, **air** (`make -C apps/api tools`)
- **golangci-lint** — https://golangci-lint.run/welcome/install/

## Quickstart

### Option A — full stack in Docker
```bash
cp .env.example .env
make up            # builds & runs db + api + web (api needs Phase 0 code to boot)
# web → http://localhost:3000   api → http://localhost:48888
```

### Option B — host dev with hot reload (recommended while building)
```bash
make install       # pnpm install + Go tools
make up-db         # start only the database (waits until healthy)
make migrate       # apply migrations (creates the timescaledb extension)

make dev-api       # terminal 1 — Go API on :48888 (hot reload)
make dev-web       # terminal 2 — Next.js on :3000
```

End-to-end flow once the API is live (the "10-minute path"):

```bash
export METERBASE_API_KEY=mb_xxx   # from the Phase 1 bootstrap/seed command or the dashboard

curl -s http://localhost:48888/v1/meters \
  -H "Authorization: Bearer $METERBASE_API_KEY" -H "Content-Type: application/json" \
  -d '{"slug":"api_requests","eventType":"api_request","aggregation":"COUNT"}'

curl -s http://localhost:48888/v1/events \
  -H "Authorization: Bearer $METERBASE_API_KEY" -H "Content-Type: application/json" \
  -d '{"id":"evt_001","type":"api_request","subject":"user_123","data":{}}'

curl -s "http://localhost:48888/v1/meters/api_requests/query?windowSize=HOUR&from=2026-01-01T00:00:00Z&to=2026-12-31T00:00:00Z" \
  -H "Authorization: Bearer $METERBASE_API_KEY"
```

## Root make targets

| Target | What it does |
| --- | --- |
| `make up` / `make down` | Build & run / stop the full Docker stack |
| `make up-db` / `make db-reset` | Start just the DB / wipe and restart it |
| `make install` | pnpm install + Go tools |
| `make migrate` | Apply DB migrations |
| `make dev-api` / `make dev-web` | Hot-reload dev servers |
| `make build` / `make test` / `make lint` | Across both apps |
| `make gen-sdk` | Generate TS client types from the contract |

## License

TBD.
