import { createClient } from "@meterbase/sdk";

// METERBASE_API_KEY is a server-only env var — never prefixed with NEXT_PUBLIC_.
// NEXT_PUBLIC_API_URL is the base URL, safe to expose to the browser.
export const api = createClient(
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:48888",
  process.env.METERBASE_API_KEY ?? ""
);
