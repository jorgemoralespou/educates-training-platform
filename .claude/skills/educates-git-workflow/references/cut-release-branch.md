# Cut a Release Branch (at Feature Freeze)

A `release/<major>.<minor>.<patch>` branch isolates stabilization of a version from
ongoing development. Cutting it is what enacts feature freeze: after this, only
stabilization work goes on the release branch, and `develop` opens up for the next
version.

Maintainer operation: direct clone only, never from a fork.

## Ask first

- Confirm the user is declaring feature freeze now. Do not cut a release branch as a
  side effect of some other request.
- The exact target version (`X.Y.Z`). Do not guess it.

## Per-operation checks

After the universal pre-flight:

- Local `develop` is up to date with `origin/develop`.
- The target version does not already exist as a tag, locally or on origin:
  `git tag -l X.Y.Z` and `git ls-remote --tags origin X.Y.Z` are both empty.
- The branch `release/X.Y.Z` does not already exist on origin.
- Check for other `release/*` branches still on origin
  (`git ls-remote --heads origin 'release/*'`). Finished release branches are supposed
  to be deleted, so finding one means either housekeeping was missed or another
  release is still in flight. Decide which by checking whether that release's final
  tag exists: if it does, the branch is a stale leftover, so mention it and suggest
  deleting it once this cut is done, but do not block on it. If the final tag does
  not exist, that release looks unfinished; stop and ask, since cutting the next
  release while the previous one is mid-stabilization is usually a process error.

## Commands

```
git switch develop
git pull --ff-only
git switch -c release/4.1.0
```

Then, gated:

1. [confirm] `git push -u origin release/4.1.0`

From this point alpha/beta tags stop (those belong to `develop` before the freeze) and
rc tags happen here. Remind the user that fixes to this release now follow the
stabilization-fix workflow.
