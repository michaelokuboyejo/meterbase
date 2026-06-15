import { createClient } from "@meterbase/sdk";

// API_URL is the server-side base URL (e.g. http://api:48888 in Docker).
// NEXT_PUBLIC_API_URL is baked into the client bundle for browser fetches.
// METERBASE_API_KEY is a server-only secret — never prefixed with NEXT_PUBLIC_.
export const api = createClient(
  process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:48888",
  process.env.METERBASE_API_KEY ?? ""
);
