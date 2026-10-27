---
title: Piped diffs
description: Review a fixed patch from stdin.
---

# Piped diffs

Review a fixed patch in a foreground local process:

```sh
git diff | diffx
git show | diffx
diffx < saved.patch
```

The process serves the UI, REST and MCP until Ctrl-C, without watching a checkout.
The page shows only that diff and its file tree; there are no other reviews to
switch to.
Piped diffs ignore remote configuration. `diffx -` explicitly selects stdin;
combining redirected stdin with a path argument or `--path` is rejected.
Collection does not inspect Git or attach repository identity.
Piped diffs have only the all scope and do not
include full file contents. Standard Git patches are limited to 16 MiB total
and 2 MiB per file; a file whose patch exceeds 1 MiB is listed without a
preview. Combined merge diffs are shown against the first parent.
Use `git show --diff-merges=separate` to review every parent separately.
