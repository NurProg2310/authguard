# AuthGuard

Full-stack authentication that allows **one active session per user**. If an account is already signed in somewhere, a second sign-in is blocked. The person signing in can choose to end the other session, and every token from that session stops working right away.

| Layer | Stack |
| --- | --- |
| Backend | Go 1.26, Gin, pgx (PostgreSQL 17), go-redis (Redis 7.4), golang-jwt, Argon2id |
| Frontend | React 18, TypeScript, Vite, Tailwind CSS, shadcn/ui (Radix), Framer Motion |
| Infra | Docker Compose, Nginx (static files + reverse proxy), Mailpit (development mail catcher) |

## Quick start

```bash
cp .env.example .env          # then replace every secret (see the commands inside the file)
docker compose up -d --build
```

- App: http://localhost:8080
- Password-reset emails (Mailpit): http://localhost:8025

The backend applies database migrations on startup. Migrations hold a PostgreSQL advisory lock, so several replicas can start at once safely.

## How the single-session rule works

```
POST /api/auth/login ──▶ verify password (Argon2id)
                          │
                          ▼  BEGIN
              SELECT … FROM users WHERE id=$1 FOR UPDATE   ◀─ concurrent logins for the same user queue here
              UPDATE sessions SET revoked … WHERE expires_at <= now()
              SELECT active session
                 ├─ none ─────▶ INSERT session ─▶ COMMIT ─▶ SET redis ag:session:<id>
                 └─ exists ───▶ 409 SESSION_ACTIVE + single-use takeover token (5 min)

POST /api/auth/takeover ─▶ same transaction, but: revoke old row (reason=superseded),
                           DEL redis key (before AND after commit), INSERT new session
```

- **Enforced in the database.** The partial unique index `sessions_one_active_per_user ON sessions(user_id) WHERE revoked_at IS NULL` means no code path can leave two live sessions for one user. The row lock on the user serialises logins, so the index is a backstop and is not expected to trip.
- **Revocation takes effect immediately.** Every access token (a JWT, 15-minute lifetime) carries a `sid` claim. The auth middleware accepts a token only while `ag:session:<sid>` exists in Redis. Deleting that key cuts off every outstanding token at once. On a Redis miss, the middleware checks PostgreSQL and re-populates the key. If Redis restarts, nobody is locked out and no revoked session gets back in.
- **Logged-out devices find out within seconds.** The old device runs a heartbeat every 15 seconds and also checks when its window regains focus. When the check fails, it goes to `/login` with the message "signed in on another device".
- **Takeover without a second password prompt.** The 409 response includes a short-lived JWT of type `takeover`, bound to the user and used once (Redis `SETNX`). A password reset after the token was issued invalidates it.

## Token model

| Token | Where it lives | Lifetime | Notes |
| --- | --- | --- | --- |
| Access (JWT, HS256) | JavaScript memory only | 15 minutes | `typ=access`, `sid`, `jti`. Checked against Redis on every request. |
| Refresh (opaque) | `HttpOnly; SameSite=Strict; Path=/api/auth` cookie | 7 days sliding, 30 days maximum | Format `<session-id>.<256-bit secret>`. Only its SHA-256 hash is stored. **A new refresh token is issued on every use.** |

- **Reuse detection.** Presenting the *previous* refresh token more than 30 seconds after it was replaced revokes the session (`token_reuse`), on the assumption that the token was stolen.
- **Several tabs.** Tabs serialise refreshes through the Web Locks API, and a `BroadcastChannel` keeps sign-in and sign-out in sync across tabs.
- **CSRF.** Every state-changing request must carry the custom header `X-AuthGuard-CSRF: 1`. A cross-origin page can't send that header without a CORS preflight, which the API never approves. This sits on top of the `SameSite=Strict` cookie.

## Security checklist

- Passwords are hashed with Argon2id (m=19 MiB, t=2, p=1, the OWASP baseline). A semaphore caps how many hashes run at once. Logins for unknown emails hash a dummy password so response time doesn't reveal whether an account exists.
- Rate limits are applied in two layers:
  - Redis fixed windows per IP on every auth route, plus per email for login and forgot-password.
  - Nginx `limit_req` as an outer layer.
- Password reset:
  - Tokens are single-use, 256-bit, stored only as hashes, and expire after 30 minutes.
  - Requesting a new reset invalidates older tokens.
  - A successful reset **revokes every session**.
  - The forgot-password endpoint responds the same way and runs its work in the background, so neither the response nor its timing reveals whether an account exists.
- Nginx sends these headers: a strict CSP (no inline scripts), `X-Frame-Options: DENY`, `nosniff`, a Referrer-Policy, a Permissions-Policy and COOP/CORP.
- Containers:
  - The backend runs on distroless as a non-root user with a read-only filesystem and `no-new-privileges`.
  - Postgres and Redis sit on an `internal` Docker network that the host can't reach.

## API

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| POST | `/api/auth/register` | none | `{name,email,password}` creates the account and signs in → 201 |
| POST | `/api/auth/login` | none | `{email,password}` → 200, or **409 `SESSION_ACTIVE`** with `active_session`, `takeover_token` |
| POST | `/api/auth/takeover` | none | `{takeover_token}` ends the other session and signs in |
| POST | `/api/auth/refresh` | cookie | Issues a new refresh token and access token |
| POST | `/api/auth/logout` | bearer or cookie | Revokes the session and clears the cookie |
| POST | `/api/auth/forgot-password` | none | `{email}` → always 202 |
| POST | `/api/auth/reset-password` | none | `{token,password}` |
| GET | `/api/auth/me` | bearer | Current user |
| GET | `/api/sessions/current` | bearer | Current session (also used as the heartbeat) |
| GET | `/api/sessions` | bearer | The 15 most recent sessions, with revoke reasons |
| GET | `/api/weather?lat=&lon=` | bearer | Current weather and a 5-day forecast from Open-Meteo, cached in Redis for 10 minutes |
| GET | `/api/weather/search?q=&lang=` | bearer | City search through Open-Meteo geocoding, cached for 24 hours |
| GET | `/api/gold` | bearer | Gold prices from the National Bank of Kazakhstan, spot XAU/USD, COMEX, Shanghai Gold Exchange and Moscow Exchange, converted to ₸/g and $/oz at NBK rates. Cached for 5 minutes. |
| GET | `/api/gold/history` | bearer | About one year of NBK official ₸/g prices for the dashboard chart. Cached for 3 hours. |
| GET | `/api/health` | none | Checks PostgreSQL and Redis |

Errors use the shape `{"error":{"code":"…","message":"…", …}}`. A `401 SESSION_REVOKED` response also includes `reason`, which is one of `superseded`, `password_reset`, `token_reuse`, `expired` or `signed_out`.

## Project layout

```
backend/
  cmd/server/            entrypoint (+ `server healthcheck` for the distroless HEALTHCHECK)
  internal/config/       environment configuration
  internal/database/     pgx pool, embedded SQL migrations + runner
  internal/cache/        Redis client
  internal/security/     Argon2id, JWT manager, random/hashed tokens
  internal/service/      auth service: single-session transaction, refresh rotation, recovery
  internal/httpapi/      Gin router, middleware (auth, CSRF, rate limiting), handlers
frontend/
  src/lib/               API client (silent refresh, cross-tab locks), auth context
  src/components/ui/     shadcn/ui primitives
  src/pages/             login, register, forgot/reset password, dashboard
nginx/                   reverse proxy, security headers, SPA serving (multi-stage builds the frontend)
```

## Configuration

All settings are environment variables. `.env.example` lists them with their defaults.

| Variable | Default | Purpose |
| --- | --- | --- |
| `JWT_SECRET` | — (required, ≥ 32 bytes) | HMAC signing key |
| `ACCESS_TOKEN_TTL` | `15m` | Access-token lifetime |
| `REFRESH_TOKEN_TTL` | `168h` | Idle timeout. A session expires if it isn't refreshed for this long. |
| `SESSION_MAX_LIFETIME` | `720h` | Absolute cap on a session's lifetime |
| `COOKIE_SECURE` | `false` in compose | **Set to `true` behind HTTPS.** The backend refuses to start with `false` in production for any non-localhost `APP_URL`. |
| `APP_URL` | `http://localhost:8080` | Used to build password-reset links |
| `SMTP_*` | Mailpit | Point these at a real provider in production |

## Going to production

1. Put TLS in front of Nginx, either by terminating it there (add a `listen 443 ssl` block) or at a load balancer.
2. Set `COOKIE_SECURE=true` and `APP_URL=https://your-domain`.
3. Uncomment the HSTS header in `nginx/security-headers.conf`.
4. Remove the `mailpit` service and configure `SMTP_*`.
5. Use managed or backed-up volumes for PostgreSQL, and generate fresh secrets.

## Local development without Docker for the app

```bash
docker compose up -d postgres redis mailpit     # infrastructure only (see note below)
cd backend && go test ./... && go run ./cmd/server
cd frontend && npm install && npm run dev       # Vite proxies /api to VITE_API_PROXY (default http://localhost:8081)
```

The `backend` network is `internal`, so to run the backend on your host you need to publish the Postgres and Redis ports through a `docker-compose.override.yml`.
