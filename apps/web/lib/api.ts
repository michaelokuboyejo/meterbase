// Thin API client. In Phase 7 this is replaced by a typed client generated from
// packages/contract/openapi.yaml (`make gen-sdk`). Keep all server access here.
const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:48888";

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });
  if (!res.ok) {
    throw new Error(`API ${res.status}: ${await res.text()}`);
  }
  return (await res.json()) as T;
}
