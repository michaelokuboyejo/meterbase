"use client";

import { Plus } from "lucide-react";
import { usePathname } from "next/navigation";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ThemeToggle } from "@/components/theme-toggle";

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

export function AppTopbar() {
  const pathname = usePathname();
  const section = getSectionLabel(pathname);

  return (
    <header className="flex h-14 items-center justify-between border-b px-6">
      <div className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">Acme Inc</span>
        <span className="text-muted-foreground/50">/</span>
        <span className="font-medium">{section}</span>
      </div>
      <div className="flex items-center gap-2">
        <Badge variant="secondary" className="font-mono">
          <span className="bg-chart-3 size-1.5 rounded-full" />
          live
        </Badge>
        <ThemeToggle />
        <Button size="sm">
          <Plus />
          Send test event
        </Button>
      </div>
    </header>
  );
}
