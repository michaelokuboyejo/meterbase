"use client";

import { LogOut, Plus } from "lucide-react";
import { usePathname } from "next/navigation";
import { useTransition } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ThemeToggle } from "@/components/theme-toggle";
import { logoutAction } from "@/app/(auth)/login/actions";
import type { DashboardUser } from "@/lib/session";

const sectionLabels: Record<string, string> = {
  "/": "Overview",
  "/meters": "Meters",
  "/customers": "Customers",
  "/pricing": "Pricing",
  "/alerts": "Alerts",
  "/webhooks": "Webhooks",
  "/settings": "Settings",
};

function getSectionLabel(pathname: string): string {
  if (pathname === "/") return "Overview";
  const segment = "/" + pathname.split("/")[1];
  return sectionLabels[segment] ?? "Overview";
}

export function AppTopbar({ user }: { user: DashboardUser }) {
  const pathname = usePathname();
  const section = getSectionLabel(pathname);
  const [pending, startTransition] = useTransition();

  function handleLogout() {
    startTransition(async () => {
      await logoutAction();
    });
  }

  return (
    <header className="flex h-14 items-center justify-between border-b px-6">
      <div className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">MeterBase</span>
        <span className="text-muted-foreground/50">/</span>
        <span className="font-medium">{section}</span>
      </div>
      <div className="flex items-center gap-2">
        <Badge variant="secondary" className="font-mono">
          <span className="bg-chart-3 size-1.5 rounded-full" />
          live
        </Badge>
        <span className="text-muted-foreground hidden text-xs sm:inline">
          {user.email}
        </span>
        <Badge variant="outline" className="hidden sm:inline-flex">
          {user.role}
        </Badge>
        <ThemeToggle />
        <Button size="sm">
          <Plus />
          Send test event
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={handleLogout}
          disabled={pending}
          aria-label="Sign out"
        >
          <LogOut className="size-4" />
        </Button>
      </div>
    </header>
  );
}
