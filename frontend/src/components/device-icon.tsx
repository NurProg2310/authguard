import { Monitor, Smartphone, Tablet } from "lucide-react";
import type { DeviceKind } from "@/lib/ua";
import { cn } from "@/lib/utils";

export function DeviceIcon({ kind, className }: { kind: DeviceKind; className?: string }) {
  const Icon = kind === "mobile" ? Smartphone : kind === "tablet" ? Tablet : Monitor;
  return (
    <div
      className={cn(
        "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border bg-gradient-to-b from-background to-muted/60 text-foreground/80 shadow-sm",
        className,
      )}
    >
      <Icon className="h-[18px] w-[18px]" />
    </div>
  );
}
