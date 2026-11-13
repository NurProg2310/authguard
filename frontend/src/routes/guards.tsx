import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useAuth } from "@/lib/auth";
import { LogoMark } from "@/components/logo";

function Splash() {
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background">
      <div className="flex flex-col items-center gap-4">
        <LogoMark className="h-9 w-9 animate-pulse" />
        <div className="h-0.5 w-24 overflow-hidden rounded-full bg-muted">
          <div className="h-full w-1/3 animate-[shimmer_1s_ease-in-out_infinite] rounded-full bg-foreground/40" />
        </div>
      </div>
    </div>
  );
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth();
  const location = useLocation();
  if (status === "loading") return <Splash />;
  if (status === "unauthenticated") return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  return <>{children}</>;
}

export function GuestOnly({ children }: { children: ReactNode }) {
  const { status } = useAuth();
  if (status === "loading") return <Splash />;
  if (status === "authenticated") return <Navigate to="/dashboard" replace />;
  return <>{children}</>;
}
