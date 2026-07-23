Version 3.8.0
=============

This version is the first release of the 3.8 support line, which carries on
from the 3.7 support line. While development of the next major version of
Educates continues, the 3.8 support line will be used to keep the current
major version up to date with security fixes and bug fixes, with selected
features also able to be back ported where needed to support user
requirements. The changes in this release were developed on the main
development branch for the upcoming 4.0 release and have been back ported to
the 3.8 support line. They update container base images, language runtimes,
bundled tools and package dependencies to eliminate known security
vulnerabilities, and refresh the supported Kubernetes version range so the
release remains usable with current Kubernetes clusters for as long as
possible.

Features Changed
----------------

* The Zot Registry used by the OCI image cache for workshop environments has
  been updated from version 1.4.3 to 2.1.18, resolving a large number of
  critical and high severity vulnerabilities reported against the old
  version. The format of the synchronization rules able to be supplied under
  ``environment.images.registries`` in the workshop definition is unchanged.
  Any image cache contents from an existing deployment are re-indexed or
  re-synchronized on demand, so no action is required when upgrading.

* The container base images used for the training portal, session manager,
  secrets manager, lookup service, tunnel manager, image cache and workshop
  base environment have been updated from Fedora 42 to Fedora 44, eliminating
  known vulnerabilities reported against operating system packages of the
  older base image. The Python package dependencies of the Python based
  platform services have also been updated to current versions.

* The training portal now runs on Python 3.14 and has been upgraded from
  Django 4.2 LTS to Django 5.2 LTS, along with updates to the other Python
  packages it uses. This ensures the key frameworks the training portal is
  built on continue to receive security fixes for an extended period.

* The assets server is now built as a static binary and runs from a minimal
  ``scratch`` container image instead of a Fedora based image, so it no
  longer includes any operating system packages against which security
  vulnerabilities could be reported.

* The browser JavaScript bundles for the workshop renderer and gateway
  applications in the workshop base environment image are now generated using
  esbuild instead of browserify. This eliminates security alerts arising from
  the deprecated elliptic package which was an indirect dependency of
  browserify, but for which no fixed version is available.

* The workshop base environment image has been updated to use Node.js 24, as
  the Node.js 20 version previously used has reached end of life. The NPM
  package dependencies of the gateway, renderer and helper applications of
  the workshop base environment, and of the Docker desktop extension, have
  also been updated to resolve reported package security advisories.

* Third party tools included in the workshop base environment image, such as
  ``yq``, Hugo and ``uv``, have been updated to current versions.

* The ``helm`` CLI included in the workshop base environment image has been
  updated from the 3.x series to 4.x. Helm 4 is a new major release which
  introduces breaking changes relative to Helm 3, so any workshops which use
  the ``helm`` CLI should be verified against the newer Helm version to ensure
  they still behave as expected.

* The set of ``kubectl`` versions bundled in the workshop base environment
  image has changed. The 1.31 and 1.32 versions have been dropped and 1.35 and
  1.36 have been added, so the supported range is now 1.33 to 1.36. The
  ``kubectl`` version is still selected automatically to match the Kubernetes
  cluster the workshop session is connected to. For clusters older than the
  supported range the oldest bundled version, 1.33, is used, and for clusters
  newer than the supported range the most recent bundled version, 1.36, is
  used. Workshops which target clusters running Kubernetes 1.32 or older may
  therefore see a larger client/server version skew and should be verified
  against a supported cluster version.

* The version of ``kind`` embedded in the ``educates`` CLI has been updated
  from 0.29 to 0.32. As a result the default Kubernetes version used when
  creating a local cluster with ``educates local cluster create`` has changed
  from 1.33 to 1.36. A specific node image can still be selected using the
  ``--kind-cluster-image`` option if a different Kubernetes version is
  required.

* The JDK versions included in the ``jdk8-environment``, ``jdk11-environment``,
  ``jdk17-environment`` and ``jdk21-environment`` workshop base images have
  been updated to the latest Eclipse Temurin patch releases, being 8u502-b07,
  11.0.32+9, 17.0.20+8 and 21.0.12+8 respectively. The bundled Maven and
  Gradle versions have also been updated. All four images now include Maven
  3.9.16. The ``jdk8-environment`` and ``jdk11-environment`` images include
  Gradle 8.14.5, the latest version able to run on those JDK versions, while
  the ``jdk17-environment`` and ``jdk21-environment`` images include Gradle
  9.6.1. Gradle 9 is a new major release which introduces breaking changes
  relative to Gradle 8, so any workshops which use Gradle in the JDK 17 or
  JDK 21 images should be verified against the newer Gradle version to ensure
  they still behave as expected. One behaviour change affects all four
  images: ``gradle init`` now aborts if the target directory already
  contains any files, including hidden files, where previously it would
  generate the new project alongside them. Since the home directory of a
  workshop session is never empty, any workshop which has users run
  ``gradle init`` directly in the home directory, or any other non-empty
  directory, will need its instructions updated to either pass the
  ``--overwrite`` option to ``gradle init`` or create the project in a new
  empty subdirectory.

* The ``sshd`` configuration used for SSH access to workshop sessions has
  been hardened. Public key authentication is now explicitly required, root
  login is disallowed, SSH agent forwarding is disabled, and TCP forwarding
  is restricted to the local direction, meaning a workshop session can still
  be used as an SSH jump host, but remote port forwarding is no longer
  permitted. The ``sshd`` log level has also been reduced from debug level,
  which the OpenSSH documentation notes violates user privacy, to the
  standard informational level.

* If a user ends a workshop session, or attempts to return to one, after their
  training portal login session has already expired, for example after leaving
  the browser window open for a long period after the workshop session had
  finished, they are now shown the workshop session finished page rather than
  being redirected to the portal login page. Previously an expired login in
  this situation could result in the portal login page being displayed, which
  was undesirable when workshop sessions are coordinated by a custom front end
  through the training portal REST API.

* The training portal has reduced the number of requests it makes against the
  Kubernetes REST API when checking for workshop sessions which were deleted
  out of band, such as when a ``WorkshopSession`` resource is deleted
  manually. A single query is now used to retrieve the set of deployed
  workshop sessions belonging to the training portal, rather than a separate
  query being made for each individual workshop session, and the check now
  runs once a minute instead of every fifteen seconds. On training portals
  hosting a large number of workshop sessions the previous behaviour could
  overload the Kubernetes REST API, resulting in errors when accessing the
  Kubernetes cluster. In addition, a workshop session database record which
  never had a corresponding deployment created, for example because the
  training portal was restarted at the wrong time, is now cleaned up after a
  grace period rather than indefinitely counting against the capacity of the
  workshop environment.

Bugs Fixed
----------

* When reserved workshop sessions were enabled for a workshop environment
  along with a timeout for deleting orphaned workshop sessions, a workshop
  session which had sat in reserve for longer than the orphaned timeout could
  be deleted moments after being allocated to a user. This was because the
  idle time reported by a workshop session is measured from when the workshop
  session was created, so the whole period a workshop session spent in
  reserve was counted as idle time. The reported idle time is now capped at
  the time which has elapsed since the workshop session was allocated to the
  user when checking whether a workshop session should be deleted due to
  being orphaned or inactive.
