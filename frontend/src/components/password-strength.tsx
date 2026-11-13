import { cn } from "@/lib/utils";

export function scorePassword(pw: string): number {
  if (!pw) return 0;
  let score = 0;
  if (pw.length >= 8) score++;
  if (pw.length >= 12) score++;
  if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) score++;
  if (/\d/.test(pw) && /[^A-Za-z0-9]/.test(pw)) score++;
  return Math.min(4, Math.max(1, score));
}

const labels = ["", "Weak", "Fair", "Good", "Strong"];
const colors = ["", "bg-destructive", "bg-warning", "bg-foreground/60", "bg-success"];

export function PasswordStrength({ password }: { password: string }) {
  const score = scorePassword(password);
  return (
    <div className="flex items-center gap-3" aria-live="polite">
      <div className="grid flex-1 grid-cols-4 gap-1">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="h-1 overflow-hidden rounded-full bg-muted">
            <div
              className={cn(
                "h-full rounded-full transition-all duration-300 ease-out",
                i <= score ? cn(colors[score], "w-full") : "w-0",
              )}
            />
          </div>
        ))}
      </div>
      <span className="w-12 text-right text-[11px] text-muted-foreground">{password ? labels[score] : ""}</span>
    </div>
  );
}
