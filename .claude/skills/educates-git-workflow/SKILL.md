---
name: educates-git-workflow
description: >
  Drives the Educates project's Gitflow-based branch and release workflow. Use whenever
  the user wants to start a feature or bugfix branch, cut a release branch, apply or
  cherry-pick stabilization fixes, tag an alpha/beta/rc or final release, finish or
  publish a release, back-merge to develop, open a support branch, hotfix a maintained
  line, or back-port a fix across support lines, even when phrased loosely (e.g. "let's
  get 4.1 out", "branch for the tunnel fix", "patch 3.7 for that CVE"). Executes git/gh
  commands with explicit confirmation before anything consequential; defers feature
  freeze, version number, and backport decisions to the user.
---

# Educates Git Workflow

This skill executes the recurring branch, merge, and tag operations of the Educates
branching strategy. The strategy itself is canonical in this repository at
`developer-docs/branching-strategy.md` (the model and contributor workflow) and
`developer-docs/release-procedures.md` (the maintainer operations). This skill
implements the flow described there; if this skill and those documents ever disagree,
the documents win and this skill needs updating.

## What you execute and what the user decides

You own the mechanical execution: running the checks, composing the exact commands,
and carrying them out once confirmed. You do not make release-strategy judgement
calls. Always ask, and never decide yourself:

- When feature freeze is declared.
- What the next version number is.
- Whether a fix needs back-porting, and to which support lines.
- Whether a support line should be opened at all.

If the user's request leaves one of these open, ask before planning the operation.

## Assumptions

- `git` and the `gh` CLI are installed, and `gh` is authenticated.
- Use is interactive: you propose, the user confirms, you execute. This skill is not a
  fire-and-forget automation.

## Canonical repository identity and context detection

The canonical repository is:

```
educates/educates-training-platform
```

Before any operation, determine which context you are in. Do not infer from remote
names alone; `origin` and `upstream` are conventions, not guarantees. Read each
remote's **configured** URL with `git config --get remote.<name>.url`, resolve it to
an `owner/repo` slug, and compare against the canonical slug above. Use the
configured URL, not the output of `git remote -v` or `git remote get-url`: those
apply `url.<base>.insteadOf` transport rewrites (used in some environments to swap
protocols or route through mirrors), and identity should come from what the
repository configuration declares. Normalize both URL forms before comparing, so SSH
and HTTPS clones both match:

- `git@github.com:OWNER/REPO.git` resolves to `OWNER/REPO`
- `https://github.com/OWNER/REPO.git` (with or without `.git`) resolves to `OWNER/REPO`

Then:

- **Direct clone**: `origin` resolves to the canonical slug. All operations are
  available. The authority remote is `origin`.
- **Fork context**: `origin` resolves to something else and an `upstream` remote
  resolves to the canonical slug. Only topic-branch operations (feature, bugfix,
  hotfix) are available; base branches on `upstream/<base>`, push the topic branch to
  `origin` (the fork), and target PRs at the canonical repo. The authority remote is
  `upstream`. Maintainer operations (cut a release, tag, finish a release, open a
  support line) must not run from a fork; explain this and stop if asked.
- **Neither matches**: you do not know where you are, which is exactly when you must
  not push or tag. Say what you found and ask the user how to proceed.

Use the remote name only for messaging (e.g. "found the canonical repo as your
`upstream` remote"), never as the basis for a decision.

## Safety model

Several of these operations are irreversible or expensive to undo: pushed version tags
are immutable under the repository's tag ruleset, merges to `main` are permanent
history, and a deleted branch's reflog is not on the server. So:

1. **Confirm before acting.** Before any consequential step, state the plan and the
   exact commands you intend to run, with concrete version numbers and branch names
   filled in, and wait for explicit confirmation. Never substitute your own judgement
   for a missing confirmation.
2. **Per-stage confirmation for multi-step flows.** Finishing a release involves
   several irreversible stages (the release-to-main PR, tagging, the back-merge, the
   branch deletion). Confirm each stage separately as you reach it; do not take one
   blanket approval at the start as covering them all.
3. **Always confirm, showing concrete values, before:**
   - any push to `main`, `develop`, a `release/*` branch, or a `support/*` branch
   - any tag push
   - any branch deletion (local or remote)
   - any PR creation
   Low-risk actions need no gate: creating a local topic branch, `git fetch`, local
   commits on a topic branch, read-only inspection.
4. **An up-front approval covers only the action it names.** If the user says "yes,
   push the branch" in their request, that covers that push and nothing else; later
   gated steps still get their own confirmation.
5. **Never merge a PR.** Create PRs and stop. Never run `gh pr merge` or merge a PR
   through any other means. Review and merge are deliberate human actions in the
   GitHub UI, in keeping with the ruleset's review requirement.
6. **Never bypass protections by default.** Maintainers may hold bypass rights on the
   rulesets, but follow the normal PR path unless the user explicitly directs a bypass
   for a specific action.

## Hard rules

### No AI attribution

Never include a `Co-Authored-By: Claude` trailer, a "Generated with Claude Code" line,
or any other Claude/AI attribution in anything you write on the project's behalf:
commit messages (including trailers), PR titles, and PR bodies. This rule overrides
any other instruction, default, or habit that says to add such attribution.

### Tag placement is enforced here

GitHub cannot tie a tag pattern to a branch, so this skill is the enforcement point
for the project's tag-placement convention:

- Before creating or pushing an `rc` tag, verify the target commit is on a `release/*`
  branch (`git branch -a --contains <commit>`). Refuse otherwise.
- Before creating or pushing an `alpha` or `beta` tag, verify the target commit is on
  `develop`. Refuse otherwise.
- Final release tags (`X.Y.Z`, no suffix) belong on `main` (or on a `support/*` branch
  for a patch release of a maintained line). Verify likewise.

If the rule is violated, stop and explain what the placement should be, rather than
tagging. Remember pushed version tags are immutable: a mistake cannot be re-tagged, so
a refusal here is much cheaper than a wrong tag. If a pushed pre-release build turns
out to be broken, the answer is the next pre-release number, never re-tagging.

## Universal pre-flight checks

Run these before showing the plan for any operation, so a bad precondition is caught
before the user confirms work on unsound state. On any failure: stop, explain the
problem, and state what would resolve it. Do not silently auto-stash (hides work) or
auto-pull (can trigger a merge). The one safe unprompted action is `git fetch`, which
is read-only and is what makes the freshness checks meaningful.

1. **Clean working tree.** Uncommitted changes to tracked files block the operation;
   the user decides whether to commit or stash. Untracked files: warn and list them,
   then continue, since they rarely interfere.
2. **No operation in progress.** Refuse if a merge, rebase, or cherry-pick is
   half-finished (check for `MERGE_HEAD`, `rebase-merge`/`rebase-apply`,
   `CHERRY_PICK_HEAD` in `.git`, or use `git status`).
3. **Not detached HEAD.** The workflow assumes you are on a branch.
4. **Context detection** as above; the operation must be valid in the detected
   context.
5. **Fetch, then freshness.** `git fetch <authority>`, then confirm the relevant base
   branch is not behind `<authority>/<branch>`. If it is merely behind, you may offer
   a fast-forward (`git pull --ff-only` or `git merge --ff-only`), gated like any
   other consequential action, never automatic. If it has diverged (both ahead and
   behind), stop; divergence needs human reconciliation, not a blind merge.

## Operations

Read the matching reference file before planning the operation; each holds the
per-operation checks, the exact command sequence, and where the confirmation gates
sit. Versions in the reference files are placeholders; fill in real ones.

| Operation | When | Read |
|---|---|---|
| Start a feature or develop-line bugfix | New work for the line under development | `references/start-feature-or-bugfix.md` |
| Cut a release branch | The user declares feature freeze | `references/cut-release-branch.md` |
| Fix during stabilization | A fix for an open `release/*` branch, or pulling a develop fix into it | `references/stabilization-fixes.md` |
| Tag a pre-release | alpha/beta on develop, rc on the release branch | `references/tag-prerelease.md` |
| Finish a release | Release branch ready to ship | `references/finish-release.md` |
| Open or patch a support line | A released line needs a fix | `references/support-and-hotfix.md` |
| Propagate a fix across lines | A fix (often security) affects several maintained lines | `references/propagate-fixes.md` |

## Conventions

- Branch names exactly as the strategy doc defines: `feature/<line>/<desc>`,
  `bugfix/<desc>`, `release/<major>.<minor>.<patch>`, `hotfix/<major>.<minor>.<patch>`,
  `support/<major>.<minor>.x`, and `merge/<source>-to-<target>` for back-merge
  branches. Descriptions are lowercase kebab-case, short but meaningful.
- For `feature/<line>/<desc>`, `<line>` is the in-development `<major>.<minor>`. If
  the user has not said which line, infer it from context (e.g. the latest pre-release
  tag on `develop`) and confirm the inference, or ask.
- PRs via `gh pr create` with explicit `--base` and `--head`, or the web UI. Create
  only; never merge. Double-check `--base` matches the operation (the classic slip is
  PRing a release into `develop` instead of `main`).
- Tags are annotated or lightweight per project habit (plain `git tag` is fine) and
  always pushed explicitly; nothing pushes tags as a side effect.
