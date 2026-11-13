# Server development

Server mode stores everything in PostgreSQL 18. `mise run dev:remote` starts the
development Postgres from `compose.dev.yml` (loopback port 54329, override with
`DIFFX_DEV_POSTGRES_PORT`), runs `diffx-server` against it and serves the app
through Vite HMR at `http://localhost:5173`. To run a built binary directly:

```sh
mise run db:up
DIFFX_SERVER_DATABASE_URL=postgres://diffx:diffx@127.0.0.1:54329/diffx?sslmode=disable \
  DIFFX_SERVER_INSECURE_COOKIES=true ./dist/diffx-server
```

Or use the checked-in Compose setup, which bundles its own Postgres:

```sh
mise run docker:up
docker compose logs server
mise run docker:down
```

The same server binary handles authentication, durable admission, workers and
queries in both setups. The Docker tasks stage embedded web assets before
building the image.

| Setting                         | Binary default | Container default | Flag                    |
| ------------------------------- | -------------- | ----------------- | ----------------------- |
| `DIFFX_SERVER_HOST`             | `127.0.0.1`    | `0.0.0.0`         | `--listen HOST:PORT`    |
| `DIFFX_SERVER_PORT`             | `7980`         | `7980`            | `--listen HOST:PORT`    |
| `DIFFX_SERVER_DATABASE_URL`     | Required       | Compose: bundled  | None (env only)         |
| `DIFFX_SERVER_RETENTION_DAYS`   | `7`            | `7`               | `--retention-days DAYS` |
| `DIFFX_SERVER_PUBLIC_URL`       | Unset          | Unset             | `--public-url URL`      |
| `DIFFX_SERVER_INSECURE_COOKIES` | `false`        | Compose: `true`   | `--insecure-cookies`    |
| `DIFFX_SERVER_GOOGLE_CLIENT_ID` | Unset          | Unset             | None                    |
| `DIFFX_SERVER_AUTH_RATE_LIMIT`  | `false`        | `false`           | None (env only)         |
| `DIFFX_SERVER_CLIENT_IP_HEADER` | Unset          | Unset             | None (env only)         |

The server listens on 7980 so a local `diffx [PATH]` (7981–7990) and the server
run side by side. The `DIFFX_SERVER_` prefix keeps deployment variables from
configuring the CLI, and the CLI's `DIFFX_LOCAL_*`/`DIFFX_REMOTE_*` from
configuring the server.

`DIFFX_SERVER_PUBLIC_URL` is the browser-facing https origin (http only with
insecure cookies) used in Google redirects and invite and reset links. An https
public URL, or loopback http for development, also enables Web Push
notifications; the server generates its VAPID keys once and keeps them in the
database.

A public URL also makes the server an OAuth 2.1 authorization server for its
MCP endpoint, so MCP clients connect by URL alone (`claude mcp add --transport
http diffx https://HOST/mcp`) and the user approves them in the browser. The
public URL is the issuer; `/.well-known/oauth-protected-resource/mcp` and
`/.well-known/oauth-authorization-server` describe it, and `/oauth/authorize`,
`/oauth/token`, `/oauth/revoke` and `/oauth/register` implement it. Clients are
public (PKCE S256, no secrets), identified by an https client metadata document
URL the server fetches, or by unauthenticated dynamic registration, bounded
like sign-in. Metadata, token, revocation and registration accept
cross-origin requests and never read cookies; consent needs a browser session
and CSRF token. Document fetches reach only public addresses, follow no
redirects and stop at 5 KiB or five seconds, so the server needs outbound
HTTPS to connect such clients. OAuth tokens work only on `/mcp` and
`/mcp/contexts/{id}`; API tokens keep working everywhere they did. Users see
and disconnect clients in Settings › Connected agents. Without a public URL
none of this exists and MCP takes API tokens only.

Who may join is set in the console's Settings page, not server configuration:
Signup lets anyone create an account from the app without an invite; Password
signup requires an email and password for new accounts. Both default on for
development, tests and self-hosting, upgraded databases included; production
admins turn both off after deploy so accounts come only from invites and sign
in with Google. Existing password accounts keep signing in, using tokens and
resetting passwords. Replicas apply changes within moments; closing takes
effect at once for new accounts. `DIFFX_SERVER_SIGNUP`,
`DIFFX_SERVER_PASSWORD_SIGNIN` and the config keys `signup` and
`passwordSignIn` are removed; the server refuses to start while any is set.
Without Google and with password signup off, each replica logs a warning that
no one new can join. Admins use the app like any user; first-admin setup,
invites, admission settings and account administration live in the console
(`diffx-console`).

Google sign-in needs `DIFFX_SERVER_GOOGLE_CLIENT_ID`,
`DIFFX_SERVER_GOOGLE_CLIENT_SECRET` (environment only, never the config file)
and `DIFFX_SERVER_PUBLIC_URL`; register
`<public URL>/api/v2/auth/google/callback` as the OAuth client's redirect URI.
Create a named token in Settings. Use the
`collector` preset for producers, or `api` for review APIs/MCP. Token secrets are
shown once; expiry defaults to 90 days (selectable 1–365). Tokens cannot manage
accounts. Browser sessions are seven-day opaque cookies with CSRF protection on
unsafe requests. Secure cookies are the binary/image default; explicitly enable
insecure cookies only for HTTP development. Production requires HTTPS.

Password accounts change their password in Settings. A forgotten password needs
a reset link from the console; completing it revokes browser sessions, not API
tokens.

Authentication rate limiting is off by default. `DIFFX_SERVER_AUTH_RATE_LIMIT=true`
allows each client IP 20 sign-in, signup, invite and reset attempts per minute, counted
in Postgres so every replica shares the limit. The listener's peer is the
ingress, so it also requires `DIFFX_SERVER_CLIENT_IP_HEADER`, the header your
trusted ingress overwrites with the single client IP (for example `X-Real-IP`);
startup fails without it. Those routes answer 400 to a missing or invalid value,
such as a list or an address with a port. Never enable it where clients can
reach the server without that ingress, since they could then choose their own
IP. Each process also admits at most four concurrent authentications.

`/healthz` answers 200 while the process serves and never contacts the database;
`/readyz` answers 200 only while the database accepts queries, otherwise 503.
Both are unauthenticated, uncached and reveal no failure detail.

Precedence is defaults → explicit JSON file → env vars → explicit flags. Use
`--config-file FILE` or `DIFFX_SERVER_CONFIG` to select a server config. The file
supports `host`, `port`, `publicUrl`, `retentionDays` and
`insecureCookies`; the database URL is never read from it. The file
must exist and
can be mounted read-only; startup never creates it or a config lock. Personal
CLI config, `DIFFX_RUNTIME_DIR`, producer URL settings and external web assets
do not configure server mode. Restart to apply changes.

Production supplies its own PostgreSQL 18+ through `DIFFX_SERVER_DATABASE_URL`
(for example `postgres://diffx@db.example.com:5432/diffx?sslmode=verify-full`);
pool sizing uses URL parameters such as `pool_max_conns`. The role needs
`CREATE` on the database: the server creates and owns the `diffx` schema and
migrates it at startup. The image carries CA roots for verified TLS. Use a
direct or session-mode pooled connection; transaction-mode poolers (PgBouncer)
break session advisory locks and `LISTEN`. Back up with your database's tooling.
The checked-in Compose file bundles an example Postgres in the `diffx-postgres`
volume, never published; set `DIFFX_POSTGRES_PASSWORD`, or point
`DIFFX_SERVER_DATABASE_URL` at your own server. The server's root filesystem is
read-only and it keeps no local state. `mise run docker:down` preserves the
volume; `docker compose down --volumes` explicitly deletes it. Earlier SQLite
server volumes are not imported: an upgraded server starts empty.

Set `DIFFX_SERVER_PORT=7979 mise run docker:up` for a different published host
port; the container always listens on 7980. Compose also accepts
public URL, retention and insecure-cookie overrides,
from the shell or a git-ignored `.env` beside `compose.yaml`. Compose defaults to
insecure cookies because its HTTP listener is published on loopback; explicitly
set `DIFFX_SERVER_INSECURE_COOKIES=false` behind production HTTPS. The fixed
`diffx` project name keeps one
volume regardless of the checkout directory.
For other settings, use image env overrides or a Compose override file.
The GitHub integration adds the `github` profile: set every
`DIFFX_SERVER_GITHUB_*` variable and `DIFFX_SERVER_FETCHER_SECRET`, mount the
app's private key in an override file, then run
`docker compose --profile github up`. The `fetcher` service (`--target fetcher`,
Alpine with Git 2.44+) keeps mirrors in the `diffx-mirrors` volume, capped by
`DIFFX_FETCHER_DISK_LIMIT_GB` (default 20), and reaches the server's internal
port 7979 only on the `fetch` network.
SIGTERM stops HTTP and workers before releasing storage; unfinished admitted
jobs recover from the durable queue after leases expire.
Replicas are stateless and share one Postgres database; scale them freely.
`LISTEN` needs a direct or session-mode connection URL.

## Admin console

`diffx-console` is the admin console: a separate binary and container
(`docker build --target console .`) on its own origin over the server's
database. It never serves the app or ingests. Configure it only through the
environment:

| Setting                              | Binary default | Container default |
| ------------------------------------ | -------------- | ----------------- |
| `DIFFX_CONSOLE_HOST`                 | `127.0.0.1`    | `0.0.0.0`         |
| `DIFFX_CONSOLE_PORT`                 | `7970`         | `7970`            |
| `DIFFX_CONSOLE_DATABASE_URL`         | Required       | Compose: bundled  |
| `DIFFX_CONSOLE_APP_URL`              | Required       | Compose: app      |
| `DIFFX_CONSOLE_PUBLIC_URL`           | Unset          | Compose: loopback |
| `DIFFX_CONSOLE_INSECURE_COOKIES`     | `false`        | Compose: `true`   |
| `DIFFX_CONSOLE_GOOGLE_CLIENT_ID`     | Unset          | Unset             |
| `DIFFX_CONSOLE_GOOGLE_CLIENT_SECRET` | Unset          | Unset             |
| `DIFFX_CONSOLE_AUTH_RATE_LIMIT`      | `false`        | `false`           |
| `DIFFX_CONSOLE_CLIENT_IP_HEADER`     | Unset          | Unset             |

`DIFFX_CONSOLE_DATABASE_URL` is the server's database; either binary may start
first and migrate it. `DIFFX_CONSOLE_APP_URL` is the app's origin, used in
invite and member reset links. `DIFFX_CONSOLE_PUBLIC_URL` is the console's
origin, used in the setup link, admin reset links and the Google callback;
unset, links use the listen address. While no admin exists, each start logs a
one-time setup link, `<console URL>/setup#t=…`, valid for 24 hours; a restart
replaces it. It creates the first admin with an email and password or, when
the console has its own Google client, with a Google account whose email Google
verified, whatever the app's settings. If
that Google account already has an app account, setup makes it the admin.

Admins sign in to the console with their password, or with Google when the
console has its own OAuth client: set both Google variables and register
`<console URL>/api/v1/auth/google/callback`. The console admits only enabled
admins and never creates accounts. Admins invite members (invites never grant
admin), set signup and password signup in Settings, promote and demote,
disable, enable and delete other accounts, and issue password reset links; no change may leave zero enabled admins, and admins never act on
themselves. Disabling or deleting stops the account on every server replica.
Rate limiting works as on the server, with the console's own variables.

A locked-out admin or user gets a reset link offline, with the console's
environment; servers may keep running:

```sh
diffx-console user reset-password --email alice@example.com
```

Admins' links open the console, other accounts' open the app. Completing a
link revokes browser sessions, not API tokens.

The console app (`apps/console`) is a document-style React app on
`packages/ui` and the `packages/console-api` client. Signed out, `/setup`
(while no admin exists) and `/reset-password` render their pages and every
other path renders sign-in. Signed in, `/accounts`, `/invites`, `/settings`
and `/admin` render under the console header and other paths redirect to `/accounts`. Its
Playwright specs (`apps/console/test/browser`) run the built `diffx-server` and
`diffx-console` on one fresh database per worker. `mise run dev:console` serves
it with Vite HMR at `localhost:5174`, beside `mise run dev:remote` at
`localhost:5173`.
