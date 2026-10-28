# GitHub integration: pending live testing

The GitHub pull request integration shipped without a real GitHub App. Every
GitHub call was exercised against fakes; nothing has talked to github.com with
real credentials. This note lists what remains to verify once an app exists,
and the known risks to watch. Remove it when live testing is done.

User setup: [GitHub pull requests](../docs-website/src/pages/github-pull-requests.md).
Design: `docs/system.md` → GitHub pull requests.

## Verified without GitHub

- Webhook HMAC, delivery deduplication and coalesced refresh intent
  (`internal/githubsync`, real binary with signed payloads).
- OAuth state binding to the starting account, sealed tokens, refresh
  rotation and reconnect on rejection (fake GitHub).
- Lease fencing, SHA checks, backoff, fan-out admission in one transaction,
  deterministic submission IDs and Resync freshness (`fetch_test.go`).
- Fetcher partial clone, offline collection, token never persisted, LRU
  eviction (`internal/fetcher`, local `file://` origin).
- Real git origin → fetcher → lease/finish → stored review with pull request
  metadata (`internal/fetcher/integration_test.go`).
- Internal listener secret and isolation from the public listener (real
  binaries); fetcher and server images build.
- Settings, Reviews column and banner (browser tests with mocked API).

## Not yet verified

Each needs a real GitHub App, an installation and public HTTPS for webhooks
(or `gh webhook forward` / smee during development).

1. **App JWT and installation tokens.** `GET /app` (slug), repository-scoped
   `POST /app/installations/{id}/access_tokens` with `contents: read`.
2. **OAuth web flow.** Authorize redirect, callback, code exchange with
   expiring user tokens, `GET /user`, `GET /user/installations` and its
   repositories. Confirm the callback lands on `/settings?github=connected`.
3. **Token refresh.** Let a user token expire (8h) and confirm refresh,
   rotation and storage; revoke the app authorization on GitHub and confirm
   Settings asks to connect again.
4. **Webhooks.** `pull_request` opened, synchronize (push and force-push),
   edited, ready_for_review, converted_to_draft, closed and merged;
   `installation` deleted and suspended; `installation_repositories` removed;
   `github_app_authorization` revoked. Confirm redelivery from GitHub's UI is
   a no-op.
5. **Fetch over HTTPS.** `x-access-token` basic auth through
   `http.extraHeader`, partial clone of `refs/pull/N/head`, blob prefetch with
   `fetch --stdin`, for a same-repository and a fork pull request, public and
   private.
6. **End to end in Compose.** `docker compose --profile github up`: fetcher
   reaches `server:7979` on the `fetch` network; 7979 is not published; the
   pull request appears in the Reviews column grouped with an HTTPS checkout of
   the same repository; the banner shows author, branches and description.
7. **Fan-out.** Two accounts following one repository both receive the pull
   request; an account without access does not.
8. **Reconcile.** Close a pull request while the server is stopped; on startup
   it leaves the Reviews column. Resync extends retention of an idle one.
9. **Rate limits.** Behavior when GitHub returns 403/429 rate limiting during
   listing, lease or Resync.
10. **Push.** Web Push to real push services: FCM (Chrome, Android), Mozilla
    autopush (Firefox) and Apple (Safari, and an installed iOS 16.4+ home
    screen app). Confirm VAPID acceptance with the https public URL as
    subject, the padded 4 KiB aes128gcm body, Topic collapsing, 410 on an
    unsubscribed device, and that an opened pull request and a force push
    each notify once while first follow, Resync and edits stay silent.
    Verified so far only against a local fake push service
    (`internal/webpush`, `internal/pushservice`, `internal/remoteserver`).

## Known risks and likely follow-ups

- The collector's 30-second timeout may be too short for very large pull
  requests; the fetcher may need its own budget.
- The fetcher passes no proxy or CA environment to Git, so deployments behind
  an HTTPS proxy fail.
- A corrupt mirror is not repaired automatically; delete it from the
  `diffx-mirrors` volume to rebuild it.
- Disk usage is checked after each job, so concurrent fetches can briefly
  exceed `DIFFX_FETCHER_DISK_LIMIT_GB`.
- Listing caps (10,000 repositories, 3,000 open pull requests per repository)
  truncate silently.
- Checkouts cloned over SSH or without `.git` in their HTTPS remote group
  separately from their pull requests.
- Each lease reads the pull request and mints a token; heavy webhook traffic
  on large repositories may need caching of installation tokens per lease.
