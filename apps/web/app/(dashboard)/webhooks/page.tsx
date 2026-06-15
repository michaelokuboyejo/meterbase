import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { api } from "@/lib/api";
import { RegisterWebhookDialog } from "./register-webhook-dialog";

export default async function WebhooksPage() {
  const res = await api.GET("/v1/webhooks", { params: { query: { limit: 100 } } });
  const webhooks = res.data?.data ?? [];

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-8">
      <div className="mb-8 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Webhooks</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Manage webhook endpoints. MeterBase signs every delivery with HMAC-SHA256.
          </p>
        </div>
        <RegisterWebhookDialog />
      </div>

      {webhooks.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No webhook endpoints registered yet.
        </p>
      ) : (
        <div className="border-border rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-border border-b">
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  URL
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  ID
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Status
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Deliveries
                </th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {webhooks.map((hook) => (
                <tr key={hook.id} className="hover:bg-muted/30 transition-colors">
                  <td className="max-w-xs truncate px-4 py-3 font-mono text-sm">
                    {hook.url}
                  </td>
                  <td className="text-muted-foreground px-4 py-3 font-mono text-xs">
                    {hook.id}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={hook.enabled ? "default" : "secondary"}>
                      {hook.enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Link
                      href={`/webhooks/${hook.id}/deliveries`}
                      className="text-muted-foreground hover:text-primary text-xs transition-colors"
                    >
                      View deliveries →
                    </Link>
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
