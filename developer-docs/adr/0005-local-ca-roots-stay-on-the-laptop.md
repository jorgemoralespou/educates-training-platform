Local CA roots stay on the laptop; clusters receive per-domain intermediates
===========================================================================

For local development, ``educates local secrets add ca`` creates or registers
a root CA whose private key never leaves the machine running the CLI. The
user imports that root into their operating system trust store once. When a
deploy needs a certificate issuer for an ingress domain, the CLI mints an
intermediate CA from the root, restricted by an X.509 name constraint to that
domain with every IP address range excluded, and only the intermediate's key
pair is copied to the cluster for cert-manager's CA issuer. One root is
allowed per CLI data home, and domains are read from certificates rather than
declared on the root. The layout of the CLI's local secret cache follows from
this, so changing it after release would mean migrating every user's cached
CAs.

Considered options
------------------

The root a user imports is trusted for every site, not only for Educates. Its
key, wherever it is stored, can mint a certificate the user's browser,
``curl``, ``git``, Go and Node will accept for any host. A local cluster runs
workshop code, often with broad permissions while workshops are being
developed, so a key placed in the cluster is exposed to that code. The
options differ in what a key taken from the cluster can sign.

Placing an unconstrained CA key in the cluster, which is what mkcert users
might expect, was rejected. mkcert's own root is equally unconstrained, but
mkcert keeps its key on the machine. A cluster holding such a key can be used
to impersonate any site to the user.

Constraining the root to the configured domain was rejected because it makes
each new local test domain need a new root and a new import into every
trust store, which works against quick iteration across projects. It also
cannot help a root the user supplies: a name constraint is part of the signed
CA certificate, so it cannot be added to an existing root such as mkcert's.

Constraining the root to the namespaces reserved for local use, such as
``test``, ``localhost``, ``internal``, ``home.arpa`` and wildcard IP DNS
services, was rejected for two reasons. Java and Android ignore name
constraints on a trust anchor, so a JVM inside a workshop session would still
accept any certificate the root signs. And a supplied root would still send
its unconstrained key to the cluster.

A per-domain intermediate has none of these problems. Name constraints on an
intermediate are enforced by browsers, the operating system verifiers,
OpenSSL, GnuTLS, Go, Node and Java alike. A key taken from the
cluster can sign only for the one domain it was minted for, and the
intermediate cannot create further CAs. The root, generated or supplied, has
the same exposure as mkcert's root today: it stays on the machine.

Consequences
------------

The CLI's local secret cache holds three kinds of CA material: a trust Secret
with the root certificate, the root key pair, which is never synced, and one
intermediate key pair per domain, which is synced only when a deploy selects
it. A deploy may write to the cache, minting an intermediate the first time a
domain is used or when its root has changed. Removing a root removes the
intermediates minted from it. The operator needs no special handling: the
intermediate's ``tls.crt`` carries the chain and its ``ca.crt`` the root, so
components trust the same root the user imported. cert-manager does not check
name constraints when it signs, so a certificate requested outside the domain
would be issued and then rejected by clients; Educates only requests the
domain and its wildcard.
