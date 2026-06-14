import createFetchClient from "openapi-fetch";
import type { paths } from "./schema.js";

/**
 * Creates a type-safe MeterBase API client.
 * All endpoints from packages/contract/openapi.yaml are available with full
 * request/response types inferred from the generated schema.
 */
export function createClient(baseUrl: string, apiKey: string) {
  const client = createFetchClient<paths>({ baseUrl });

  client.use({
    onRequest({ request }) {
      if (apiKey) {
        request.headers.set("Authorization", `Bearer ${apiKey}`);
      }
      return request;
    },
  });

  return client;
}
