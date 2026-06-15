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

export const dynamic = "force-dynamic";

export default async function OverviewPage() {
  const now = new Date();
  const fourteenDaysAgo = new Date(now);
  fourteenDaysAgo.setUTCDate(fourteenDaysAgo.getUTCDate() - 14);

  const [metersRes, eventsRes] = await Promise.all([
    api.GET("/v1/meters", { params: { query: { limit: 100 } } }),
    api.GET("/v1/events", { params: { query: { limit: 5 } } }),
  ]);

  const meters = metersRes.data?.data ?? [];
  const recent = eventsRes.data?.data ?? [];

  // Query the first meter's usage for the overview chart, if any meters exist.
  let chartData: { bucket: string; value: number }[] = [];
  const chartMeter = meters[0]?.slug ?? "";
  if (chartMeter) {
    const queryRes = await api.GET("/v1/meters/{slug}/query", {
      params: {
        path: { slug: chartMeter },
        query: {
          from: fourteenDaysAgo.toISOString(),
          to: now.toISOString(),
          windowSize: "DAY",
        },
      },
    });
    chartData = (queryRes.data?.data ?? []).map((pt) => ({
      bucket: pt.bucket,
      value: pt.value,
    }));
  }

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
            <CardDescription>
              {chartMeter ? (
                <>
                  <span className="font-mono">{chartMeter}</span> · last 14 days
                </>
              ) : (
                "Total metered events, last 14 days"
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <UsageChart
              data={chartData}
              windowSize="DAY"
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
