import {
  Activity,
  Bell,
  CreditCard,
  Gauge,
  Plus,
  Settings,
  Users,
  Webhook,
  Zap,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

// Placeholder data — replaced by the query API in Phase 7.
const metrics = [
  { label: "Events today", value: "1,284,910", delta: "+12.4%", up: true },
  { label: "Active meters", value: "18", delta: "+2", up: true },
  { label: "Tracked customers", value: "342", delta: "+9", up: true },
  { label: "Est. revenue (MTD)", value: "$48,210", delta: "+6.1%", up: true },
];

const usage = [38, 41, 36, 52, 48, 61, 55, 67, 72, 64, 78, 81, 74, 92];

const recent = [
  { id: "evt_01HZX9F3K2", type: "api_request", subject: "user_8821", value: "1", time: "12:04:51" },
  { id: "evt_01HZX9F1A7", type: "tokens", subject: "user_2043", value: "1,500", time: "12:04:50" },
  { id: "evt_01HZX9EZ9P", type: "api_request", subject: "user_8821", value: "1", time: "12:04:50" },
  { id: "evt_01HZX9EYK4", type: "storage_gb", subject: "acct_func", value: "2.4", time: "12:04:48" },
  { id: "evt_01HZX9EX2M", type: "tokens", subject: "user_5519", value: "920", time: "12:04:47" },
];

const nav = [
  { label: "Overview", icon: Gauge, active: true },
  { label: "Meters", icon: Activity, active: false },
  { label: "Customers", icon: Users, active: false },
  { label: "Pricing", icon: CreditCard, active: false },
  { label: "Alerts", icon: Bell, active: false },
  { label: "Webhooks", icon: Webhook, active: false },
];

export default function Page() {
  return (
    <div className="flex min-h-screen">
      {/* Sidebar */}
      <aside className="bg-sidebar border-sidebar-border hidden w-60 flex-col border-r md:flex">
        <div className="flex h-14 items-center gap-2 px-5">
          <span className="bg-primary size-2.5 rounded-full" />
          <span className="text-[15px] font-semibold tracking-tight">MeterBase</span>
        </div>
        <nav className="flex flex-1 flex-col gap-0.5 px-3 py-2">
          {nav.map(({ label, icon: Icon, active }) => (
            <a
              key={label}
              href="#"
              className={
                "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors " +
                (active
                  ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
                  : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground")
              }
            >
              <Icon className="size-4" />
              {label}
            </a>
          ))}
        </nav>
        <div className="px-3 py-3">
          <a
            href="#"
            className="text-muted-foreground hover:text-foreground flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors"
          >
            <Settings className="size-4" />
            Settings
          </a>
        </div>
      </aside>

      {/* Main */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Topbar */}
        <header className="flex h-14 items-center justify-between border-b px-6">
          <div className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">Acme Inc</span>
            <span className="text-muted-foreground/50">/</span>
            <span className="font-medium">Overview</span>
          </div>
          <div className="flex items-center gap-3">
            <Badge variant="secondary" className="font-mono">
              <span className="bg-chart-3 size-1.5 rounded-full" />
              live
            </Badge>
            <Button size="sm">
              <Plus />
              Send test event
            </Button>
          </div>
        </header>

        {/* Content */}
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
                <div className="flex h-40 items-end gap-1.5">
                  {usage.map((v, i) => (
                    <div
                      key={i}
                      className="bg-primary/15 hover:bg-primary/30 flex-1 rounded-t-sm transition-colors"
                      style={{ height: `${v}%` }}
                    />
                  ))}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-base">Recent events</CardTitle>
                <CardDescription>Raw ingest stream</CardDescription>
              </CardHeader>
              <CardContent className="px-0">
                <div className="divide-border divide-y">
                  {recent.map((e) => (
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
                        <div className="nums text-sm font-medium">{e.value}</div>
                        <div className="text-muted-foreground nums font-mono text-[11px]">
                          {e.time}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </main>
      </div>
    </div>
  );
}
