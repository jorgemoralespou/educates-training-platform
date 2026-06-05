Version 3.7.2
=============

Features Changed
----------------

- When session manager was started it would log Educates configuration at log
  level of INFO for debugging purposes. If log files were being sent to a
  centralised logging service, this would result in training portal credentials
  being sent as default log level was INFO. Configuration now logged at DEBUG
  log level and will only be logged by the session manager if log level was
  manually overridden to be DEBUG.
