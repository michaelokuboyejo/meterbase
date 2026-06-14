"use client";

import {
  Activity,
  Bell,
  CreditCard,
  Gauge,
  Settings,
  Users,
  Webhook,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

const nav = [
  { label: "Overview", icon: Gauge, href: "/" },
  { label: "Meters", icon: Activity, href: "/meters" },
  { label: "Customers", icon: Users, href: "/customers" },
  { label: "Pricing", icon: CreditCard, href: "/pricing" },
  { label: "Alerts", icon: Bell, href: "/alerts" },
  { label: "Webhooks", icon: Webhook, href: "/webhooks" },
];

function isActive(pathname: string, href: string) {
  if (href === "/") return pathname === "/";
  return pathname.startsWith(href);
}

export function AppSidebar() {
  const pathname = usePathname();

  return (
    <aside className="bg-sidebar border-sidebar-border hidden w-60 flex-col border-r md:flex">
      <div className="flex h-14 items-center gap-2 px-5">
        <span className="bg-primary size-2.5 rounded-full" />
        <span className="text-[15px] font-semibold tracking-tight">MeterBase</span>
      </div>
      <nav className="flex flex-1 flex-col gap-0.5 px-3 py-2">
        {nav.map(({ label, icon: Icon, href }) => (
          <Link
            key={label}
            href={href}
            className={
              "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors " +
              (isActive(pathname, href)
                ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
                : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground")
            }
          >
            <Icon className="size-4" />
            {label}
          </Link>
        ))}
      </nav>
      <div className="px-3 py-3">
        <Link
          href="/settings"
          className={
            "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors " +
            (isActive(pathname, "/settings")
              ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
              : "text-muted-foreground hover:text-foreground hover:bg-sidebar-accent/60")
          }
        >
          <Settings className="size-4" />
          Settings
        </Link>
      </div>
    </aside>
  );
}
