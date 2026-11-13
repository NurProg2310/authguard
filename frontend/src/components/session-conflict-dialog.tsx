import { useEffect, useState } from "react";
import { ShieldAlert } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { DeviceIcon } from "@/components/device-icon";
import type { SessionConflict } from "@/lib/api";
import { parseUA } from "@/lib/ua";
import { relativeTime } from "@/lib/utils";

interface Props {
  conflict: SessionConflict | null;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

function useCountdown(until: string | undefined) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!until) return;
    const i = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(i);
  }, [until]);
  if (!until) return 0;
  return Math.max(0, Math.floor((new Date(until).getTime() - now) / 1000));
}

export function SessionConflictDialog({ conflict, pending, onCancel, onConfirm }: Props) {
  const remaining = useCountdown(conflict?.takeover_expires_at);
  const s = conflict?.active_session;
  const ua = parseUA(s?.user_agent ?? "");
  const expired = !!conflict && remaining === 0;

  return (
    <Dialog open={!!conflict} onOpenChange={(open) => !open && !pending && onCancel()}>
      <DialogContent hideClose onEscapeKeyDown={(e) => pending && e.preventDefault()}>
        <DialogHeader>
          <div className="mb-2 flex h-10 w-10 items-center justify-center rounded-full border border-warning/30 bg-warning/10 text-warning">
            <ShieldAlert className="h-5 w-5" />
          </div>
          <DialogTitle>You're signed in somewhere else</DialogTitle>
          <DialogDescription>
            Your account allows one active session at a time. Continue here to sign out the other device immediately.
          </DialogDescription>
        </DialogHeader>

        {s && (
          <div className="flex items-center gap-3 rounded-lg border bg-muted/40 p-3">
            <DeviceIcon kind={ua.kind} />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{ua.label}</p>
              <p className="truncate text-xs text-muted-foreground">
                {s.ip_address || "Unknown IP"} · signed in {relativeTime(s.created_at)}
              </p>
            </div>
            <span className="relative flex h-2 w-2 shrink-0" aria-label="Active">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-60" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-success" />
            </span>
          </div>
        )}

        <p className="text-xs text-muted-foreground">
          {expired ? (
            "This request has expired. Cancel and sign in again."
          ) : (
            <>
              Not you? Cancel and reset your password. This option expires in{" "}
              <span className="font-mono tabular-nums text-foreground">
                {Math.floor(remaining / 60)}:{String(remaining % 60).padStart(2, "0")}
              </span>
              .
            </>
          )}
        </p>

        <DialogFooter className="pt-1">
          <Button variant="outline" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={onConfirm} loading={pending} disabled={expired}>
            Sign out other device & continue
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
