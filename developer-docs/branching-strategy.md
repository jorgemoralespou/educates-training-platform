Branching Strategy
==================

This project uses a branching strategy modelled on Gitflow, with some adaptations of its own. The arrangement below is described through a worked example in which `develop` is the integration line for the 4.x series while an earlier released line (3.x) is being patched.

The version numbers here are **illustrative only**. They show how the branches relate; they are not a statement of the current versions, and this page is not necessarily kept in step with them as development moves on. Read `4.x`/`3.x` as "the line under development" and "an older released line", and apply the same patterns to whatever versions are current.

This document covers the branching model itself and the day to day workflow for contributing features and fixes. The maintainer side, that is cutting releases, tagging versions, and opening and patching support lines, is covered in the [release procedures](release-procedures.md).

Branch Overview
---------------

| Branch | Purpose | Branched from |
|---|---|---|
| `main` | Production-ready code; only **final** release tags live here | n/a |
| `develop` | Integration line for the line under development (4.x in this example); alpha/beta tags are made here | `main` |
| `support/<major>.<minor>.x` | Optional, on-demand maintenance line for a release line that needs patching; several can exist at once (e.g. `support/3.6.x`, `support/3.7.x`) | `main`, at that line's latest release tag, created only when a need to patch it arises |
| `feature/4.0/*` | Individual features for the line under development | `develop` |
| `release/<major>.<minor>.<patch>` | Stabilizing an imminent release; rc tags are made here (e.g. `release/4.0.0`) | `develop`; or a `support/<major>.<minor>.x` branch for a stabilized patch release of a maintained line |
| `bugfix/<desc>` | A non-feature fix; for the in-development line, or for a release found broken during stabilization | `develop`, or the `release/` branch being stabilized |
| `hotfix/<major>.<minor>.z` | A single fix for a maintained line | the matching `support/<major>.<minor>.x` |

Repository Model: GitHub and Forks
----------------------------------

The canonical repository on GitHub is the source of truth. All shared, long-lived branches, meaning `main`, `develop`, and any `release/*` and `support/*` branches, live there. They are the team's coordination points; they are not recreated per-contributor.

**Internal team: work directly in the canonical repo.** Members with write access push their own `feature/*` and `bugfix/*` branches straight to the canonical repo and open pull requests within it. There is no fork and no upstream/origin juggling: everyone fetches and pushes against the one repo. Branch protection on the shared branches (`main`, `develop`, `release/*`, `support/*`) enforces review and CI instead of a fork boundary. The exception is genuinely speculative work that should be kept out of the canonical repo, which can live in a personal fork until it is worth proposing.

**Outside contributors: always fork.** People without write access fork the canonical repo, push their branch to their fork, and open a pull request from the fork back to the canonical repo, targeting the correct base branch (`develop`, or a `release/*` branch for a stabilization fix). A fork is a point-in-time snapshot: branches created on the canonical repo *after* the fork, including a new `release/*` branch, do not appear in the fork automatically. To work against one, add the canonical repo as the `upstream` remote, `git fetch upstream`, and branch from `upstream/<branch>`.

`release/*` and `support/*` branches are shared infrastructure: they are created on the canonical repo by whoever runs releases, never independently in a contributor's fork. To contribute a stabilization fix without write access, branch off `upstream/release/...`, push the `bugfix/*` branch to your fork, and open a PR targeting the release branch on the canonical repo.

GitHub Ruleset Constraints
--------------------------

The canonical repo is protected by GitHub rulesets, which impose constraints this model works within:

* **Merges to `main`, `develop`, and `support/*` must go through pull requests.** A direct push to these branches is rejected, so every merge-back and release merge is a PR. This is why local "finish" style auto-push commands don't work against them.
* **Published version tags are immutable.** The tag ruleset blocks updating or deleting tags matching the version pattern, so a release tag cannot be moved or removed once pushed. Tag creation is left open, so routine alpha/beta/rc tagging is unaffected.
* **GitHub cannot enforce "rc tags only on `release/*`, alpha/beta only on `develop`".** A tag names a commit, not a branch, so no ruleset can tie a tag pattern to a branch. That rule is a **convention** upheld by process and, if desired, by a check in the CI workflow that inspects the tagged commit, not something GitHub enforces.

Tooling
-------

This strategy is a branching *model* and does not depend on any particular tool. The **primary, recommended way to drive it is plain `git`** plus GitHub for pull requests. The recipes below and in the [release procedures](release-procedures.md) cover every branch/merge/tag step that way. This is deliberate: because merges to the shared branches must go through PRs, the steps that matter most are GitHub-side actions a local helper tool cannot perform anyway, so a helper buys relatively little here.

A helper tool remains **optional**. If one is used, prefer **git-flow-next** (`git-flow.sh`). The classic `git-flow` (nvie) and `git-flow-avh` editions are both discontinued and the Homebrew `git-flow` formula is deprecated and scheduled to be disabled, whereas git-flow-next is the actively maintained successor and stays backward-compatible with the same `feature` / `release` / `hotfix` / `support` branch types. Note its value is limited in this setup: its "finish" automation merges and pushes locally, which the PR-requiring rulesets reject, so it helps mainly with the un-gated mechanics (creating correctly-named branches, local merges of `bugfix/` into a `release/` branch). It cannot open or merge a PR for you. Tooling status changes over time, so check current documentation before relying on any helper.

### Claude Code skill

The repository also carries a Claude Code skill, **`educates-git-workflow`**, under [.claude/skills/educates-git-workflow](../.claude/skills/educates-git-workflow/). Anyone using Claude Code in a clone of this repository gets it automatically: asking for any of the operations described here or in the [release procedures](release-procedures.md) (starting a feature or bugfix branch, cutting or finishing a release, tagging, patching a support line, porting a fix across lines) will be driven through this workflow rather than improvised with raw git.

What it does, and the guarantees it follows:

* Runs pre-flight checks before proposing anything: clean working tree, no operation in progress, base branch up to date, and that the clone really is the canonical repo (or a fork, which restricts it to topic-branch operations).
* Follows the naming conventions, enforces the tag-placement rule (rc only on `release/*`, alpha/beta only on `develop`), and checks release notes exist before a release or patch release can be finished.
* States the exact commands with concrete version numbers and branch names, and waits for explicit confirmation before anything consequential: any push to a shared branch, any tag push, any branch deletion, any PR creation. Multi-step flows confirm each stage separately.
* Only ever creates pull requests, never merges them, and does not bypass the rulesets.
* Leaves the judgement calls to you: it asks about feature freeze, version numbers, whether to open a support line, and backport targets rather than deciding.

This document and the release procedures remain **canonical**; the skill implements the flow described in them. A change to the strategy or procedures should be accompanied by a matching update to the skill, so the two do not drift.

Features and Fixes for the Line Under Development
-------------------------------------------------

All work for the line under development happens on `develop` as standard Gitflow.

1. Branch a feature off `develop`: `feature/4.0/<short-description>`.
2. Develop and review the feature.
3. Merge back into `develop`.

Use kebab-case for the description, kept short and descriptive, e.g. `feature/4.0/new-export-api`.

A fix to the in-development line that isn't new functionality can use a `bugfix/<desc>` branch off `develop` instead of `feature/`; the workflow is otherwise identical (branch off `develop`, merge back into `develop`). The same `bugfix/` prefix is also used for fixes found while stabilizing a release, distinguished by which branch it is cut from.

Documentation changes follow this same workflow: they land on `develop` through a normal `feature/*` or `bugfix/*` branch and PR, the same as code. There is no separate process for documentation updates between releases; the published documentation has a `latest` version built from `develop`, so documentation changes are publicly visible without waiting for a release (see the [release procedures](release-procedures.md) for details).

Fixes During Release Stabilization
----------------------------------

Once a `release/*` branch exists (see the [release procedures](release-procedures.md) for when and how one is cut), changes to it are handled by where the change originates:

* **A fix found during stabilization** (the normal case) is made on the `release/` branch. For small fixes, commit directly to the branch. For anything that warrants its own review or CI run, cut a `bugfix/<desc>` branch off `release/`, then merge it back. Either way the fix reaches `develop` later through the standard merge-back when the release is finished.
* **A change that already exists on `develop`** (e.g. a fix made there first) is brought in by cherry-picking the specific commit(s) onto the `release/` branch.

Do **not** merge `develop` into the `release/` branch to pull in a change. That drags all of develop's in-progress work into the release and defeats the freeze. Cherry-pick the individual commits instead.

Maintenance of Released Lines
-----------------------------

A released line that needs patching after the fact gets an on-demand `support/<major>.<minor>.x` branch, cut from `main` at that line's latest release tag. Most patches are a single fix made on a `hotfix/*` branch off the support branch, with the patch release tagged on the support branch. When a patch needs pre-release (rc) builds, or bundles several changes (often back-ports) that should stabilize together, a `release/<major>.<minor>.<patch>` branch is instead cut from the support branch, stabilized there with rc tags, and merged back into the support branch before the final tag — the same release-branch machinery as the primary line, but based on the support branch rather than `develop`/`main`, and with no back-merge to `develop`. A fix that affects more than one maintained line, security fixes in particular, must land on **all** affected lines that are still maintained, including `develop`.

The mechanics of opening support lines, applying hotfixes, tagging patch releases, and propagating fixes across lines are maintainer operations covered in the [release procedures](release-procedures.md).

Naming Conventions
------------------

* Lowercase, kebab-case descriptions: `feature/4.0/add-search-bar`.
* Keep descriptions short but meaningful; a tracking issue number is optional.
* Use the `/`-separated prefixes exactly as listed in the table above.

Common Operations
-----------------

Recipes for the contributor-facing operations. `git` commands are the spine; where a pull request is needed, both a `gh` command and the web-UI alternative are shown. PRs are shown as *create only*, since review and merge happen after approval, in keeping with the ruleset's review requirement (do not auto-merge from the CLI). `gh` is the optional GitHub CLI (`brew install gh`, `gh auth login`); the web UI does the same job.

If you have write access these recipes are run against a direct clone of the canonical repo, where `origin` is the canonical repo. If you are working from a fork, branch from the canonical repo's state (via the `upstream` remote), push the branch to your fork, and open the PR back to the canonical repo.

### Start a feature or develop-line fix

```
git switch develop
git pull
git switch -c feature/4.0/new-export-api    # or bugfix/<desc> for a fix
# ...work, commit...
git push -u origin feature/4.0/new-export-api
```

Then open a PR into `develop`:

```
gh pr create --base develop --head feature/4.0/new-export-api --title "New export API"
```

or open it in the GitHub UI. After approval, merge the PR in the UI.

### Contribute a fix to a release branch

For a fix to a release being stabilized, base the branch on the `release/*` branch instead of `develop`, and target the PR at the release branch:

```
git switch release/4.0.0
git pull
git switch -c bugfix/some-fix
# ...fix, commit...
git push -u origin bugfix/some-fix
gh pr create --base release/4.0.0 --head bugfix/some-fix --title "Fix ..."
```

From a fork, replace the first two commands with `git fetch upstream` and `git switch -c bugfix/some-fix upstream/release/4.0.0`.

Release, tagging, and support-line operations are deliberately not listed here. They are maintainer actions performed on a direct clone of the canonical repo and are documented in the [release procedures](release-procedures.md).
