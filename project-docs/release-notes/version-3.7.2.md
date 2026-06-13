Version 3.7.2
=============

Features Changed
----------------

* When session manager was started it would log Educates configuration at log
  level of INFO for debugging purposes. If log files were being sent to a
  centralised logging service, this would result in training portal credentials
  being sent as default log level was INFO. Configuration now logged at DEBUG
  log level and will only be logged by the session manager if log level was
  manually overridden to be DEBUG.

* Session IDs for workshop sessions are now obfuscated. So instead of it being
  of the form `snnn`, where `nnn` is a zero padded incrementing integer, you
  will now see a random sequence of lower case letters and numbers. This will
  be reflected in the session URL for accessing the workshop session making it
  harder to guess what the URL for any workshop session will be. As long as you
  use appropriate data variables in the workshop defintion or workshop
  instructions for constructing URLs and other entities based on the session
  name, existing workshops should continue to work without needing changes.

* The buttons to the right of the dashboard tabs now display tooltips indicating
  what the buttons are for. The tooltip message for the countdown timer will
  change to indicate when the session can be extended, in addition to the
  colour being changed to amber.

* When a second browser window was loaded on the workshop session dashboard,
  a "HIJACKED" banner would be displayed across the background of the terminals.
  This has been changed to display a more neutral popup message explain that
  multiple views with different sizes for terminals can cause problems. The
  recycle button has also been changed now to flag when underlying terminal
  size differs to current window size and when clicked will resize underlying
  terminal size back to the current window size. The "FORBIDDEN" banner
  message which could appear when a terminal connection was rejected for some
  reason has also been replaced with a popup.

Bugs Fixed
----------

* Due to a race condition, if `exit` was run in the terminal to exit the shell,
  it would immediately reconnect when it was supposed to rely on a user to
  manually click on the reload button above.

* Due to a race condition in the lookup service, introduced in version 3.7.1, if
  a workshop environment was seen before the training portal hosting it had been
  registered, an internal error would prevent that workshop environment from
  being registered. The affected workshops would then be missing from the list
  of available workshops and requests for sessions against those workshops would
  fail. Whether the problem occurred depended on timing of events when the
  lookup service started and so could vary across restarts.

* The lookup service `/auth/verify` endpoint, used to check whether an access
  token is still valid, always returned an HTTP 500 error due to the request
  handler not being implemented as an asynchronous function. It now correctly
  returns an HTTP 200 response for a valid token and an HTTP 401 response for an
  expired or invalid token.
