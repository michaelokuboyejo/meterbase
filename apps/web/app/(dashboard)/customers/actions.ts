"use server";

import { api } from "@/lib/api";

export async function createCustomerAction(
  externalId: string,
  name: string | null
): Promise<void> {
  const { error } = await api.POST("/v1/customers", {
    body: { externalId, name: name ?? null, metadata: {} },
  });
  if (error) {
    const msg = (error as { error?: { message?: string } })?.error?.message;
    throw new Error(msg ?? "Failed to create customer");
  }
}
