# Fixes During Release Stabilization

Once a `release/*` branch exists, route a change by where it originates. Never merge
`develop` into the release branch: that drags all of develop's in-progress work into
the frozen release. Individual commits are cherry-picked instead.

## Per-operation checks

After the universal pre-flight:

- The `release/X.Y.Z` branch exists on the authority and the local copy is up to date.

## A small fix found during stabilization

Commit directly to the release branch:

```
git switch release/4.1.0
# ...fix, commit...
```

Then, gated:

1. [confirm] `git push origin release/4.1.0`

## A fix that warrants its own review or CI run

Cut a `bugfix/*` branch off the release branch and PR it back to the release branch:

```
git switch release/4.1.0
git switch -c bugfix/some-fix
# ...fix, commit...
```

Then, gated:

1. [confirm] `git push -u origin bugfix/some-fix`
2. [confirm] `gh pr create --base release/4.1.0 --head bugfix/some-fix --title "Fix ..."`

The `--base` here is the release branch, not `develop`; double-check it. Stop after
creating the PR.

From a fork: base the bugfix branch on `upstream/release/4.1.0`, push to the fork, PR
against the release branch on the canonical repo.

## A change that already exists on develop

Cherry-pick the specific commit(s); ask the user for the commit SHA(s) if not given:

```
git switch release/4.1.0
git cherry-pick <commit-sha>
```

Then, gated:

1. [confirm] `git push origin release/4.1.0`

If the cherry-pick conflicts, stop and let the user resolve or direct the resolution;
do not invent a resolution silently.

Either way the fix reaches `develop` later through the standard back-merge when the
release is finished; nothing extra to do for that now.
