---
title: Reviewing changes
description: Navigate projects, snapshots, comments and reviewed marks.
---

# Reviewing changes

The Reviews column lists repositories by recent activity, each with its current
streams: one per source, checkout, branch and comparison. A local diffx shows
the column once there are at least two current streams to switch between; a
remote server always shows it. Each stream shows its
newest snapshot only; clean or merged streams and superseded snapshots are hidden.
A dot marks streams with changes you have not opened in this browser. Click a
repository's name to open its most active stream; its chevron expands or
collapses the list. Collapse the column to a rail of repository marks; it stays
collapsed across visits, and each mark opens that repository's latest stream.
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
(or one piped capture) and are stored in SQLite: later snapshots re-anchor
comments to moved code, flag outdated ones, and keep marks on files whose patch
is unchanged. Deleting one snapshot keeps them; they end with the stream's last
snapshot. Display preferences remain in the browser.
