---
name: educates-upgrade-theme-libraries
description: Upgrade the vendored third-party frontend libraries bundled into the Educates workshop dashboard theme (Bootstrap, Font Awesome, jQuery, Underscore.js, JSONForm, js-yaml). Invoke when asked to "update theme libraries", "upgrade bootstrap/font-awesome/jquery/underscore/jsonform/js-yaml", "bump the dashboard frontend libraries", or "refresh the vendored web assets".
argument-hint: "[<library-name>] (omit to check/update all six)"
allowed-tools: "Read, Bash, WebFetch, Glob, Edit"
---

# Educates Workshop Theme Libraries Upgrade Protocol

Refresh the third-party browser libraries vendored into the workshop dashboard
theme. These assets are **committed directly into the repository** and have no
build-time fetcher — this skill is how they are updated: it re-downloads a
library from its upstream source and writes the files back into the tree.

If `$ARGUMENTS` names a specific library (e.g. `bootstrap`, `jquery`), update
only that one. With no argument, check all six for newer upstream versions and
report what's available before changing anything.

## Canonical location

All libraries live under one directory (the `educates` dashboard theme's static
assets):

```
workshop-images/base-environment/opt/eduk8s/etc/themes/educates/static/static/libraries/
```

Call this `$LIB` below. The target filenames are **version-agnostic** (no
version in the path), so an upgrade overwrites the existing files in place — no
renaming or `git rm` of old files is needed (except Font Awesome / Bootstrap if
upstream ever changes which files ship).

## Libraries, sources, and target files

Two shapes: **zip subset** (download a release zip, extract a few files,
stripping the zip's top-level folder) and **single file** (download one JS file
directly). Versions shown are the current committed ones — confirm against the
tree before bumping (see "Finding current versions").

| Library | Shape | Upstream source (version in URL) | Files written under `$LIB` |
|---|---|---|---|
| `bootstrap` | zip subset | `https://github.com/twbs/bootstrap/releases/download/v<VER>/bootstrap-<VER>-dist.zip` | `bootstrap/css/bootstrap.min.css`, `bootstrap/js/bootstrap.bundle.min.js` |
| `font-awesome` | zip subset | `https://github.com/FortAwesome/Font-Awesome/releases/download/<VER>/fontawesome-free-<VER>-web.zip` | `font-awesome/css/all.min.css` + `font-awesome/webfonts/{fa-brands-400,fa-regular-400,fa-solid-900,fa-v4compatibility}.{woff2,ttf}` (8 webfonts) |
| `jquery` | single file | `https://cdn.jsdelivr.net/npm/jquery@<VER>/dist/jquery.min.js` | `jquery/jquery.min.js` |
| `underscore` | single file | `https://cdn.jsdelivr.net/npm/underscore@<VER>/underscore-umd-min.js` | `underscore/underscore-umd-min.js` |
| `jsonform` | single file | `https://cdn.jsdelivr.net/npm/jsonform@<VER>/lib/jsonform.min.js` | `jsonform/jsonform.min.js` |
| `js-yaml` | single file | `https://cdn.jsdelivr.net/npm/js-yaml@<VER>/dist/js-yaml.min.js` | `js-yaml/js-yaml.min.js` |

For the two zip-subset libraries, only the files listed above are kept; the
rest of the release archive is discarded.

## Keep aligned with the renderer and gateway apps

These same libraries are also npm dependencies of the two bundled apps in the
base environment — `workshop-images/base-environment/opt/renderer` and
`.../opt/gateway` — which compile them into their own JavaScript via esbuild.
The vendored theme copy and the npm-bundled copy must stay on the **same
version**, or the dashboard's static assets drift from what the apps ship.

Whenever you change a vendored library version here, update the matching npm
dependency in each app's `package.json` that declares it, and vice versa:

| Theme library | npm package | `renderer/package.json` | `gateway/package.json` |
|---|---|---|---|
| `bootstrap` | `bootstrap` | yes | yes |
| `font-awesome` | `@fortawesome/fontawesome-free` | yes | yes |
| `jquery` | `jquery` | yes | yes |
| `js-yaml` | `js-yaml` | yes | yes |
| `underscore` | `underscore` | yes | — |
| `jsonform` | `jsonform` | yes | — |

The `package.json` versions are caret ranges (e.g. `"bootstrap": "^5.3.2"`).
Set the range to the version you vendored (`^<VER>`), then refresh the lockfile
and rebuild so the bundle matches:

```bash
cd workshop-images/base-environment/opt/<app>   # renderer and/or gateway
npm install        # updates package-lock.json to the new version
```

The app bundle is produced by `scripts/compile.sh` (`tsc` + `esbuild`); it runs
during the base-environment image build, so a `make image-base-environment` (or
running `compile.sh` in the app dir) is what proves the new version compiles and
bundles cleanly.

## Finding current versions

The committed minified files carry a version banner in their first line(s):

```bash
LIB=workshop-images/base-environment/opt/eduk8s/etc/themes/educates/static/static/libraries
head -c 200 "$LIB/bootstrap/css/bootstrap.min.css"      # "Bootstrap v5.3.8"
head -c 120 "$LIB/jquery/jquery.min.js"                  # "jQuery v3.7.1"
head -c 200 "$LIB/font-awesome/css/all.min.css"          # "Font Awesome Free 6.5.1"
head -c 200 "$LIB/underscore/underscore-umd-min.js"      # underscore version
head -c 200 "$LIB/js-yaml/js-yaml.min.js"
head -c 200 "$LIB/jsonform/jsonform.min.js"
```

## Finding the latest upstream version

- **bootstrap**: latest release at `https://github.com/twbs/bootstrap/releases` (use the latest stable `v5.x`, not a beta/alpha).
- **font-awesome**: latest "Free for Web" release at `https://github.com/FortAwesome/Font-Awesome/releases` (the asset named `fontawesome-free-<ver>-web.zip`).
- **jquery / underscore / jsonform / js-yaml**: the npm version. Check `https://www.npmjs.com/package/<name>` (or `https://registry.npmjs.org/<name>/latest`). jsDelivr serves any published npm version at the URL pattern above.

Use WebFetch to read the releases/npm page and pick the latest stable version. For major-version bumps, read the changelog for breaking changes and flag them to the user before proceeding.

## Update process

Set up the path and a scratch dir:

```bash
LIB=workshop-images/base-environment/opt/eduk8s/etc/themes/educates/static/static/libraries
TMP=$(mktemp -d)
```

### Single-file libraries (jquery, underscore, jsonform, js-yaml)

Just overwrite the file from the versioned jsDelivr URL. Example (jquery → `<VER>`):

```bash
curl -fsSL "https://cdn.jsdelivr.net/npm/jquery@<VER>/dist/jquery.min.js" -o "$LIB/jquery/jquery.min.js"
```

Use the matching URL + target from the table for underscore, jsonform, js-yaml. Always use `curl -f` so a 404 (bad version/path) fails loudly instead of writing an error page into the asset.

### Zip-subset libraries (bootstrap, font-awesome)

Download the release zip, then copy only the listed files, stripping the zip's
top-level folder. Bootstrap example (`<VER>` e.g. `5.3.8`):

```bash
curl -fsSL "https://github.com/twbs/bootstrap/releases/download/v<VER>/bootstrap-<VER>-dist.zip" -o "$TMP/bootstrap.zip"
unzip -q "$TMP/bootstrap.zip" -d "$TMP/bootstrap"
ROOT="$TMP/bootstrap/bootstrap-<VER>-dist"
cp "$ROOT/css/bootstrap.min.css" "$LIB/bootstrap/css/bootstrap.min.css"
cp "$ROOT/js/bootstrap.bundle.min.js" "$LIB/bootstrap/js/bootstrap.bundle.min.js"
```

Font Awesome example (`<VER>` e.g. `6.5.1`):

```bash
curl -fsSL "https://github.com/FortAwesome/Font-Awesome/releases/download/<VER>/fontawesome-free-<VER>-web.zip" -o "$TMP/fa.zip"
unzip -q "$TMP/fa.zip" -d "$TMP/fa"
ROOT="$TMP/fa/fontawesome-free-<VER>-web"
cp "$ROOT/css/all.min.css" "$LIB/font-awesome/css/all.min.css"
for f in fa-brands-400 fa-regular-400 fa-solid-900 fa-v4compatibility; do
  cp "$ROOT/webfonts/$f.woff2" "$LIB/font-awesome/webfonts/$f.woff2"
  cp "$ROOT/webfonts/$f.ttf"   "$LIB/font-awesome/webfonts/$f.ttf"
done
```

Before copying, `ls "$ROOT/css" "$ROOT/webfonts"` to confirm upstream still ships
these exact filenames; Font Awesome occasionally changes its webfont set across
major versions. If the set changed, update the table and the file list, and
`git rm` any file no longer shipped so stale assets don't linger.

Clean up: `rm -rf "$TMP"`.

## Verification

1. Each updated file is non-empty and carries the new version banner:
   ```bash
   head -c 200 "$LIB/<library>/<file>"
   ```
   Confirm the version string matches what you intended (catches a silently
   downloaded error page or a wrong path).
2. The downloaded file is the minified asset, not HTML (a 404 page). `file` or a
   quick `head` should show JS/CSS/font content, not `<!DOCTYPE html>`.
3. `git diff --stat` shows the expected files under `$LIB/<library>/` **and**,
   when the version changed, the matching `package.json` (+ `package-lock.json`)
   in `renderer`/`gateway` — see "Keep aligned with the renderer and gateway
   apps". A theme-only change with no package.json update is usually a drift bug.
4. Full check (optional, heavy): rebuild the base environment image so the theme
   bundles the new assets:
   ```bash
   make image-base-environment
   ```
   then load a workshop and confirm the dashboard renders (no missing-asset 404s
   in the browser console).

## Notes and gotchas

- These files are committed to git, so the update IS the change — there's no
  separate sync step or lock file to regenerate.
- The libraries are not checksum-pinned; integrity rests on HTTPS plus the
  version-banner and not-HTML checks in Verification above.
- Keep the target filenames exactly as listed — the theme's HTML/templates
  reference them by these stable names (e.g. `jquery.min.js`,
  `bootstrap.bundle.min.js`, `underscore-umd-min.js`).
- This skill only covers these six dashboard frontend libraries; it does not
  touch base-image CLI tools (helm, yq, hugo, …) or any other software.

## Post-upgrade checklist

- [ ] Requested library/libraries downloaded from the correct versioned URL
- [ ] Files written to the exact target paths with stable filenames
- [ ] Version banner in each file matches the intended version
- [ ] No error-page HTML written into an asset (`curl -f` used)
- [ ] Font Awesome / Bootstrap: upstream file set unchanged, or table + stale files updated
- [ ] Matching npm dependency aligned to the same version in `renderer/package.json` and `gateway/package.json` (where present), with `npm install` run to update each lockfile
- [ ] `git diff --stat` limited to the intended library directories plus the aligned `package.json`/`package-lock.json` files
- [ ] Major-version bumps: changelog reviewed for breaking changes and flagged
