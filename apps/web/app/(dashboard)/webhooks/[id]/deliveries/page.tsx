import Link from "next/link";
import { ChevronLeft } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { api } from "@/lib/api";

export const dynamic = "force-dynamic";

export default async function DeliveriesPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;

  const [webhooksRes, deliveriesRes] = await Promise.all([
    api.GET("/v1/webhooks", { params: { query: { limit: 100 } } }),
    api.GET("/v1/webhooks/{id}/deliveries", {
      params: { path: { id }, query: { limit: 100 } },
    }),
  ]);

  const endpoint = (webhooksRes.data?.data ?? []).find((h) => h.id === id);
  const deliveries = deliveriesRes.data?.data ?? [];

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-8">
      <div className="mb-6">
        <Link
          href="/webhooks"
          className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm transition-colors"
        >
          <ChevronLeft className="size-3.5" />
          Webhooks
        </Link>
        <h1 className="mt-1 text-2xl font-semibold tracking-tight">
          Deliveries
        </h1>
        {endpoint && (
          <p className="text-muted-foreground mt-1 font-mono text-sm">
            {endpoint.url}
          </p>
        )}
      </div>

      {deliveries.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No deliveries yet for this endpoint.
        </p>
      ) : (
        <div className="border-border rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-border border-b">
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Event type
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Status
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Attempts
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Last attempt
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Created
                </th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {deliveries.map((delivery) => (
                <tr key={delivery.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-3 font-mono text-xs">
                    {delivery.eventType}
                  </td>
                  <td className="px-4 py-3">
                    <Badge
                      variant={
                        delivery.status === "succeeded"
                          ? "secondary"
                          : delivery.status === "failed"
                            ? "destructive"
                            : "outline"
                      }
                    >
                      {delivery.status}
                    </Badge>
                  </td>
                  <td className="nums px-4 py-3 text-right font-mono text-xs">
                    {delivery.attempts}
                  </td>
                  <td className="text-muted-foreground nums px-4 py-3 text-right font-mono text-xs">
                    {delivery.lastAttempt
                      ? new Date(delivery.lastAttempt).toLocaleString("en-US", {
                          month: "short",
                          day: "numeric",
                          hour: "2-digit",
                          minute: "2-digit",
                        })
                      : <span className="opacity-40">—</span>}
                  </td>
                  <td className="text-muted-foreground nums px-4 py-3 text-right font-mono text-xs">
                    {new Date(delivery.createdAt).toLocaleString("en-US", {
                      month: "short",
                      day: "numeric",
                      hour: "2-digit",
                      minute: "2-digit",
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
