Version 4.0.0
=============

New Features
------------

* ...

Features Changed
----------------

* The training portal has been made more resilient when allocating and
  managing workshop sessions under load. Previously, when many sessions were
  being requested at the same time, or when the Kubernetes REST API was slow
  to respond or applying throttling, session allocation could fail or requests
  could back up, sometimes surfacing as a ``database is locked`` error or as
  failed or timed out session requests. Updates the training portal makes to
  Kubernetes are now performed in a way that no longer blocks other session
  activity while waiting on the Kubernetes API, reducing failed sessions and
  improving reliability under high traffic.

* The browser JavaScript bundles for the workshop renderer and gateway
  applications in the workshop base environment image are now generated using
  esbuild instead of browserify. This eliminates security alerts arising from
  the deprecated elliptic package which was an indirect dependency of
  browserify, but for which no fixed version is available.

* The default Hugo theme used when rendering workshop instructions has been
  replaced with a new self contained implementation which no longer depends on
  JavaScript code generated from the classic renderer. The original theme is
  still included under the name ``educates-classic`` and can be selected by
  setting the ``WORKSHOP_RENDERER_THEME`` environment variable for a workshop
  to ``educates-classic`` if any issues are encountered with the new theme. The
  recommended way of setting this environment variable is via the ``env``
  override for a workshop in the ``TrainingPortal`` resource definition. The
  ``educates-classic`` theme will be removed in a future version at the same
  time as the classic renderer is removed.

* The ``baseurl`` short code for the Hugo renderer no longer includes a
  trailing slash when expanded, so the documented usage of
  ``{{<baseurl>}}/path`` now produces a URL with a single slash. Previously the
  expansion always ended in a slash, resulting in a repeated slash. Although
  web servers will generally collapse the repeated slash when workshop
  instructions are served from a workshop session, in the case of static HTML
  files generated using ``educates render-workshop``, where URLs are relative
  to the root, the result was a URL starting with a double slash, which a
  browser would incorrectly interpret as a scheme relative URL. Note that if
  workshop instructions relied on the undocumented form ``{{<baseurl>}}path``,
  without a slash after the short code, they will need to be updated to use the
  documented form.

* If a user ends a workshop session, or attempts to return to one, after their
  training portal login session has already expired, for example after leaving
  the browser window open for a long period after the workshop session had
  finished, they are now shown the workshop session finished page rather than
  being redirected to the portal login page. Previously an expired login in
  this situation could result in the portal login page being displayed, which
  was undesirable when workshop sessions are coordinated by a custom front end
  through the training portal REST API.

Bugs Fixed
----------

* ...
