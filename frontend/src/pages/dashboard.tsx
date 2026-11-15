import { useCallback, useEffect, useState, type ReactNode } from "react";
import { motion, type Variants } from "framer-motion";
import { Clock, Fingerprint, KeyRound, LogOut, RefreshCw, ShieldCheck, Timer, Zap } from "lucide-react";
import { toast } from "sonner";
import { useAuth } from "@/lib/auth";
import { authApi, type SessionInfo } from "@/lib/api";
import { parseUA } from "@/lib/ua";
import { formatDateTime, relativeTime } from "@/lib/utils";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { DeviceIcon } from "@/components/device-icon";
import { AppHeader } from "@/components/app-header";
import { WeatherCard } from "@/components/weather-card";
import { GoldCard } from "@/components/gold-card";

const container: Variants = { hidden: {}, show: { transition: { staggerChildren: 0.06 } } };
const item: Variants = {
  hidden: { opacity: 0, y: 10 },
  show: { opacity: 1, y: 0, transition: { duration: 0.45, ease: [0.16, 1, 0.3, 1] } },
};

const statusMeta: Record<string, { label: string; variant: BadgeProps["variant"] }> = {
  active: { label: "Active", variant: "success" },
  signed_out: { label: "Signed out", variant: "outline" },
  superseded: { label: "Replaced", variant: "warning" },
  expired: { label: "Expired", variant: "outline" },
  password_reset: { label: "Password reset", variant: "outline" },
  token_reuse: { label: "Revoked · reuse", variant: "destructive" },
};

function sessionStatus(s: SessionInfo) {
  if (!s.revoked_at) return statusMeta.active;
  return statusMeta[s.revoke_reason ?? ""] ?? { label: "Revoked", variant: "outline" as const };
}

export default function DashboardPage() {
  const { user, logout } = useAuth();
  const [current, setCurrent] = useState<SessionInfo | null>(null);
  const [history, setHistory] = useState<SessionInfo[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [signingOut, setSigningOut] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [c, h] = await Promise.all([authApi.currentSession(), authApi.sessions()]);
      setCurrent(c.session);
      setHistory(h.sessions);
    } catch {
      /* session-revoked is handled globally */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const onLogout = async () => {
    setSigningOut(true);
    try {
      await logout();
      toast("Signed out");
    } finally {
      setSigningOut(false);
    }
  };

  const firstName = user?.name.split(" ")[0] ?? "";
  const ua = parseUA(current?.user_agent ?? navigator.userAgent);

  return (
    <div className="min-h-dvh bg-background">
      <AppHeader />
      <motion.main
        variants={container}
        initial="hidden"
        animate="show"
        className="mx-auto w-full max-w-5xl px-4 pb-20 pt-8 sm:px-6 sm:pt-12"
      >
        <motion.div variants={item} className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h1 className="text-2xl font-semibold tracking-[-0.03em] sm:text-[28px]">Good to see you, {firstName}</h1>
            <p className="mt-1.5 text-sm text-muted-foreground">
              Your account is protected by single-session enforcement.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading} className="self-start">
            <RefreshCw className={loading ? "animate-spin" : ""} />
            Refresh
          </Button>
        </motion.div>

        <motion.div variants={item} className="mt-8 grid gap-3 sm:grid-cols-3">
          <Stat
            icon={<ShieldCheck />}
            label="Session status"
            value={
              <span className="inline-flex items-center gap-2">
                <span className="relative flex h-2 w-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-50" />
                  <span className="relative inline-flex h-2 w-2 rounded-full bg-success" />
                </span>
                Active
              </span>
            }
          />
          <Stat icon={<Clock />} label="Signed in" value={current ? relativeTime(current.created_at) : null} />
          <Stat icon={<Timer />} label="Session renews until" value={current ? relativeTime(current.expires_at) : null} />
        </motion.div>

        <motion.div variants={item} className="mt-6">
          <WeatherCard />
        </motion.div>

        <motion.div variants={item} className="mt-6">
          <GoldCard />
        </motion.div>

        <div className="mt-6 grid gap-6 lg:grid-cols-5">
          <motion.div variants={item} className="lg:col-span-3">
            <Card className="h-full">
              <CardHeader>
                <CardTitle>This device</CardTitle>
                <CardDescription>The only device currently signed in to your account.</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="flex items-center gap-4 rounded-lg border bg-muted/30 p-4">
                  <DeviceIcon kind={ua.kind} className="h-12 w-12" />
                  <div className="min-w-0">
                    <p className="flex items-center gap-2 truncate font-medium">
                      {ua.label}
                      <Badge variant="success">Current</Badge>
                    </p>
                    <p className="mt-0.5 truncate text-sm text-muted-foreground">{user?.email}</p>
                  </div>
                </div>
                <dl className="mt-5 grid grid-cols-2 gap-x-6 gap-y-4 text-sm">
                  <Detail label="IP address" value={current?.ip_address || "—"} mono />
                  <Detail label="Session ID" value={current ? current.id.slice(0, 8) + "…" : null} mono />
                  <Detail label="Signed in" value={current ? formatDateTime(current.created_at) : null} />
                  <Detail label="Last refreshed" value={current ? relativeTime(current.last_seen_at) : null} />
                </dl>
              </CardContent>
              <CardFooter className="justify-between gap-3">
                <p className="text-xs text-muted-foreground">Signing out revokes all tokens instantly.</p>
                <Button variant="outline" size="sm" onClick={onLogout} loading={signingOut}>
                  {!signingOut && <LogOut />}
                  Sign out
                </Button>
              </CardFooter>
            </Card>
          </motion.div>

          <motion.div variants={item} className="lg:col-span-2">
            <Card className="h-full">
              <CardHeader>
                <CardTitle>Security</CardTitle>
                <CardDescription>How your session is protected.</CardDescription>
              </CardHeader>
              <CardContent className="grid gap-4">
                <Feature
                  icon={<Fingerprint />}
                  title="One active session"
                  body="Signing in elsewhere requires ending this session first, enforced transactionally in PostgreSQL."
                />
                <Feature
                  icon={<Zap />}
                  title="Instant revocation"
                  body="Every request is validated against Redis, so revoked tokens stop working immediately."
                />
                <Feature
                  icon={<KeyRound />}
                  title="Rotating refresh tokens"
                  body="Refresh tokens are single-use. Reuse of an old token revokes the session."
                />
              </CardContent>
            </Card>
          </motion.div>
        </div>

        <motion.div variants={item} className="mt-6">
          <Card>
            <CardHeader>
              <CardTitle>Session history</CardTitle>
              <CardDescription>Recent sign-ins to your account. Only one can be active at a time.</CardDescription>
            </CardHeader>
            <CardContent className="px-0 pb-2 sm:px-0">
              <ul className="divide-y border-t">
                {history === null
                  ? Array.from({ length: 3 }).map((_, i) => (
                      <li key={i} className="flex items-center gap-3 px-5 py-3.5 sm:px-6">
                        <Skeleton className="h-10 w-10 rounded-lg" />
                        <div className="grid flex-1 gap-2">
                          <Skeleton className="h-3.5 w-40" />
                          <Skeleton className="h-3 w-56" />
                        </div>
                      </li>
                    ))
                  : history.map((s) => {
                      const p = parseUA(s.user_agent);
                      const st = sessionStatus(s);
                      return (
                        <li
                          key={s.id}
                          className="flex items-center gap-3 px-5 py-3.5 transition-colors hover:bg-muted/30 sm:px-6"
                        >
                          <DeviceIcon kind={p.kind} />
                          <div className="min-w-0 flex-1">
                            <p className="flex items-center gap-2 truncate text-sm font-medium">
                              {p.label}
                              {s.current && <span className="text-xs font-normal text-muted-foreground">· this device</span>}
                            </p>
                            <p className="truncate text-xs text-muted-foreground">
                              <span className="font-mono">{s.ip_address || "unknown"}</span> · {formatDateTime(s.created_at)}
                            </p>
                          </div>
                          <Badge variant={st.variant}>{st.label}</Badge>
                        </li>
                      );
                    })}
              </ul>
            </CardContent>
          </Card>
        </motion.div>
      </motion.main>
    </div>
  );
}

function Stat({ icon, label, value }: { icon: ReactNode; label: string; value: ReactNode | null }) {
  return (
    <div className="rounded-xl border bg-card p-4 shadow-sm">
      <div className="flex items-center gap-2 text-xs text-muted-foreground [&_svg]:h-3.5 [&_svg]:w-3.5">
        {icon}
        {label}
      </div>
      <div className="mt-2 text-[15px] font-medium tracking-tight">{value ?? <Skeleton className="h-5 w-24" />}</div>
    </div>
  );
}

function Detail({ label, value, mono }: { label: string; value: string | null; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={`mt-1 truncate ${mono ? "font-mono text-[13px]" : ""}`}>
        {value ?? <Skeleton className="h-4 w-24" />}
      </dd>
    </div>
  );
}

function Feature({ icon, title, body }: { icon: ReactNode; title: string; body: string }) {
  return (
    <div className="flex gap-3">
      <div className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md border bg-muted/50 [&_svg]:h-3.5 [&_svg]:w-3.5">
        {icon}
      </div>
      <div>
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-0.5 text-[13px] leading-5 text-muted-foreground">{body}</p>
      </div>
    </div>
  );
}
