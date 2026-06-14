---
title: Workshop Overview
---

This workshop exercises remote SSH access to a workshop session over the
Educates websocket **tunnelling proxy**.

When SSH access and the tunnel are enabled for a workshop, the session container
runs an SSH daemon, and a tunnelling proxy makes that daemon reachable through
the session ingress over a websocket connection. An SSH client connects by using
a `ProxyCommand` that bridges the SSH connection over the websocket.

In this workshop the session container plays both roles: it runs the SSH daemon
and the tunnel, and is also where you originate the SSH connection from — so you
connect back into the same container through the tunnel. This makes it a
self-contained way to confirm the tunnelling proxy is working.

You will make the connection using two different tunnel clients:

* The **Educates CLI** — `educates tunnel connect`, downloaded into `~/bin` at
  session start by a setup script.
* The **Python reference client** — `tunnel.py`, included under the exercises
  directory.

The SSH private key for the workshop user is already present in the session
container at `~/.ssh/id_rsa`, so no key needs to be downloaded — the focus is
purely on connecting through the tunnel.
