import { cookies } from "next/headers";

const API_URL =
  process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:48888";

export type DashboardUser = {
  id: string;
  orgId: string;
  email: string;
  role: "admin" | "viewer";
};

// getUser reads the session cookie and validates it against /auth/me.
// Returns the user or null if the session is missing or invalid.
// Must only be called from server components / server actions.
export async function getUser(): Promise<DashboardUser | null> {
  const cookieStore = await cookies();
  const token = cookieStore.get("mb_session")?.value;
  if (!token) return null;

  try {
    const res = await fetch(`${API_URL}/auth/me`, {
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!res.ok) return null;
    return res.json() as Promise<DashboardUser>;
  } catch {
    return null;
  }
}
