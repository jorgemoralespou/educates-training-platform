#!/bin/bash
set -e

# Detect architecture
ARCH=$(uname -m)
if [ "$ARCH" == "x86_64" ]; then
  ARCH="amd64"
elif [ "$ARCH" == "aarch64" ]; then
  ARCH="arm64"
fi

# Create local bin directory
mkdir -p ~/.local/bin
export PATH=$PATH:~/.local/bin

# Install Carvel tools
echo "Installing Carvel tools..."
sudo wget -O- https://carvel.dev/install.sh | K14SIO_INSTALL_BIN_DIR=~/.local/bin bash

echo "Installing kind..."
curl -Lo ~/.local/bin/kind https://github.com/kubernetes-sigs/kind/releases/download/v0.30.0/kind-linux-$ARCH
chmod +x ~/.local/bin/kind

# Install k9s
echo "Installing k9s..."
curl -sS https://webinstall.dev/k9s | bash

echo "All additional tools have been installed successfully!" 