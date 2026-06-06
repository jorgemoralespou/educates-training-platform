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
