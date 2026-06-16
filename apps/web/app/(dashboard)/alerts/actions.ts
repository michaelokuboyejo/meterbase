"use server";

import { api } from "@/lib/api";

type AlertScope = "subject" | "customer" | "global";
type WindowSize = "MINUTE" | "HOUR" | "DAY" | "MONTH";

export async function createAlertAction(params: {
  meterId: string;
  scope: AlertScope;
  window: WindowSize;
  threshold: number;
}): Promise<void> {
  const { error } = await api.POST("/v1/alert-rules", {
    body: { ...params, enabled: true },
  });
  if (error) {
    const msg = (error as { error?: { message?: string } })?.error?.message;
    throw new Error(msg ?? "Failed to create alert rule");
  }
}
