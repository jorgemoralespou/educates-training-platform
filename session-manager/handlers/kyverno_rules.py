import os
import copy
import logging

import yaml

from wrapt import synchronized

from .helpers import xget

logger = logging.getLogger("educates.kyverno_rules")

# Bundled + user-supplied per-workshop policies, loaded from the
# educates-config Secret rendered by the session-manager chart. Our
# shipped rules are Kyverno ValidatingPolicy (policies.kyverno.io);
# user-supplied workshopSecurity.additionalKyvernoPolicies may still be
# the legacy ClusterPolicy (kyverno.io), which we keep scoping but
# deprecate.
kyverno_policies = []

if os.path.exists("/opt/app-root/config/kyverno-policies.yaml"):
    with open("/opt/app-root/config/kyverno-policies.yaml") as fp:
        kyverno_policies = list(yaml.load_all(fp.read(), Loader=yaml.Loader))

# Kinds in the policies.kyverno.io group whose per-workshop scoping is a
# matchConstraints.namespaceSelector injection (all VAP-shaped). We ship
# ValidatingPolicy today; MutatingPolicy and the other new Policy types
# share the same matchConstraints shape and can be added here when we
# start shipping them.
NEW_POLICY_KINDS = frozenset({"ValidatingPolicy"})


def _session_namespace_selector(environment_name):
    """Selector restricting a policy to a workshop environment's session
    namespaces — the same expressions the legacy path injected into
    ClusterPolicy `match` blocks."""
    return {
        "matchExpressions": [
            {
                "key": "training.educates.dev/environment.name",
                "operator": "In",
                "values": [environment_name],
            },
            {
                "key": "training.educates.dev/component",
                "operator": "In",
                "values": ["session"],
            },
        ]
    }


def _validation_actions(action):
    """Map the workshop rules action onto ValidatingPolicy
    spec.validationActions. Enforce blocks (Deny); Audit only records."""
    if action == "Audit":
        return ["Audit"]
    return ["Deny"]


def _scoped_validating_policy(policy, workshop_spec, environment_name):
    """Build a per-environment copy of a new-style Policy, scoped to the
    environment's session namespaces via matchConstraints.namespaceSelector.
    Returns None when the policy is excluded."""
    action = xget(workshop_spec, "session.namespaces.security.rules.action", "Enforce")
    exclude = xget(workshop_spec, "session.namespaces.security.rules.exclude", [])

    policy_name = xget(policy, "metadata.name")
    if policy_name in exclude:
        return None

    spec = copy.deepcopy(xget(policy, "spec", {}))

    # Inject the session-namespace selector. Append to any selector the
    # source policy already carries (AND semantics) rather than replace it.
    match_constraints = spec.setdefault("matchConstraints", {})
    namespace_selector = match_constraints.setdefault("namespaceSelector", {})
    expressions = namespace_selector.setdefault("matchExpressions", [])
    expressions.extend(_session_namespace_selector(environment_name)["matchExpressions"])

    # The workshop's rules action governs, overriding the bundled policy's
    # own validationActions (matching the legacy validationFailureAction
    # behaviour).
    if policy.get("kind") == "ValidatingPolicy":
        spec["validationActions"] = _validation_actions(action)

    return {
        "apiVersion": policy["apiVersion"],
        "kind": policy["kind"],
        "metadata": {
            "name": f"educates-environment-{environment_name}-{policy_name}",
        },
        "spec": spec,
    }


def _scoped_cluster_policy_rules(policy, workshop_spec, environment_name):
    """Legacy ClusterPolicy path (deprecated): pull each rule out, add a
    session-namespace selector to its match targets, and return the rules
    to be merged into one per-environment ClusterPolicy. Emits a
    deprecation warning."""
    exclude = xget(workshop_spec, "session.namespaces.security.rules.exclude", [])

    policy_name = xget(policy, "metadata.name")

    logger.warning(
        "Workshop Kyverno ClusterPolicy %r is deprecated; migrate it to a "
        "ValidatingPolicy (policies.kyverno.io). ClusterPolicy is deprecated "
        "in Kyverno 1.18 and removed in 1.20, and Educates will drop support "
        "for workshop-provided ClusterPolicies on the same timeline.",
        policy_name,
    )

    if policy_name in exclude:
        return []

    policy_rules = xget(policy, "spec.rules", [])
    rules = []

    for index, rule in enumerate(policy_rules, start=1):
        rule = copy.deepcopy(rule)

        if len(policy_rules) != 1:
            rule["name"] = f"{policy_name}/{index}"
        else:
            rule["name"] = policy_name

        # Add a namespace selector to each resource which is a target of the
        # rule, so the rule only applies to this environment's session
        # namespaces.
        resources = [condition.get("resources", {}) for condition in xget(rule, "match.all", [])]

        if not resources:
            resources = [condition.get("resources", {}) for condition in xget(rule, "match.any", [])]

        if not resources:
            resources = [xget(rule, "match.resources", {})]

        if not resources:
            continue

        for resource in resources:
            resource["namespaceSelector"] = _session_namespace_selector(environment_name)

        rules.append(rule)

    return rules


def _environment_rules(policies, workshop_spec, environment_name):
    """Core, testable: turn the per-workshop policy stream into the
    environment-scoped policy objects to create. New-style Policy types
    each become their own scoped object; legacy ClusterPolicy rules are
    merged into a single ClusterPolicy as before."""
    action = xget(workshop_spec, "session.namespaces.security.rules.action", "Enforce")

    objects = []
    cluster_policy_rules = []

    for policy in policies:
        if not policy:
            continue

        kind = policy.get("kind")

        if kind in NEW_POLICY_KINDS:
            scoped = _scoped_validating_policy(policy, workshop_spec, environment_name)
            if scoped is not None:
                objects.append(scoped)
        elif kind == "ClusterPolicy":
            cluster_policy_rules.extend(
                _scoped_cluster_policy_rules(policy, workshop_spec, environment_name)
            )
        else:
            logger.warning(
                "Ignoring workshop policy %r with unsupported kind %r; expected "
                "a policies.kyverno.io Policy type (e.g. ValidatingPolicy).",
                xget(policy, "metadata.name"),
                kind,
            )

    if cluster_policy_rules:
        objects.append(
            {
                "apiVersion": "kyverno.io/v1",
                "kind": "ClusterPolicy",
                "metadata": {
                    "name": f"educates-environment-{environment_name}",
                },
                "spec": {
                    "validationFailureAction": action,
                    "background": True,
                    "rules": cluster_policy_rules,
                },
            }
        )

    return objects


@synchronized
def kyverno_environment_rules(workshop_spec, environment_name):
    return _environment_rules(kyverno_policies, workshop_spec, environment_name)
