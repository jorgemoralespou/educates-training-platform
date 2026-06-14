---
title: Workshop Summary
---

In this workshop you exercised remote SSH access to a workshop session over the
Educates websocket tunnelling proxy.

You confirmed that:

* With `session.applications.sshd` and its `tunnel` enabled, the session
  container runs an SSH daemon reachable through the tunnelling proxy over a
  websocket connection.
* An SSH connection can be routed through the tunnel using an SSH `ProxyCommand`,
  connecting back into the same session container.
* The connection works using the **Educates CLI** (`educates tunnel connect`).
* The connection works using the **Python reference client** (`tunnel.py`).

In both cases the SSH session reported the workshop user and session pod
hostname, and an `SSH_CONNECTION` value set only inside a real SSH session,
confirming the tunnelling proxy carried the connection.
