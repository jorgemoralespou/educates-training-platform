# Scenario 04 — Custom default website theme

Validates the chart's `session-manager.websiteStyling.inline` block:
structured `{html,script,style}` triples that the chart maps to the
flat `<filename>: <content>` shape the runtime expects, written into
the `default-website-theme` Secret in the operator namespace.

## What's tested

End-to-end: the custom theme value must reach the live portal HTML
served by the training-portal pod.

`post-deploy.sh` curls the portal URL (resolved from
`status.educates.url` on the TrainingPortal CR) and greps for a
unique marker that `chart-values.yaml` put into `training-portal.html`.
This exercises the whole chain — chart → `educates-config` Secret →
session-manager pickup → training-portal Deployment → HTTP response —
not just the chart's Secret-rendering.

## Layout

Same as scenario 01 (HTTP, nip.io domain, no TLS) — the website-theme
value is orthogonal to TLS, so we use the simplest base. The only
chart-values delta from 01 is the `websiteStyling.inline` block.

## Out of scope

- Custom themes propagated from a foreign namespace via
  `secretPropagation.upstream.websiteThemes`. That requires a
  Workshop/WorkshopEnvironment that references the theme by name and
  is its own scenario.
- Browser-rendered DOM verification (we curl the raw HTML and grep —
  enough to confirm session-manager applied the theme).
