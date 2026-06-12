# Propagate a Fix Across Maintained Lines

A fix (a vulnerability fix especially) usually affects more than one line: several
`support/*` branches and `develop`. Which lines are affected is the user's decision;
ask for the explicit list and do not assume. Executing the porting is yours.

Work **oldest affected maintained line first**, then port forward in order, ending
with `develop`. One direction means no re-doing the fix and no line left behind.
Cherry-pick is usually cleaner than merge where the surrounding code has diverged.

## Procedure

1. Ask the user which lines are affected. Restate the resulting checklist, e.g.:
   - `support/3.6.x` (oldest affected)
   - `support/3.7.x`
   - `develop`
2. Land the fix on the oldest line first via the normal hotfix workflow
   (`references/support-and-hotfix.md`), if it is not already there.
3. For each newer line in order, port the commit(s):

```
git switch support/3.7.x
git pull --ff-only
git switch -c hotfix/3.7.2          # next patch version for that line
git cherry-pick <commit-sha(s)>
```

   Gated, per line:
   - [confirm] `git push -u origin hotfix/3.7.2`
   - [confirm] `gh pr create --base support/3.7.x --head hotfix/3.7.2 --title "..."`

4. For `develop`, the port is a `bugfix/*` branch:

```
git switch develop
git pull --ff-only
git switch -c bugfix/<desc>
git cherry-pick <commit-sha(s)>
```

   Gated:
   - [confirm] `git push -u origin bugfix/<desc>`
   - [confirm] `gh pr create --base develop --head bugfix/<desc> --title "..."`

5. If a cherry-pick conflicts, stop and resolve with the user.
6. When every line's PR exists, restate the checklist with status per line, so the
   user can see nothing was missed before they merge. Tagging each line's patch
   release follows the support-and-hotfix reference, per line, after its PR merges.

## Back-porting

When the fix already exists on a newer line and an older maintained line turns out to
be affected, the same procedure runs in reverse: cherry-pick down onto the older
support branch via its hotfix workflow. The checklist discipline is identical.
