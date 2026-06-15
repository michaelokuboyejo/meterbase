import { api } from "@/lib/api";
import { CreateCustomerDialog } from "./create-customer-dialog";

export default async function CustomersPage() {
  const res = await api.GET("/v1/customers", { params: { query: { limit: 100 } } });
  const customers = res.data?.data ?? [];

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-8">
      <div className="mb-8 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Customers</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Manage billable customers and their usage attribution.
          </p>
        </div>
        <CreateCustomerDialog />
      </div>

      {customers.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No customers yet. Add a customer to start tracking usage.
        </p>
      ) : (
        <div className="border-border rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-border border-b">
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  External ID
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  Name
                </th>
                <th className="text-muted-foreground px-4 py-3 text-left text-xs font-medium uppercase tracking-wide">
                  ID
                </th>
                <th className="text-muted-foreground px-4 py-3 text-right text-xs font-medium uppercase tracking-wide">
                  Created
                </th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {customers.map((customer) => (
                <tr key={customer.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-3 font-mono text-sm">
                    {customer.externalId}
                  </td>
                  <td className="text-muted-foreground px-4 py-3 text-sm">
                    {customer.name ?? <span className="opacity-40">—</span>}
                  </td>
                  <td className="text-muted-foreground px-4 py-3 font-mono text-xs">
                    {customer.id}
                  </td>
                  <td className="text-muted-foreground nums px-4 py-3 text-right font-mono text-xs">
                    {new Date(customer.createdAt).toLocaleDateString("en-US", {
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
