import Link from "next/link";
import { ChevronLeft } from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { UsageChart } from "@/components/usage-chart";
import { api } from "@/lib/api";

export const dynamic = "force-dynamic";

const AGG_LABELS: Record<string, string> = {
  SUM: "Sum",
  COUNT: "Count",
  AVG: "Average",
  MIN: "Minimum",
  MAX: "Maximum",
  UNIQUE_COUNT: "Unique count",
};

export default async function MeterDetailPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;

  const now = new Date();
  const thirtyDaysAgo = new Date(now);
  thirtyDaysAgo.setUTCDate(thirtyDaysAgo.getUTCDate() - 30);

  const [meterRes, queryRes] = await Promise.all([
    api.GET("/v1/meters/{slug}", { params: { path: { slug } } }),
    api.GET("/v1/meters/{slug}/query", {
      params: {
        path: { slug },
        query: {
          from: thirtyDaysAgo.toISOString(),
          to: now.toISOString(),
          windowSize: "DAY",
        },
      },
    }),
  ]);

  const meter = meterRes.data;

  if (!meter) {
    return (
      <main className="mx-auto w-full max-w-6xl px-6 py-8">
        <p className="text-muted-foreground text-sm">
          Meter <span className="font-mono">{slug}</span> not found.
        </p>
      </main>
    );
  }

  const queryData = queryRes.data?.data ?? [];

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-8">
      <div className="mb-6">
        <Link
          href="/meters"
          className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm transition-colors"
        >
          <ChevronLeft className="size-3.5" />
          Meters
        </Link>
        <h1 className="mt-1 font-mono text-2xl font-semibold tracking-tight">
          {meter.slug}
        </h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Event type: <span className="font-mono">{meter.eventType}</span>
        </p>
      </div>

      <div className="mb-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs font-medium uppercase tracking-wide">
              Aggregation
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Badge variant="secondary">{meter.aggregation}</Badge>
            <p className="text-muted-foreground mt-1 text-xs">
              {AGG_LABELS[meter.aggregation] ?? meter.aggregation}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs font-medium uppercase tracking-wide">
              Value property
            </CardDescription>
          </CardHeader>
          <CardContent>
            {meter.valueProperty ? (
              <span className="font-mono text-sm">{meter.valueProperty}</span>
            ) : (
              <span className="text-muted-foreground text-sm">—</span>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs font-medium uppercase tracking-wide">
              Group by
            </CardDescription>
          </CardHeader>
          <CardContent>
            {meter.groupBy && meter.groupBy.length > 0 ? (
              <div className="flex flex-wrap gap-1">
                {meter.groupBy.map((dim) => (
                  <Badge key={dim} variant="secondary" className="font-mono">
                    {dim}
                  </Badge>
                ))}
              </div>
            ) : (
              <span className="text-muted-foreground text-sm">None</span>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Usage</CardTitle>
          <CardDescription>
            Last 30 days &middot; DAY window &middot;{" "}
            {AGG_LABELS[meter.aggregation] ?? meter.aggregation}
            {meter.valueProperty ? ` of ${meter.valueProperty}` : ""}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <UsageChart
            data={queryData}
            windowSize="DAY"
            label={AGG_LABELS[meter.aggregation] ?? meter.aggregation}
            className="h-56"
          />
        </CardContent>
      </Card>
    </main>
  );
}
