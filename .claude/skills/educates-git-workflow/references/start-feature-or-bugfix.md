# Start a Feature or Develop-Line Bugfix

New functionality goes on `feature/<line>/<desc>`; a non-feature fix to the
in-development line goes on `bugfix/<desc>`. Both branch off `develop` and merge back
into `develop` via a PR.

## Ask first

- A short kebab-case description (propose one from the user's wording and confirm).
- Feature or bugfix? (New functionality vs a fix.)
- For a feature: which line (`<major>.<minor>`), if not already clear. Infer from the
  latest pre-release tag on `develop` if possible and confirm the inference.

## Per-operation checks

After the universal pre-flight:

- Local `develop` is up to date with `<authority>/develop`.
- The proposed branch name does not already exist on the authority:
  `git ls-remote --heads <authority> <branch-name>` returns nothing.

## Direct clone

```
git switch develop
git pull --ff-only
git switch -c feature/4.1/new-export-api    # or bugfix/<desc>
# ...work, commit...
```

Then, gated:

1. [confirm] `git push -u origin feature/4.1/new-export-api`
2. [confirm] `gh pr create --base develop --head feature/4.1/new-export-api --title "..."`

Stop after creating the PR. Review and merge happen in the GitHub UI.

## Fork context

The fork's `develop` may be stale or absent; the fork is only where the branch is
pushed from. Base everything on upstream:

```
git fetch upstream
git switch -c feature/4.1/new-export-api upstream/develop
# ...work, commit...
```

Then, gated:

1. [confirm] `git push -u origin feature/4.1/new-export-api`   (origin is the fork)
2. [confirm] `gh pr create --repo educates/educates-training-platform --base develop --head <fork-owner>:feature/4.1/new-export-api --title "..."`
