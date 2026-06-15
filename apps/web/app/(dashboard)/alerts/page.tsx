import { Badge } from "@/components/ui/badge";
import { api } from "@/lib/api";
import { CreateAlertDialog } from "./create-alert-dialog";

export const dynamic = "force-dynamic";

const fmt = new Intl.NumberFormat("en-US", { notation: "compact" });

export default async function AlertsPage() {
  const [alertsRes, metersRes] = await Promise.all([
    api.GET("/v1/alert-rules", { params: { query: { limit: 100 } } }),
    api.GET("/v1/meters", { params: { query: { limit: 100 } } }),
  ]);

  const rules = alertsRes.data?.data ?? [];
  const meters = metersRes.data?.data ?? [];
  const meterIndex = Object.fromEntries(meters.map((m) => [m.id, m.slug]));

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-8">
      <div className="mb-8 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Alerts</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Set usage-threshold alert rules to fire signed webhooks.
          </p>
        </div>
        <CreateAlertDialog meters={meters} />
      </div>

      {rules.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No alert rules yet. Add a rule to be notified when usage crosses a threshold.
        </p>
      ) : (
        <div className="border-border rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-border border-b">
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Meter
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Scope
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Window
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Threshold
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Status
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Created
                </th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {rules.map((rule) => (
                <tr key={rule.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-3 font-mono text-sm">
                    {meterIndex[rule.meterId] ?? rule.meterId}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant="secondary">{rule.scope}</Badge>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant="secondary">{rule.window}</Badge>
                  </td>
                  <td className="nums px-4 py-3 text-right font-mono text-sm">
                    {fmt.format(rule.threshold)}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={rule.enabled ? "default" : "secondary"}>
                      {rule.enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </td>
                  <td className="text-muted-foreground nums px-4 py-3 text-right font-mono text-xs">
                    {new Date(rule.createdAt).toLocaleDateString("en-US", {
                      year: "numeric",
                      month: "short",
                      day: "numeric",
                    })}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </main>
  );
}
