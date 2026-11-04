# CLI

## Review

```sh
diffx                                         # review current checkout
diffx /path/to/repo                           # review another checkout
diffx . --scan-worktrees                      # include the repo's other worktrees
diffx ~/work --scan-depth 4                   # discover checkouts below a directory
diffx --path ~/work --path ~/oss              # several roots
diffx . --base HEAD                           # uncommitted changes only
diffx . --base origin/main                    # compare against a ref
diffx . --no-browser                          # print URL, do not open browser
diffx . --port 7985                           # fixed viewer port (0 = any)
diffx . --headless                            # socket only, no viewer
diffx . --replace                             # replace running instance
diffx . --reset-state                         # back up state and start fresh
git diff | diffx                              # review a piped patch
diffx --version                               # print version
diffx --help                                  # usage
```

## Status and MCP

```sh
diffx status          # running instance
diffx status --json   # as JSON
diffx mcp             # MCP over stdio for agents
```

## Remote machine over SSH

```sh
diffx ~/work --headless                                    # on the remote host
diffx connect ssh://devbox                                 # forward and open browser
diffx connect ssh://user@devbox:2222                       # user and SSH port
diffx connect ssh://devbox --port 7995 --no-browser        # local port, no browser
diffx connect ssh://devbox --remote-diffx /path/to/diffx   # remote binary path
diffx connect ssh://devbox --json                          # print forward as JSON
```

## Sync to a server

```sh
diffx config set remote.url https://reviews.example.com
diffx config set remote.token - < token.txt   # read token from stdin
diffx remote status                           # check destination and pending deliveries
diffx sync                                    # publish current checkout
diffx sync --path /path/to/repo               # publish another checkout
diffx sync --print                            # print only server URL
diffx sync --retry                            # resend failed uploads
```

## Config

```sh
diffx config get local.port
diffx config set local.port 7985
diffx config set local.retentionDays 14
diffx config remove local.port
```

Keys: `local.port`, `local.state`, `local.webDir`, `local.retentionDays`,
`remote.url`, `remote.token`. File: `~/.config/diffx/config.json`
(`--config-file` or `DIFFX_CONFIG_PATH` overrides).

## Agent hooks

```sh
diffx hook --harness claude                   # run by agent plugins (codex|claude|opencode|pi)
diffx collector status                        # queued hook deliveries
diffx collector retry                         # retry queued deliveries
```

## Diagnostics

```sh
diffx . --debug                               # collection progress
diffx sync --debug                            # sync progress
diffx diagnostics export --latest --output failure.json
diffx diagnostics export REPORT_ID --output failure.json
```
