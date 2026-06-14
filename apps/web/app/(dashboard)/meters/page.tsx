import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import { MOCK_METERS } from "@/lib/fixtures";

export default function MetersPage() {
  const meters = MOCK_METERS;

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-8">
      <div className="mb-8">
        <h1 className="text-2xl font-semibold tracking-tight">Meters</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Define and manage usage meters. Each meter aggregates a specific event
          type into time-bucketed values.
        </p>
      </div>

      {meters.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No meters defined yet. Create a meter via the API to start aggregating
          events.
        </p>
      ) : (
        <div className="border-border rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-border border-b">
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Slug
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Event type
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Aggregation
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Created
                </th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {meters.map((meter) => (
                <tr key={meter.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-3">
                    <Link
                      href={`/meters/${meter.slug}`}
                      className="hover:text-primary font-mono text-sm transition-colors"
                    >
                      {meter.slug}
                    </Link>
                  </td>
                  <td className="text-muted-foreground px-4 py-3 text-sm">
                    {meter.eventType}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant="secondary">{meter.aggregation}</Badge>
                  </td>
                  <td className="text-muted-foreground nums px-4 py-3 text-right font-mono text-xs">
                    {new Date(meter.createdAt).toLocaleDateString("en-US", {
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
