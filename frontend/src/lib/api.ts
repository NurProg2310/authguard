// Thin API client.
// - The access token lives only in memory (never localStorage) to limit XSS blast radius.
// - The refresh token is an HttpOnly, SameSite=Strict cookie scoped to /api/auth.
// - Refreshes are single-flight per tab and serialised across tabs with the Web
//   Locks API, so rotating refresh tokens never race each other.

export interface User {
  id: string;
  email: string;
  name: string;
  created_at: string;
}

export interface SessionInfo {
  id: string;
  user_agent: string;
  ip_address: string;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
  revoked_at: string | null;
  revoke_reason: RevokeReason | null;
  current: boolean;
}

export type RevokeReason = "signed_out" | "superseded" | "expired" | "password_reset" | "token_reuse" | "invalid";

export interface AuthPayload {
  user: User;
  access_token: string;
  token_type: "Bearer";
  expires_at: string;
  session_id: string;
}

export interface SessionConflict {
  active_session: SessionInfo;
  takeover_token: string;
  takeover_expires_at: string;
}

interface ErrorBody {
  code?: string;
  message?: string;
  reason?: string;
  fields?: Record<string, string>;
  [k: string]: unknown;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public body: ErrorBody = {},
  ) {
    super(message);
  }
  get fields(): Record<string, string> {
    return this.body.fields ?? {};
  }
  get reason(): string | undefined {
    return this.body.reason;
  }
}

type AuthEvent = { type: "session-revoked"; reason: string } | { type: "refreshed"; payload: AuthPayload };
const listeners = new Set<(e: AuthEvent) => void>();

export function onAuthEvent(fn: (e: AuthEvent) => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
function emit(e: AuthEvent) {
  listeners.forEach((l) => l(e));
}

let accessToken: string | null = null;
export function setAccessToken(token: string | null) {
  accessToken = token;
}

async function send(path: string, init: { method?: string; json?: unknown; auth?: boolean } = {}) {
  const headers: Record<string, string> = {
    Accept: "application/json",
    "X-AuthGuard-CSRF": "1",
  };
  if (init.json !== undefined) headers["Content-Type"] = "application/json";
  if (init.auth !== false && accessToken) headers.Authorization = `Bearer ${accessToken}`;
  try {
    return await fetch(`/api${path}`, {
      method: init.method ?? "GET",
      headers,
      credentials: "same-origin",
      body: init.json !== undefined ? JSON.stringify(init.json) : undefined,
    });
  } catch {
    throw new ApiError(0, "NETWORK", "Can't reach the server. Check your connection.");
  }
}

async function parse<T>(res: Response): Promise<T> {
  if (res.status === 204) return undefined as T;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const e: ErrorBody = body?.error ?? {};
    throw new ApiError(res.status, e.code ?? "UNKNOWN", e.message ?? "Something went wrong", e);
  }
  return body as T;
}

let inflight: Promise<AuthPayload> | null = null;

export function refreshSession(): Promise<AuthPayload> {
  if (inflight) return inflight;
  const run = async (): Promise<AuthPayload> => {
    for (let attempt = 0; ; attempt++) {
      try {
        const p = await parse<AuthPayload>(await send("/auth/refresh", { method: "POST", auth: false }));
        accessToken = p.access_token;
        return p;
      } catch (e) {
        // Another tab rotated the cookie a moment ago; the browser now has the new one.
        if (e instanceof ApiError && e.code === "REFRESH_RACE" && attempt < 2) {
          await new Promise((r) => setTimeout(r, 250));
          continue;
        }
        throw e;
      }
    }
  };
  const locked: Promise<AuthPayload> =
    typeof navigator !== "undefined" && "locks" in navigator
      ? navigator.locks.request("authguard-refresh", run)
      : run();
  inflight = locked.finally(() => {
    inflight = null;
  });
  return inflight;
}

/** Authenticated request with transparent access-token refresh. */
export async function request<T>(path: string, init: { method?: string; json?: unknown } = {}): Promise<T> {
  let res = await send(path, init);
  if (res.status === 401) {
    const body: ErrorBody = (await res.clone().json().catch(() => ({})))?.error ?? {};
    if (body.code === "SESSION_REVOKED") {
      accessToken = null;
      emit({ type: "session-revoked", reason: body.reason ?? "invalid" });
    } else if (body.code === "TOKEN_EXPIRED" || body.code === "INVALID_TOKEN" || body.code === "MISSING_TOKEN") {
      try {
        const p = await refreshSession();
        emit({ type: "refreshed", payload: p });
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          accessToken = null;
          emit({ type: "session-revoked", reason: e.reason ?? "expired" });
        }
        throw e;
      }
      res = await send(path, init);
    }
  }
  return parse<T>(res);
}

export const authApi = {
  login: (email: string, password: string) =>
    send("/auth/login", { method: "POST", json: { email, password }, auth: false }).then((r) => parse<AuthPayload>(r)),
  register: (name: string, email: string, password: string) =>
    send("/auth/register", { method: "POST", json: { name, email, password }, auth: false }).then((r) =>
      parse<AuthPayload>(r),
    ),
  takeover: (takeover_token: string) =>
    send("/auth/takeover", { method: "POST", json: { takeover_token }, auth: false }).then((r) =>
      parse<AuthPayload>(r),
    ),
  logout: () => send("/auth/logout", { method: "POST" }).then((r) => parse<void>(r)),
  forgotPassword: (email: string) =>
    send("/auth/forgot-password", { method: "POST", json: { email }, auth: false }).then((r) =>
      parse<{ message: string }>(r),
    ),
  resetPassword: (token: string, password: string) =>
    send("/auth/reset-password", { method: "POST", json: { token, password }, auth: false }).then((r) =>
      parse<{ message: string }>(r),
    ),
  currentSession: () => request<{ session: SessionInfo }>("/sessions/current"),
  sessions: () => request<{ sessions: SessionInfo[] }>("/sessions"),
};

export function conflictFrom(err: unknown): SessionConflict | null {
  if (err instanceof ApiError && err.code === "SESSION_ACTIVE") {
    const b = err.body as unknown as SessionConflict;
    if (b.takeover_token && b.active_session) return b;
  }
  return null;
}

export function revokeMessage(reason: string): string {
  switch (reason) {
    case "superseded":
      return "You were signed out because your account was signed in on another device.";
    case "password_reset":
      return "Your password was changed, so all sessions were ended. Please sign in again.";
    case "token_reuse":
      return "We noticed unusual activity and ended your session to keep your account safe.";
    case "expired":
      return "Your session expired. Please sign in again.";
    case "signed_out":
      return "You've been signed out.";
    default:
      return "Your session has ended. Please sign in again.";
  }
}

export interface WeatherForecast {
  latitude: number;
  longitude: number;
  timezone: string;
  current: {
    time: string;
    temperature: number;
    apparent_temperature: number;
    humidity: number;
    wind_speed: number;
    weather_code: number;
    is_day: boolean;
  };
  daily: { date: string; weather_code: number; temp_max: number; temp_min: number }[];
}

export interface Place {
  name: string;
  region?: string;
  country?: string;
  latitude: number;
  longitude: number;
}

export const weatherApi = {
  forecast: (lat: number, lon: number) =>
    request<{ forecast: WeatherForecast }>(`/weather?lat=${lat.toFixed(4)}&lon=${lon.toFixed(4)}`),
  search: (q: string, lang: string) =>
    request<{ places: Place[] }>(`/weather/search?q=${encodeURIComponent(q)}&lang=${encodeURIComponent(lang)}`),
};
