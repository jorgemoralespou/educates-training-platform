# Scenario 07 — `config:` escape-hatch deep-merge

Validates the chart's `session-manager.config` opaque map: a deep-merge
applied on top of the typed-derived `educates-operator-config.yaml`
content, with `config:` winning on conflict.

## What's tested

The rendered `educates-config` Secret in the operator namespace must
contain:

1. `dockerDaemon.networkMTU: 1450` — the chart-default for this typed
   field is `1400`. The escape hatch sets `1450` and must win.
2. `experimental.markerKey: scenario-07-marker` — an arbitrary key
   under an arbitrary block not in the typed surface, passed through
   untouched.

`post-deploy.sh` decodes the Secret and greps for both.

## Layout

Same as scenario 01 (HTTP, nip.io domain, no TLS) — the escape hatch
is orthogonal to TLS, so we use the simplest base. The only delta from
01 is the added `config:` block.

## Why this scenario exists

The typed-values refactor promoted ~15 well-known fields out of the
opaque `config:` map. `config:` survives as an escape hatch for runtime
fields that aren't yet typed. This scenario locks in the merge
semantics so future typed-promotion changes can't silently break the
override path.

## Out of scope

- The runtime *honouring* the escape-hatch values. The runtime knows
  about `dockerDaemon.networkMTU` (typed before this refactor), so
  setting it works end-to-end — but `experimental.markerKey` is not a
  real runtime field and is only checked at the Secret level. We
  validate the chart's behaviour, not the runtime's.
