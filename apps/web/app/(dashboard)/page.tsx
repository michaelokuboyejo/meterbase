import { Zap } from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { UsageChart } from "@/components/usage-chart";
import { api } from "@/lib/api";
import { MOCK_OVERVIEW_QUERY } from "@/lib/fixtures";

export default async function OverviewPage() {
  const [metersRes, eventsRes] = await Promise.all([
    api.GET("/v1/meters", { params: { query: { limit: 100 } } }),
    api.GET("/v1/events", { params: { query: { limit: 5 } } }),
  ]);

  const meters = metersRes.data?.data ?? [];
  const recent = eventsRes.data?.data ?? [];

  const metrics = [
    { label: "Events today", value: "—", delta: "", up: true },
    { label: "Active meters", value: String(meters.length), delta: "", up: true },
    { label: "Tracked customers", value: "—", delta: "", up: true },
    { label: "Est. revenue (MTD)", value: "—", delta: "", up: true },
  ];

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-8">
      <div className="mb-8">
        <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Usage across all meters for the current billing period.
        </p>
      </div>

      {/* Metric cards */}
      <div className="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {metrics.map((m) => (
          <Card key={m.label} className="gap-3 py-5">
            <CardHeader className="px-5">
              <CardDescription className="text-xs font-medium uppercase tracking-wide">
                {m.label}
              </CardDescription>
            </CardHeader>
            <CardContent className="px-5">
              <div className="nums text-3xl font-semibold tracking-tight">
                {m.value}
              </div>
              <div className="text-muted-foreground mt-1.5 flex items-center gap-1 text-xs">
                <Zap className="text-chart-3 size-3" />
                <span className="nums text-chart-3 font-medium">{m.delta}</span>
                <span>vs. last period</span>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Usage + recent */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">Usage</CardTitle>
            <CardDescription>Total metered events, last 14 days</CardDescription>
          </CardHeader>
          <CardContent>
            <UsageChart
              data={MOCK_OVERVIEW_QUERY.data}
              windowSize={MOCK_OVERVIEW_QUERY.windowSize}
              label="Events"
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Recent events</CardTitle>
            <CardDescription>Raw ingest stream</CardDescription>
          </CardHeader>
          <CardContent className="px-0">
            <div className="divide-border divide-y">
              {recent.length === 0 ? (
                <div className="text-muted-foreground px-6 py-4 text-xs">
                  No events yet — send your first event via the SDK or API.
                </div>
              ) : (
                recent.map((e) => (
                  <div
                    key={e.id}
                    className="flex items-center justify-between gap-3 px-6 py-2.5"
                  >
                    <div className="min-w-0">
                      <div className="truncate font-mono text-xs">{e.id}</div>
                      <div className="text-muted-foreground text-xs">
                        {e.type} · {e.subject}
                      </div>
                    </div>
                    <div className="text-right">
                      <div className="text-muted-foreground nums font-mono text-[11px]">
                        {e.time
                          ? new Date(e.time).toLocaleTimeString()
                          : "—"}
                      </div>
                    </div>
                  </div>
                ))
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </main>
  );
}
