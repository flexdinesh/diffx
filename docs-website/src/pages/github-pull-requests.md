---
title: GitHub pull requests
description: Review open GitHub pull requests on a diffx server.
---

# GitHub pull requests

A `diffx-server` can review the open pull requests of GitHub repositories you
follow. Each pull request is its own review, grouped with your other reviews of
that repository. Its row in the Reviews column shows its number, title, GitHub
and author. A banner above the diff shows its state, branches, a link to GitHub
and its description. Closed and merged pull requests leave the Reviews column;
their reviews stay reachable through search until they expire.

This needs the distributed server, a GitHub App you create, and the
`diffx-fetcher` container. Local `diffx` has no GitHub integration.

## How it works

1. GitHub sends pull request events to the server's webhook.
2. The server records which pull requests need a refresh. Repeated events
   coalesce; redelivered events have no effect.
3. `diffx-fetcher` leases one pull request at a time over a private port. The
   server gives it a short-lived token that can only read that repository's
   contents.
4. The fetcher keeps a partial clone of each repository, downloads only the
   files the pull request changes, and computes the diff offline.
5. The server publishes the diff to every account that follows the repository
   and can still read it on GitHub.

The server checks access again when you open Settings, follow a repository or
resync, and before delivering to an account whose last check is older than
eight hours. GitHub does not retry failed webhook deliveries, so the server
also reconciles at startup and when you choose **Resync pull requests**.

## Create the GitHub App

In GitHub, open **Settings → Developer settings → GitHub Apps → New GitHub
App** (or the same page of an organization).

- **Homepage URL**: your diffx server, such as `https://diffx.example.com`.
- **Callback URL**: `https://diffx.example.com/api/v2/github/callback`.
- **Expire user authorization tokens**: on.
- **Request user authorization (OAuth) during installation**: off.
- **Webhook URL**: `https://diffx.example.com/api/v2/github/webhooks`.
- **Webhook secret**: a long random value, such as the output of
  `openssl rand -hex 32`.
- **Repository permissions**: Contents read-only, Pull requests read-only,
  Metadata read-only.
- **Subscribe to events**: Pull request.

Installation and authorization events are delivered without a subscription.
After creating the app, note its App ID and Client ID, generate a client
secret, and generate a private key (`.pem`).

The webhook must reach the server over public HTTPS. Without it, pull requests
update only at server startup and on Resync.

## Configure the server

Set every variable; a partial set stops the server with the missing names.
Secrets are read only from the environment.

| Variable                               | Value                                                                                |
| -------------------------------------- | ------------------------------------------------------------------------------------ |
| `DIFFX_SERVER_GITHUB_APP_ID`           | App ID                                                                               |
| `DIFFX_SERVER_GITHUB_CLIENT_ID`        | Client ID                                                                            |
| `DIFFX_SERVER_GITHUB_CLIENT_SECRET`    | Client secret                                                                        |
| `DIFFX_SERVER_GITHUB_PRIVATE_KEY_FILE` | Path to the private key file                                                         |
| `DIFFX_SERVER_GITHUB_WEBHOOK_SECRET`   | Webhook secret                                                                       |
| `DIFFX_SERVER_GITHUB_PUBLIC_URL`       | Browser-facing HTTPS origin, such as `https://diffx.example.com`                     |
| `DIFFX_SERVER_GITHUB_TOKEN_KEY`        | 32 random bytes in base64 (`openssl rand -base64 32`); encrypts stored GitHub tokens |
| `DIFFX_SERVER_FETCHER_SECRET`          | At least 32 characters (`openssl rand -hex 32`); shared with the fetcher             |

The server listens for the fetcher on `DIFFX_SERVER_INTERNAL_PORT` (default 7979) on the same host as its public listener. Never publish that port; only
the fetcher should reach it. Keep `DIFFX_SERVER_GITHUB_TOKEN_KEY` stable:
changing it requires every account to connect GitHub again.

## Run the fetcher

With Compose, put the variables above in a git-ignored `.env` beside
`compose.yaml`, mount the private key into the server with an override file,
and start the `github` profile:

```yaml
# compose.override.yaml
services:
  server:
    environment:
      DIFFX_SERVER_GITHUB_PRIVATE_KEY_FILE: /run/secrets/github-app.pem
    volumes:
      - ./github-app.pem:/run/secrets/github-app.pem:ro
```

```sh
docker compose --profile github up -d
```

The fetcher keeps mirrors in the `diffx-mirrors` volume and evicts the least
recently used ones above `DIFFX_FETCHER_DISK_LIMIT_GB` (default 20).
`DIFFX_FETCHER_CONCURRENCY` (default 2, at most 8) sets parallel fetches.

To run it elsewhere, build `docker build --target fetcher .` and set
`DIFFX_FETCHER_SERVER_URL` (the server's internal listener, such as
`http://server:7979`), `DIFFX_FETCHER_SECRET` and `DIFFX_FETCHER_MIRRORS`. The
fetcher needs Git 2.44 or later and HTTPS access to github.com.

## Connect and follow

1. Open **Account settings** and choose **Connect GitHub**. GitHub asks you to
   authorize the app, then returns to Settings.
2. Choose **Install on repositories** to install the app where you need it.
   You can follow only repositories that both you and the app can read.
3. Check the repositories to follow. Their open pull requests, drafts
   included, appear in the Reviews column shortly.

**Resync pull requests** rechecks access and refreshes every followed
repository, including pull requests that closed without an event. Resync also
extends retention of pull requests that have not changed recently. If GitHub
access expires or is revoked, Settings asks you to connect again; followed
repositories receive nothing until then. **Disconnect** removes the
authorization and every follow; existing reviews stay until they expire.

## Limits

- A push, edit or state change creates a new snapshot in the same review
  stream, so comments and reviewed marks carry across force-pushes.
- Pull request descriptions render as Markdown without raw HTML; images show
  as links and are never loaded.
- Repositories cloned over SSH, or without `.git` in their HTTPS remote, group
  separately from their pull requests.
- Very large pull requests can exceed collection time or preview limits.
