import { cn } from "@/lib/utils";

export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={cn("h-7 w-7", className)} aria-hidden>
      <rect width="32" height="32" rx="8" className="fill-foreground" />
      <path
        d="M16 7l7 3v5.5c0 4.6-3 8.4-7 9.5-4-1.1-7-4.9-7-9.5V10l7-3z"
        fill="none"
        className="stroke-background"
        strokeWidth="2"
        strokeLinejoin="round"
      />
      <path
        d="M12.8 16.2l2.2 2.2 4.2-4.4"
        fill="none"
        className="stroke-background"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function Logo({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <LogoMark />
      <span className="text-[15px] font-semibold tracking-tight">AuthGuard</span>
    </span>
  );
}
