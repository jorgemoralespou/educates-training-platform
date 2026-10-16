CA trust reaches components only through a key-free Secret the operator owns
===========================================================================

When an ingress CA is configured, Educates components receive it as an Opaque
Secret named ``educates-ingress-ca`` in the operator namespace, holding a
single ``ca.crt`` key, and published in
``EducatesClusterConfig.status.ingress.caCertificateSecretRef``. The operator
derives it from what the user supplied: the CA trust Secret when one is given,
otherwise the CA certificate carried by the CA key pair that cert-manager
signs with in Managed mode, its ``ca.crt`` or else the root-most certificate
of its chain, or the CA Secret named in Inline mode. Components never read a
Secret the user created. The status field is the contract between the
operator and every component, the session manager, lookup service, training
portal, workshop sessions and node CA injector alike, so changing what it
promises later would touch all of them.

Considered options
------------------

The first alternative was to publish the user's own Secret in status, as
Inline mode first did. That was rejected because of how the Secret travels.
cert-manager's CA issuer needs ``tls.crt`` and ``tls.key`` in one Secret, so
the natural thing to reference in Managed mode is a Secret holding the CA
private key. Secret copiers copy a Secret's data verbatim, so a referenced
Secret lands in the Educates namespace and from there in every training portal
and workshop environment namespace, and several pods mount it whole. A key in
that Secret would therefore spread to dozens of namespaces. Validation that
rejects a Secret holding a key would move the burden onto every user and
still leave the contract only as safe as each user's Secret. cert-manager's
own documentation advises against reading ``ca.crt`` from a Secret that also
holds a private key.

The second alternative was to adopt cert-manager's trust-manager to distribute
CA trust. trust-manager builds a bundle from sources in its trust namespace
and writes it into a ConfigMap or Secret in every namespace a selector
matches. That is the distribution half of the problem, which Educates already
solves with Secret copiers tied to the lifecycle of portals and workshop
environments. It does not touch pods or container trust stores, which is the
hard half for workshop sessions. Adopting it would add a fifth cluster service
chart, its permissions in the generated charts role, a readiness gate, and a
story for Inline users who bring their own, all on a ``Bundle`` API that is
alpha and already scheduled for replacement by ``ClusterBundle``. The native
Kubernetes ClusterTrustBundle with projected volumes, generally available from
Kubernetes 1.37, is the better primitive to revisit once the minimum supported
Kubernetes version reaches it, or once Educates needs to place trust in
namespaces it does not own.

Consequences
------------

The operator owns a derived Secret and keeps it current by watching its
source. CA trust and the CA key pair are separate objects throughout the CLI
and the API, so users and tools handle two Secrets where one used to serve
both purposes. Components mount only the ``ca.crt`` key, so even a
misconfigured source cannot place a key in a pod's filesystem. A Secret
supplied as a session manager override follows the same derivation, so the
contract has no exception.
