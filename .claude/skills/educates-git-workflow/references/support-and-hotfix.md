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

## Patch a support line (hotfix)

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
same as `main` and `develop`.

## Tag the patch release

After the hotfix PR is merged. Patch releases get release notes like any other
release, so before tagging, check
`git show origin/support/3.7.x:project-docs/release-notes/version-3.7.1.md` succeeds
and `project-docs/index.rst` on that branch references `release-notes/version-3.7.1`.
If missing, stop; add the notes through the hotfix/PR flow first.

```
git switch support/3.7.x
git pull --ff-only
git tag 3.7.1
```

3. [confirm] `git push origin 3.7.1`

Placement check applies: a final-format tag on a support branch must sit on that
support branch. The pushed tag triggers the same CI release build as any release.

Then ask: does this fix affect other maintained lines or develop? If yes, continue
with `references/propagate-fixes.md`. Treat "applied to every affected maintained
line, including develop" as a required checklist item, especially for security fixes.
