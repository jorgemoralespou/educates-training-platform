SSH Remote Access Test
======================

Test workshop for exercising remote SSH access to a workshop session over the
Educates websocket tunnelling proxy. The workshop session container runs the SSH
daemon and the tunnelling proxy, and is also used as the originating point for
the SSH connection, so the session connects back into itself through the tunnel.

Two SSH tunnel clients are exercised:

* The **Educates CLI** (`educates tunnel connect`), downloaded for the session
  architecture from the Educates GitHub releases into `~/bin` by a setup script.
* The **Python reference client** (`tunnel.py`), included under the exercises
  directory.

A Go reference client (`tunnel.go`) is also included under the exercises
directory for reference, but is not exercised by the instructions.

**Enabling SSH access and the tunnel:**

SSH access is enabled by adding `session.applications.sshd` to the workshop
definition with `enabled` set to `true`. The websocket tunnelling proxy, which
allows the SSH connection to be made through an ingress, is enabled by also
setting `tunnel.enabled` to `true`:

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-ssh-remote-access
spec:
  session:
    applications:
      sshd:
        enabled: true
        tunnel:
          enabled: true
```

The SSH private key for the workshop user is available in the session container
at `~/.ssh/id_rsa`, and the SSH connection is routed through the tunnel using an
SSH `ProxyCommand` that bridges the connection over the websocket proxy.
