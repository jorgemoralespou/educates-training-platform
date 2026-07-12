---
name: educates-release-notes
description: Create release notes for an Educates version. Invoke when asked to "create release notes", "prepare for a release", "generate release notes", or "document changes for version X.Y.Z". Creates a versioned markdown file in project-docs/release-notes/ and updates project-docs/index.rst.
argument-hint: "<version> (e.g. 4.1.0)"
allowed-tools: "Read, Glob, Bash"
---

# Educates Release Notes Creation

Create release notes for version `$ARGUMENTS` of the educates-training-platform.

If no version is specified in `$ARGUMENTS`, check `project-docs/release-notes/` for the latest `version-*.md` file and increment the patch version.

## Step 1: Determine the Version

Use the version from `$ARGUMENTS` (e.g. `4.1.0`). If not provided:

```bash
ls project-docs/release-notes/version-*.md | sort -V | tail -1
```

Increment the patch version (or minor/major as appropriate).

## Step 2: Find the Previous Release

```bash
ls project-docs/release-notes/version-*.md | sort -V
```

Identify the version immediately before the target version. Then find its git tag or the commit where it was merged. Educates release tags are plain `X.Y.Z` (and `X.Y.Z-alpha.N`/`-beta.N`/`-rc.N` pre-releases) with **no `v` prefix**:

```bash
git tag --sort=-version:refname | grep -E '^[0-9]' | head
git log --oneline --grep="version-<previous>" -- project-docs/release-notes/
```

## Step 3: Analyze Git Changes

Get all commits since the previous release:

```bash
git log --oneline <previous-tag-or-commit>..HEAD
```

Get changed files:

```bash
git diff --name-only <previous-tag-or-commit>..HEAD
git diff --stat <previous-tag-or-commit>..HEAD
```

Get commit messages formatted for analysis:

```bash
git log --pretty=format:"%s" <previous-tag-or-commit>..HEAD
```

Check for merge commits (feature branches):

```bash
git log --merges <previous-tag-or-commit>..HEAD
```

## Step 4: Categorize Changes

Map changes to sections by scanning commits and file diffs:

| Category | What to look for |
|---|---|
| **New Features** | New files, new commands, new config options; commit keywords: "add", "new", "feature", "support for" |
| **Features Changed** | Modified existing files, version bumps, config changes; keywords: "update", "change", "modify", "improve", "refactor" |
| **Bugs Fixed** | Error handling, corrections; keywords: "fix", "bug", "error", "issue", "correct" |
| **Deprecations** | Things still supported but on the way out; keywords: "deprecate", "deprecated". Note the removal timeline (and any upstream timeline it tracks). |
| **Known Issues** | Manual entry — from issue tracker or known limitations |

A user-facing change is only "done" when it has a release-notes entry — this is a
project norm (see `CLAUDE.md`), so the running `version-*.md` may already carry
many entries. Treat your job as filling gaps and tidying, not writing from scratch.

Common patterns:
- `go.mod`/`go.sum` changes → dependency updates (Features Changed)
- `Dockerfile` changes → base image or tool version updates (Features Changed)
- New files in `project-docs/` → documentation (New Features or Features Changed)
- kubectl version blocks in `workshop-images/base-environment/Dockerfile` → Kubernetes version support change

## Step 5: Create the Release Notes File

Create `project-docs/release-notes/version-{x.y.z}.md` using this exact format:

```markdown
Version {x.y.z}
=============

New Features
------------

* Description of new feature.

Features Changed
----------------

* Description of changed feature.

Bugs Fixed
----------

* Description of bug fix.

Deprecations
------------

* Description of a feature that still works but is slated for removal, with the
  removal timeline (if any).

Known Issues
------------

* Description of known issue (if any).
```

Formatting rules:
- Main heading uses `=` underline (same length as the heading text)
- Section headings use `-` underline
- Bullet points use `*`
- Omit sections that have no entries
- Write in third person, concise, specific
- Include version numbers for dependency updates
- Clearly flag breaking changes

## Step 6: Update `project-docs/index.rst`

Read `project-docs/index.rst` and find the `Release Notes:` toctree section (around line 80–82). Add the new entry at the **top** of the list (newest first):

```rst
.. toctree::
  :maxdepth: 2
  :caption: Release Notes:

  release-notes/version-{x.y.z}
  release-notes/version-{previous}
  ...
```

Maintain 2-space indentation and keep entries sorted newest first.

## Step 7: Review

- Verify version number is correct and consistent in filename and heading
- Confirm all major changes from git log are covered
- Check formatting matches existing release notes files (read one for comparison)
- Verify the `index.rst` entry was added correctly and the file renders as valid RST
