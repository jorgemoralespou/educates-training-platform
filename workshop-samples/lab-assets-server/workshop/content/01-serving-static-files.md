---
title: Serving Static Files
---

The assets server was prepopulated on startup by downloading the Educates
`develop` branch source archive from GitHub and unpacking it under the
`educates-source` path. Because GitHub source archives contain a single
top-level directory named after the repository and branch, the unpacked source
is located at `educates-source/educates-training-platform-develop`.

Start by browsing what the assets server is hosting. Switch to the **Assets**
dashboard tab to see the directory listing served by the assets server:

```dashboard:open-dashboard
name: Assets
```

You can click through the directories in that listing to explore the unpacked
source. Each path under the listing is served directly as a static file.

Now download an individual static file from the terminal. Fetch the top-level
`README.md` from the unpacked source into the exercises directory:

```terminal:execute
command: |-
  curl -fsS -O {{< param ingress_protocol >}}://{{< param assets_repository >}}/educates-source/educates-training-platform-develop/README.md
```

Confirm the file was downloaded and has content:

```terminal:execute
command: ls -l README.md
```

Open the downloaded file in the editor to confirm it is the Educates project
README served by the assets server:

```editor:open-file
file: ~/exercises/README.md
```

{{< note >}}
The `curl` command uses `-f` so that it fails if the assets server returns an
HTTP error rather than silently saving an error page, making it a reliable
check that the file was served correctly.
{{< /note >}}
