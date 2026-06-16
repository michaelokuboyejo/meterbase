"use server";

import { api } from "@/lib/api";

export async function registerWebhookAction(
  url: string
): Promise<{ id: string; url: string; secret: string; enabled: boolean }> {
  const { data, error } = await api.POST("/v1/webhooks", { body: { url } });
  if (error || !data) {
    const msg = (error as { error?: { message?: string } })?.error?.message;
    throw new Error(msg ?? "Failed to register webhook");
  }
  return data;
}
