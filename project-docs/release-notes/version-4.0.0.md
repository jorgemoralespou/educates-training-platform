Version 4.0.0
=============

New Features
------------

* ...

Features Changed
----------------

* The browser JavaScript bundles for the workshop renderer and gateway
  applications in the workshop base environment image are now generated using
  esbuild instead of browserify. This eliminates security alerts arising from
  the deprecated elliptic package which was an indirect dependency of
  browserify, but for which no fixed version is available.

Bugs Fixed
----------

* ...
