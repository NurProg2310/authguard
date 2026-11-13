import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  ApiError,
  authApi,
  onAuthEvent,
  refreshSession,
  revokeMessage,
  setAccessToken,
  type AuthPayload,
  type User,
} from "@/lib/api";

type Status = "loading" | "authenticated" | "unauthenticated";

interface AuthContextValue {
  status: Status;
  user: User | null;
  sessionId: string | null;
  notice: string | null;
  clearNotice: () => void;
  login: (email: string, password: string) => Promise<void>;
  register: (name: string, email: string, password: string) => Promise<void>;
  takeover: (token: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

interface State {
  status: Status;
  user: User | null;
  sessionId: string | null;
  expiresAt: string | null;
}

const signedOut: State = { status: "unauthenticated", user: null, sessionId: null, expiresAt: null };
const HEARTBEAT_MS = 15_000;

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<State>({ ...signedOut, status: "loading" });
  const [notice, setNotice] = useState<string | null>(null);
  const channel = useRef<BroadcastChannel | null>(null);

  const apply = useCallback((p: AuthPayload) => {
    setAccessToken(p.access_token);
    setState({ status: "authenticated", user: p.user, sessionId: p.session_id, expiresAt: p.expires_at });
  }, []);

  const clear = useCallback((reason?: string) => {
    setAccessToken(null);
    setState(signedOut);
    if (reason) setNotice(revokeMessage(reason));
  }, []);

  // Restore the session from the refresh cookie on first load.
  useEffect(() => {
    refreshSession()
      .then(apply)
      .catch((e) => clear(e instanceof ApiError && e.code === "SESSION_REVOKED" ? e.reason : undefined));
  }, [apply, clear]);

  // Events raised by the API client (revocation detected mid-request, silent refresh).
  useEffect(
    () =>
      onAuthEvent((e) => {
        if (e.type === "session-revoked") clear(e.reason);
        else apply(e.payload);
      }),
    [apply, clear],
  );

  // Keep tabs of the same browser in sync.
  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;
    const bc = new BroadcastChannel("authguard");
    channel.current = bc;
    bc.onmessage = (ev) => {
      if (ev.data?.type === "logout") clear();
      if (ev.data?.type === "login") refreshSession().then(apply).catch(() => clear());
    };
    return () => {
      bc.close();
      channel.current = null;
    };
  }, [apply, clear]);

  // Proactively refresh the access token shortly before it expires.
  useEffect(() => {
    if (state.status !== "authenticated" || !state.expiresAt) return;
    const delay = Math.max(5_000, new Date(state.expiresAt).getTime() - Date.now() - 60_000);
    const t = window.setTimeout(() => {
      refreshSession()
        .then(apply)
        .catch((e) => {
          if (e instanceof ApiError && e.status === 401) clear(e.reason ?? "expired");
        });
    }, delay);
    return () => window.clearTimeout(t);
  }, [state.status, state.expiresAt, apply, clear]);

  // Heartbeat: detects a takeover from another device within seconds, and
  // instantly when the tab regains focus.
  useEffect(() => {
    if (state.status !== "authenticated") return;
    const check = () => {
      if (document.visibilityState === "visible") authApi.currentSession().catch(() => {});
    };
    const i = window.setInterval(check, HEARTBEAT_MS);
    window.addEventListener("focus", check);
    document.addEventListener("visibilitychange", check);
    return () => {
      window.clearInterval(i);
      window.removeEventListener("focus", check);
      document.removeEventListener("visibilitychange", check);
    };
  }, [state.status]);

  const afterLogin = useCallback(
    (p: AuthPayload) => {
      apply(p);
      setNotice(null);
      channel.current?.postMessage({ type: "login" });
    },
    [apply],
  );

  const value = useMemo<AuthContextValue>(
    () => ({
      status: state.status,
      user: state.user,
      sessionId: state.sessionId,
      notice,
      clearNotice: () => setNotice(null),
      login: async (email, password) => afterLogin(await authApi.login(email, password)),
      register: async (name, email, password) => afterLogin(await authApi.register(name, email, password)),
      takeover: async (token) => afterLogin(await authApi.takeover(token)),
      logout: async () => {
        try {
          await authApi.logout();
        } finally {
          clear();
          channel.current?.postMessage({ type: "logout" });
        }
      },
    }),
    [state, notice, afterLogin, clear],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}
