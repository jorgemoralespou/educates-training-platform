---
title: Connecting with the Python Client
---

The Python reference client does the same job as the Educates CLI — it acts as
an SSH `ProxyCommand` that bridges the SSH connection over the websocket tunnel.
It is a small standalone script included under the exercises directory. Open it
to see how it works:

```editor:open-file
file: ~/exercises/python-client/tunnel.py
```

It reads the tunnel URL from its argument, opens a websocket to the tunnelling
proxy, and copies bytes between standard input/output and the websocket — which
is exactly what SSH needs from a `ProxyCommand`.

## Install the websocket library

The client depends on the `websockets` Python library. Install it into the
session:

```terminal:execute
command: pip install --user 'websockets>=14'
```

{{< note >}}
Confirming this client works is part of validating the tunnel end to end.
{{< /note >}}

## Connect through the tunnel

The `python-client` host alias configured earlier is identical to the
`educates-cli` alias except that its `ProxyCommand` runs `tunnel.py`. Connect
back into this session through the tunnel using it:

```terminal:execute
command: ssh python-client 'echo "Connected over the tunnel as $(whoami) on $(uname -n)"; echo "SSH_CONNECTION=$SSH_CONNECTION"'
```

As before, seeing the workshop user, the session pod hostname, and an
`SSH_CONNECTION` value confirms the SSH session was established through the
websocket tunnelling proxy — this time using the Python client.
