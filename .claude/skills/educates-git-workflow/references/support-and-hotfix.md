# Open or Patch a Support Line

A `support/<major>.<minor>.x` branch is the maintenance line for a released version.
One is cut only when a real need to patch that line arises, never by default; whether
to open one is the user's call, not yours.

Opening a support line and tagging patch releases are maintainer operations: direct
clone only. A hotfix branch itself may come from a fork (an outside contributor
proposing a fix to a maintained line).

## Open a support line

Ask first: confirm the user wants the line opened, and which release tag to cut from
(normally that line's latest release tag).

Checks, after the universal pre-flight:

- The release tag being branched from exists.
- `support/X.Y.x` does not already exist on origin.
- The name matches `support/<major>.<minor>.x` exactly (the literal letter `x` as the
  patch part).

```
git switch -c support/3.7.x 3.7.0
```

1. [confirm] `git push -u origin support/3.7.x`

## Two ways to patch a support line

Once the line exists, a patch release is built one of two ways. Which one is the
user's call; ask if it is not clear:

- **Path A — direct hotfix.** A single, self-contained fix that needs no pre-release
  validation. The fix goes on a `hotfix/*` branch, PRs straight back to the support
  branch, and the final tag is applied on the support branch. This is the default for
  simple patches.
- **Path B — stabilized patch release.** The patch needs rc builds first, or bundles
  several changes (often back-ports) that should stabilize together before shipping.
  A `release/X.Y.Z` branch is cut *from the support branch* and stabilized there, with
  rc tags, exactly as an imminent primary-line release is — only the support branch,
  not `main`/`develop`, is the base it returns to.

Both paths end with a final `X.Y.Z` tag on the support branch, and both must consider
propagation to other lines (see "Propagating across lines" at the end).

## Path A — direct hotfix

Ask first: the next patch version for the line (check existing tags:
`git tag -l '3.7.*'`).

Checks: local support branch up to date with the authority (`upstream` if the fix
originates from a fork).

```
git switch support/3.7.x
git pull --ff-only
git switch -c hotfix/3.7.1
# ...fix, commit...
```

Then, gated:

1. [confirm] `git push -u origin hotfix/3.7.1`
2. [confirm] `gh pr create --base support/3.7.x --head hotfix/3.7.1 --title "Fix ... (3.7.1)"`

Stop; human reviews and merges in the UI. Merges to `support/*` go through PRs, the
same as `main` and `develop`. Then tag the patch release (see "Tag the patch release").

## Path B — stabilized patch release

Use when the patch needs rc builds, or batches several changes (often back-ports)
together. The support branch plays the role `main`+`develop` play for a primary-line
release: the release branch is cut from it, stabilized, and finished back into it.

Ask first: the target patch version `X.Y.Z` (check `git tag -l '3.7.*'`).

Checks, after the universal pre-flight:

- Local `support/X.Y.x` is up to date with origin.
- `X.Y.Z` does not already exist as a tag, locally or on origin.
- `release/X.Y.Z` does not already exist on origin.

### Cut the release branch from the support line

This is the one deviation from `references/cut-release-branch.md`, whose default base
is `develop`; here the base is the support branch.

```
git switch support/3.7.x
git pull --ff-only
git switch -c release/3.7.3
```

Then, gated:

1. [confirm] `git push -u origin release/3.7.3`

### Stabilize on the release branch

Assemble the patch here as ordinary stabilization work:

- Cherry-pick the back-ported commit(s), oldest-first, per
  `references/propagate-fixes.md`. If a cherry-pick carries a newer version's
  release-notes file, see "Back-porting changes that carry newer-version release
  notes" below.
- Add the `version-X.Y.Z.md` release notes and the `project-docs/index.rst` entry.

rc builds are tagged on this `release/X.Y.Z` branch via `references/tag-prerelease.md`.
No change to the tag-placement rule is needed: the rc sits on a `release/*` branch,
which is exactly where rc tags belong. If an rc is bad, fix on the release branch and
tag the next rc number; never re-tag.

### Finish the patch release

When an rc is blessed, finish much like `references/finish-release.md`, but the base
is the support branch, not `main`:

1. [confirm] `gh pr create --base support/3.7.x --head release/3.7.3 --title "Release 3.7.3"`

   The `--base` is the **support branch**, not `main` and not `develop`. Double-check
   it; this is the classic slip. Stop; a human reviews and merges in the UI.

After the PR merges, tag the final release on the support branch (see "Tag the patch
release"), then, gated:

2. [confirm] `git push origin --delete release/3.7.3`

   The `release/X.Y.Z` branch is ephemeral and deleted once finished; also delete the
   local copy. The `support/X.Y.x` line is permanent and is never deleted.

**No back-merge into `develop`.** A primary-line release back-merges to `develop`; a
support-line release does not. develop is a separate, later line, so propagation to it
(and to other maintained lines) is handled deliberately via
`references/propagate-fixes.md`, not by merging the support branch. In the common case
the changes were back-ported *from* develop and are already there.

## Tag the patch release

Used by both paths, after the fix/PR is merged into `support/X.Y.x`. Patch releases get
release notes like any other release, so before tagging, check
`git show origin/support/3.7.x:project-docs/release-notes/version-3.7.1.md` succeeds
and `project-docs/index.rst` on that branch references `release-notes/version-3.7.1`.
If missing, stop; add the notes through the hotfix/PR (Path A) or release-branch
(Path B) flow first.

```
git switch support/3.7.x
git pull --ff-only
git tag 3.7.1
```

- [confirm] `git push origin 3.7.1`

Placement check applies: a final-format tag on a support branch must sit on that
support branch. The pushed tag triggers the same CI release build as any release.

## Back-porting changes that carry newer-version release notes

A back-ported commit was authored against a newer line, so it may add or edit a
release-notes file for a **future** version (e.g. `version-4.0.0.md`). That file must
not ride along onto the older line: the notes belong under the version actually being
shipped (`version-X.Y.Z.md`), and the newer file usually does not exist on the older
line, so a plain cherry-pick throws a modify/delete conflict anyway.

Separate the code change from the notes:

1. Before dropping anything, capture the note text:
   `git show <sha> -- project-docs/release-notes/version-<newer>.md`.
2. Cherry-pick with `--no-commit` so the staged set can be pruned:
   ```
   git cherry-pick -n <sha>
   ```
3. Drop the newer notes from the staged set (the modify/delete conflict is the signal
   that a foreign notes file was coming across; resolving it *is* the drop). Also
   remove any `release-notes/version-<newer>` line the commit added to
   `project-docs/index.rst`:
   ```
   git rm project-docs/release-notes/version-<newer>.md
   # revert the version-<newer> toctree entry in project-docs/index.rst
   ```
4. Commit only the code change, reusing the original message and author:
   ```
   git commit -C <sha>
   ```
5. Reland the wording under the release being built: fold it into `version-X.Y.Z.md`
   as part of the normal release-notes commit — not a separate one.

Only apply this to commits that actually touch a notes file; cherry-pick the rest
normally.

Adapting the note for `X.Y.Z`:

- Write it as a **first-class change in the release being shipped**, not as a
  back-port. Release notes describe what changed for the reader; provenance (that it
  was back-ported, and from where) belongs in the commit message and PR, not the notes.
- **Do not forward-reference.** Strip wording tied to the newer version — feature
  names, "new in X.Y", or a version number the reader of the older line is not running.
- **Keep shared identifiers.** Carry over issue or CVE references (e.g. `#1073`, an
  advisory ID); they let users correlate the same fix across lines.
- **Keep wording consistent across lines.** When the same fix ships on several
  maintained lines and develop, phrase each line's note the same way so readers
  recognize it as the same underlying fix — this does the job "back-ported" would,
  without the confusing version reference.

## Propagating across lines

After either path, ask: does this fix affect other maintained lines or develop? If yes,
continue with `references/propagate-fixes.md`. Treat "applied to every affected
maintained line, including develop" as a required checklist item, especially for
security fixes.
