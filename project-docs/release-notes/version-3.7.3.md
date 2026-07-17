Version 3.7.3
=============

This version is a patch release of the 3.7 support line. The changes in this
release were developed on the main development branch for the upcoming 4.0
release and have been back ported to the 3.7 support line.

Features Changed
----------------

* The training portal has been made more resilient when allocating and
  managing workshop sessions under load. Previously, when many sessions were
  being requested at the same time, or when the Kubernetes REST API was slow
  to respond or applying throttling, session allocation could fail or requests
  could back up, sometimes surfacing as a `database is locked` error or as
  failed or timed out session requests. Updates the training portal makes to
  Kubernetes are now performed in a way that no longer blocks other session
  activity while waiting on the Kubernetes API, reducing failed sessions and
  improving reliability under high traffic.
