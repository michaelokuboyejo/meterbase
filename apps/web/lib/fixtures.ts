// Contract-shaped mock data for FE-2/FE-3. Replaced by live API calls in Phase 7.
// Types mirror openapi.yaml schemas exactly.

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

// --- FE-3 types & fixtures ---------------------------------------------------

export type Customer = {
  id: string
  externalId: string
  name: string | null
  metadata: Record<string, unknown>
  createdAt: string
}

export type AlertScope = "subject" | "customer" | "global"

export type AlertRule = {
  id: string
  meterId: string
  scope: AlertScope
  window: WindowSize
  threshold: number
  enabled: boolean
  createdAt: string
}

export type WebhookEndpoint = {
  id: string
  url: string
  enabled: boolean
}

export type WebhookDelivery = {
  id: string
  endpointId: string
  eventType: string
  payload: Record<string, unknown>
  status: "pending" | "succeeded" | "failed"
  attempts: number
  lastAttempt: string | null
  createdAt: string
}

export const MOCK_CUSTOMERS: Customer[] = [
  {
    id: "cus_acme",
    externalId: "acme-corp",
    name: "Acme Corp",
    metadata: { plan: "pro" },
    createdAt: "2026-06-01T00:00:00Z",
  },
  {
    id: "cus_globex",
    externalId: "globex",
    name: "Globex Inc",
    metadata: {},
    createdAt: "2026-06-03T00:00:00Z",
  },
  {
    id: "cus_initech",
    externalId: "user_789",
    name: null,
    metadata: {},
    createdAt: "2026-06-10T00:00:00Z",
  },
]

export const MOCK_ALERT_RULES: AlertRule[] = [
  {
    id: "alr_tokens_month",
    meterId: "mtr_tokens_used",
    scope: "customer",
    window: "MONTH",
    threshold: 1000000,
    enabled: true,
    createdAt: "2026-06-05T00:00:00Z",
  },
  {
    id: "alr_api_day",
    meterId: "mtr_api_requests",
    scope: "subject",
    window: "DAY",
    threshold: 5000,
    enabled: false,
    createdAt: "2026-06-08T00:00:00Z",
  },
]

export const MOCK_WEBHOOKS: WebhookEndpoint[] = [
  {
    id: "wh_prod",
    url: "https://hooks.example.com/meterbase",
    enabled: true,
  },
  {
    id: "wh_dev",
    url: "https://hooks.example.com/meterbase-dev",
    enabled: false,
  },
]

export const MOCK_DELIVERIES: WebhookDelivery[] = [
  {
    id: "del_001",
    endpointId: "wh_prod",
    eventType: "alert.triggered",
    payload: { meter: "tokens_used", value: 1050000, threshold: 1000000 },
    status: "succeeded",
    attempts: 1,
    lastAttempt: "2026-06-14T09:01:00Z",
    createdAt: "2026-06-14T09:00:00Z",
  },
  {
    id: "del_002",
    endpointId: "wh_prod",
    eventType: "alert.triggered",
    payload: { meter: "tokens_used", value: 1100000, threshold: 1000000 },
    status: "failed",
    attempts: 3,
    lastAttempt: "2026-06-14T10:05:00Z",
    createdAt: "2026-06-14T10:00:00Z",
  },
  {
    id: "del_003",
    endpointId: "wh_dev",
    eventType: "alert.triggered",
    payload: { meter: "api_requests", value: 5200, threshold: 5000 },
    status: "pending",
    attempts: 0,
    lastAttempt: null,
    createdAt: "2026-06-14T11:00:00Z",
  },
]
