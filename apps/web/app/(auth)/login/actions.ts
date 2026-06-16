"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

const API_URL =
  process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:48888";

const SESSION_MAX_AGE = 60 * 60 * 24 * 7; // 7 days in seconds

export async function loginAction(
  _prev: string | null,
  formData: FormData
): Promise<string | null> {
  const email = formData.get("email") as string;
  const password = formData.get("password") as string;

  if (!email || !password) {
    return "Email and password are required.";
  }

  let res: Response;
  try {
    res = await fetch(`${API_URL}/auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
    });
  } catch {
    return "Could not reach the API. Check that the server is running.";
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    return (body as { error?: { message?: string } })?.error?.message ?? "Invalid email or password.";
  }

  const data = (await res.json()) as { token: string };
  const cookieStore = await cookies();
  cookieStore.set("mb_session", data.token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    maxAge: SESSION_MAX_AGE,
    path: "/",
  });

  redirect("/");
}

export async function logoutAction(): Promise<void> {
  const cookieStore = await cookies();
  const token = cookieStore.get("mb_session")?.value;

  if (token) {
    try {
      await fetch(`${API_URL}/auth/logout`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      });
    } catch {
      // best-effort — clear cookie regardless
    }
    cookieStore.delete("mb_session");
  }

  redirect("/login");
}
