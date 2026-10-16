How each cluster service is provided is fixed for the life of an install
========================================================================

The ``EducatesClusterConfig`` fixes, through CEL transition rules, how each
cluster service is provided: ``spec.ingress.certificates.provider``,
``spec.ingress.controller.provider``, ``spec.dns.provider``, and the policy
enforcement engines and Kyverno provider, together with the presence of
``spec.policyEnforcement`` and its ``kyverno`` block. Changing any of them
means deleting Educates and installing it again, or recreating the cluster.
Settings inside a service the operator installs stay changeable, and so does
the service's version, which an operator upgrade brings with its vendored
chart. Teardown relies on the rules: the spec seen at deletion is the spec
the install ran with, so the operator removes a cluster service only when
that spec says it installed it. Reversing this after release would mean
giving the operator a record of what it installed and a transition for every
pair of providers.

Considered options
------------------

Supporting provider changes in place was rejected. Each pair of providers
would need its own transition, and some cannot be made safely on a running
cluster: switching the certificate provider replaces the wildcard
certificate every training portal and workshop session serves, and switching
away from a bundled ingress controller removes the controller serving every
ingress. A transition that fails partway leaves a cluster half switched.
Educates clusters are ephemeral, and a local one is recreated in minutes, so
the transitions would be costly code for an operation users rarely need.

Recording in status which releases the operator installed, and removing
exactly those on teardown whatever the spec says, was rejected. The record
only serves provider switches, which the rules now forbid. It has gaps of its
own: an install that fails partway, or an operator that stops between
installing a release and recording it, leaves a release the record does not
name. And it would be a second account of ownership beside the spec, able to
disagree with it.

Leaving the providers changeable and teardown unconditional, as the 4.0
alpha releases did, was rejected. Deleting Educates removed a cert-manager,
Contour, external-dns or Kyverno the administrator had installed under the
same release and namespace names, breaking other workloads on the cluster,
and switching a provider left the bundled service running beside its
replacement.

Consequences
------------

An update that changes one of these fields is rejected by the API server
with a message naming the field. ``educates admin platform deploy`` turns
each rejection into an explanation that points at ``educates admin platform
delete`` or recreating the cluster, and a CLI test checks that the embedded
CRD carries every rule's message, so a new rule cannot go unexplained.

A value is frozen even between two providers that install nothing, such as
``None`` and ``Manual`` for DNS, which keeps each rule a plain comparison. An
absent ``spec.dns`` reads as provider ``None``, so the block can be added or
removed only when its provider is ``None``. The Kyverno fields are frozen
one by one rather than through a CEL copy of the operator's install
condition, so the rules never restate install logic. Removing
``spec.ingress`` once it is set is rejected, since it would otherwise let a
provider be swapped by dropping the block and adding it back.

A new cluster service, or a new provider for an existing one, follows the
same rule: its provider field gets a CEL transition rule beside the others,
and its teardown runs only when the operator's install condition for it
holds, as ``cleanupCertManager`` does with ``shouldInstallCertManager``.
