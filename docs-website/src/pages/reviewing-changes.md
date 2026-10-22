---
title: Reviewing changes
description: Navigate projects, snapshots, comments and reviewed marks.
---

# Reviewing changes

The Projects drawer lists repositories by recent activity, each with its current
streams: one per source, checkout, branch and comparison. Each stream shows its
newest snapshot only; clean or merged streams and superseded snapshots are hidden.
A dot marks streams with changes you have not opened in this browser. Search
projects to filter current streams by repository, branch, worktree, host or agent.
Collapse the drawer from its header; it stays collapsed across visits. The header
names the open review's repository, branch and worktree. In a local checkout
process, **Sync all checkouts** (Alt+R) collects every checkout again; like a
remote arrival, a newer snapshot of the open stream is announced, not swapped in.
Collapsed files and folders are remembered across reloads in the same tab. When a newer snapshot
arrives for the stream you are reviewing, a notice offers **View latest** without
replacing the open review.

Browse all reviews (Ctrl/Cmd+K) to reach older or empty snapshots. Select a
repository, then an observation. Search/filter stored metadata to find
a branch, worktree, host, source or agent run. Switch between its collected all,
staged and unstaged scopes. Use the file tree to navigate, mark files as reviewed
and comment on lines. **Hide reviewed files**, beside the file filter, removes
reviewed files from both the tree and the diff for that tab. **Copy unresolved** and **Copy all** export comments as
agent-ready XML.

Observations are immutable. New content creates another observation; matching
submissions associate new provenance with an existing review without changing
its captured diff. Comments and reviewed marks belong to the checkout's stream
(or one piped capture) and are stored in SQLite: later snapshots re-anchor
comments to moved code, flag outdated ones, and keep marks on files whose patch
is unchanged. Deleting one snapshot keeps them; they end with the stream's last
snapshot. Display preferences remain in the browser.
