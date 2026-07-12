"""Unit tests for per-workshop Kyverno policy scoping.

Exercises the testable core, ``_environment_rules``, directly with
synthetic policy documents so no cluster or config Secret is needed.
``handlers`` is imported as an implicit namespace package (the repo root
of session-manager is added to sys.path by conftest.py).
"""

from handlers.kyverno_rules import _environment_rules

ENV = "lab-test-w01"

SESSION_SELECTOR_KEYS = {
    "training.educates.dev/environment.name",
    "training.educates.dev/component",
}


def _validating_policy(name, action_default="Audit"):
    return {
        "apiVersion": "policies.kyverno.io/v1alpha1",
        "kind": "ValidatingPolicy",
        "metadata": {"name": name},
        "spec": {
            "validationActions": [action_default],
            "matchConstraints": {
                "resourceRules": [
                    {
                        "apiGroups": [""],
                        "apiVersions": ["v1"],
                        "operations": ["CREATE", "UPDATE"],
                        "resources": ["pods"],
                    }
                ]
            },
            "validations": [{"expression": "true", "message": "ok"}],
        },
    }


def _cluster_policy(name):
    return {
        "apiVersion": "kyverno.io/v1",
        "kind": "ClusterPolicy",
        "metadata": {"name": name},
        "spec": {
            "rules": [
                {
                    "name": name,
                    "match": {"resources": {"kinds": ["Pod"]}},
                    "validate": {"message": "no", "pattern": {}},
                }
            ]
        },
    }


def _selector_keys(namespace_selector):
    return {e["key"] for e in namespace_selector["matchExpressions"]}


def test_validating_policy_is_scoped_per_environment():
    out = _environment_rules([_validating_policy("disallow-host-path")], {}, ENV)

    assert len(out) == 1
    obj = out[0]
    assert obj["kind"] == "ValidatingPolicy"
    assert obj["apiVersion"] == "policies.kyverno.io/v1alpha1"
    assert obj["metadata"]["name"] == f"educates-environment-{ENV}-disallow-host-path"

    selector = obj["spec"]["matchConstraints"]["namespaceSelector"]
    assert _selector_keys(selector) == SESSION_SELECTOR_KEYS
    env_expr = next(
        e for e in selector["matchExpressions"]
        if e["key"] == "training.educates.dev/environment.name"
    )
    assert env_expr["values"] == [ENV]


def test_action_maps_to_validation_actions():
    # Default action (Enforce) -> Deny; the bundled file's own Audit is overridden.
    enforce = _environment_rules([_validating_policy("p", "Audit")], {}, ENV)
    assert enforce[0]["spec"]["validationActions"] == ["Deny"]

    audit_spec = {"session": {"namespaces": {"security": {"rules": {"action": "Audit"}}}}}
    audit = _environment_rules([_validating_policy("p")], audit_spec, ENV)
    assert audit[0]["spec"]["validationActions"] == ["Audit"]


def test_exclude_skips_validating_policy():
    spec = {"session": {"namespaces": {"security": {"rules": {"exclude": ["p"]}}}}}
    out = _environment_rules([_validating_policy("p"), _validating_policy("q")], spec, ENV)
    names = [o["metadata"]["name"] for o in out]
    assert names == [f"educates-environment-{ENV}-q"]


def test_legacy_cluster_policy_still_merged_with_warning(caplog):
    import logging

    with caplog.at_level(logging.WARNING, logger="educates.kyverno_rules"):
        out = _environment_rules([_cluster_policy("legacy-rule")], {}, ENV)

    assert len(out) == 1
    obj = out[0]
    assert obj["kind"] == "ClusterPolicy"
    assert obj["metadata"]["name"] == f"educates-environment-{ENV}"
    assert obj["spec"]["validationFailureAction"] == "Enforce"
    resources = obj["spec"]["rules"][0]["match"]["resources"]
    assert _selector_keys(resources["namespaceSelector"]) == SESSION_SELECTOR_KEYS
    assert any("deprecated" in r.message.lower() for r in caplog.records)


def test_mixed_stream_returns_both_shapes():
    out = _environment_rules(
        [_validating_policy("vp"), _cluster_policy("cp")], {}, ENV
    )
    kinds = sorted(o["kind"] for o in out)
    assert kinds == ["ClusterPolicy", "ValidatingPolicy"]


def test_unsupported_kind_skipped_with_warning(caplog):
    import logging

    weird = {"kind": "SomethingElse", "metadata": {"name": "weird"}, "spec": {}}
    with caplog.at_level(logging.WARNING, logger="educates.kyverno_rules"):
        out = _environment_rules([weird], {}, ENV)

    assert out == []
    assert any("unsupported kind" in r.message.lower() for r in caplog.records)
