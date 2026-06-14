// Contract-shaped mock data for FE-2. Replaced by live API calls in Phase 7.
// Types mirror openapi.yaml schemas exactly (Meter, QueryResult).

export type Aggregation = "SUM" | "COUNT" | "AVG" | "MIN" | "MAX" | "UNIQUE_COUNT"
export type WindowSize = "MINUTE" | "HOUR" | "DAY" | "MONTH"

export type Meter = {
  id: string
  slug: string
  eventType: string
  aggregation: Aggregation
  valueProperty: string | null
  groupBy: string[]
  createdAt: string
}

export type QueryBucket = {
  bucket: string
  value: number
  groups?: Record<string, string>
}

export type QueryResult = {
  meter: string
  windowSize: WindowSize
  data: QueryBucket[]
}

function dailyBuckets(days: number, base: number, seed = 1): QueryBucket[] {
  const end = new Date("2026-06-14T00:00:00Z")
  return Array.from({ length: days }, (_, i) => {
    const d = new Date(end)
    d.setUTCDate(d.getUTCDate() - (days - 1 - i))
    const noise = Math.sin((i + seed) * 2.7) * 0.3
    return {
      bucket: d.toISOString(),
      value: Math.max(0, Math.round(base * (1 + noise + (i / days) * 0.4))),
    }
  })
}

export const MOCK_METERS: Meter[] = [
  {
    id: "mtr_api_requests",
    slug: "api_requests",
    eventType: "api_request",
    aggregation: "COUNT",
    valueProperty: null,
    groupBy: ["model", "region"],
    createdAt: "2026-06-01T00:00:00Z",
  },
  {
    id: "mtr_tokens_used",
    slug: "tokens_used",
    eventType: "llm_call",
    aggregation: "SUM",
    valueProperty: "tokens",
    groupBy: ["model"],
    createdAt: "2026-06-01T00:00:00Z",
  },
  {
    id: "mtr_active_users",
    slug: "active_users",
    eventType: "user_action",
    aggregation: "UNIQUE_COUNT",
    valueProperty: null,
    groupBy: [],
    createdAt: "2026-06-03T00:00:00Z",
  },
]

const SEEDS: Record<string, { base: number; seed: number }> = {
  api_requests: { base: 1200, seed: 1 },
  tokens_used: { base: 85000, seed: 3 },
  active_users: { base: 240, seed: 7 },
}

export function mockQueryResult(slug: string, windowSize: WindowSize): QueryResult {
  const { base, seed } = SEEDS[slug] ?? { base: 1000, seed: 2 }
  return { meter: slug, windowSize, data: dailyBuckets(30, base, seed) }
}

export const MOCK_OVERVIEW_QUERY: QueryResult = {
  meter: "api_requests",
  windowSize: "DAY",
  data: dailyBuckets(14, 1200, 1),
}
