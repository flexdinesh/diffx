---
title: Reviewing changes
description: Navigate projects, snapshots, comments and reviewed marks.
---

# Reviewing changes

diffx opens on **Reviews**, your review bench: every current stream with
changes, one per source, checkout, branch and comparison, newest activity
first. Each stream shows its newest snapshot only; clean streams and
superseded snapshots are hidden. **Needs attention** lists new streams and
streams updated since you opened or marked them viewed; **All open** lists
every current stream and **Closed** merged or closed pull requests and stale
branches. Drafts wait under All open until they are ready. On a first visit,
work idle for a day counts as already seen. Filter by repository or agent,
or type to search. Move with ↑ and ↓, press Space to peek at a review's size,
progress and description without opening it, and Enter to open it. **Mark all
viewed** clears Needs attention; Alt+Shift+V and Alt+Shift+U mark the selected
review viewed or unread. Views and filters stay in the address, so you can
bookmark them.

Inside a review, **‹ Reviews** (Alt+Shift+B) returns to the bench as you left
it. The header's arrows (Alt+Shift+J and Alt+Shift+K) step to the next or
previous review in the bench's order, and its check marks the review viewed or
unread. Once every file is reviewed, a notice offers the next review.
The header names the open review's repository, branch and worktree; the
repository and branch are menus you can type into to filter, then press Enter
to switch. In a local checkout
process, **Sync checkout** or **Sync all checkouts** (Alt+R) collects the
process's checkouts again; like a
remote arrival, a newer snapshot of the open stream is announced, not swapped in.
Collapsed files and folders are remembered across reloads in the same tab. When a newer snapshot
arrives for the stream you are reviewing, a notice offers **View latest** without
replacing the open review.

Search reviews (the header magnifier or Ctrl/Cmd+K) to reach older or empty
snapshots. Select a
repository, then an observation. Search/filter stored metadata to find
a branch, worktree, host, source or agent run. Switch between its collected all,
staged and unstaged scopes. Use the file tree to navigate, mark files as reviewed
and comment on lines. Alt+J and Alt+K move between files; Alt+M marks the
selected file reviewed and moves to the next one, or unmarks a reviewed file.
**Hide reviewed files**, beside the file filter, removes
reviewed files from both the tree and the diff for that tab. **Copy unresolved** and **Copy all** export comments as
agent-ready XML.

Observations are immutable. New content creates another observation; matching
submissions associate new provenance with an existing review without changing
its captured diff. Comments and reviewed marks belong to the checkout's stream
(or one piped capture) and are stored with the snapshots (SQLite locally,
Postgres on a server): later snapshots re-anchor
comments to moved code, flag outdated ones, and keep marks on files whose patch
is unchanged. Deleting one snapshot keeps them; they end with the stream's last
snapshot. Display preferences remain in the browser.
