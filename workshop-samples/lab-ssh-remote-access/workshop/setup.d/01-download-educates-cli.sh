#!/bin/bash

# Download the Educates CLI for the session architecture into ~/bin (which is on
# the workshop session PATH) so the workshop can use "educates tunnel connect"
# as the SSH ProxyCommand. The CLI is published as a per-architecture binary on
# the Educates GitHub releases.

set -eo pipefail

VERSION=3.7.1

case "$(uname -m)" in
    x86_64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *)
        echo "Unsupported architecture: $(uname -m)" >&2
        exit 1
        ;;
esac

mkdir -p "$HOME/bin"

curl --silent --fail --location \
    -o "$HOME/bin/educates" \
    "https://github.com/educates/educates-training-platform/releases/download/${VERSION}/educates-linux-${ARCH}"

chmod +x "$HOME/bin/educates"
