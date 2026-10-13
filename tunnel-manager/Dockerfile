# syntax=docker/dockerfile:1

# ---- builder: uv, managed CPython, venv ------------------------------------

FROM debian:trixie-slim AS builder

COPY --from=ghcr.io/astral-sh/uv:0.13 /uv /usr/local/bin/

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

USER 1001

ENV UV_PYTHON_DOWNLOADS=automatic \
    UV_PYTHON_PREFERENCE=only-managed \
    UV_PYTHON_INSTALL_DIR=/opt/app-root/python \
    UV_PROJECT_ENVIRONMENT=/opt/app-root/venv \
    UV_COMPILE_BYTECODE=1

WORKDIR /opt/app-root/src

COPY --chown=1001:0 pyproject.toml uv.lock .python-version ./

# Trim the managed interpreter in the same step that installs it so the
# removed files never land in a layer: the stdlib test suite, the static
# library, IDLE, tkinter, ensurepip and turtledemo are not needed at runtime.
# Only paths under the interpreter prefix; never the venv's site-packages.
RUN uv sync --frozen --no-dev --no-cache && \
    rm -rf /opt/app-root/python/*/lib/python3.*/test \
           /opt/app-root/python/*/lib/python3.*/idlelib \
           /opt/app-root/python/*/lib/python3.*/tkinter \
           /opt/app-root/python/*/lib/python3.*/ensurepip \
           /opt/app-root/python/*/lib/python3.*/turtledemo \
           /opt/app-root/python/*/lib/libpython3.*.a

# ---- final: runtime only --------------------------------------------------

FROM debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root && \
    cp /etc/ssl/certs/ca-certificates.crt /opt/app-root/ca-bundle.crt && \
    chown -R 1001:0 /opt/app-root

# The venv records the interpreter by absolute path, so both trees must land
# at the same locations they were built at.
COPY --from=builder --chown=1001:0 /opt/app-root/python /opt/app-root/python
COPY --from=builder --chown=1001:0 /opt/app-root/venv /opt/app-root/venv

WORKDIR /opt/app-root/src

ENV PYTHONUNBUFFERED=1 \
    PYTHONIOENCODING=UTF-8 \
    LC_ALL=C.UTF-8 \
    LANG=C.UTF-8 \
    PATH=/opt/app-root/bin:/opt/app-root/venv/bin:$PATH

# CA bundle overrides. The file exists from build time (system roots only,
# copied above) so every consumer works with no CA mounted; the start script
# overwrites it with system roots plus the ingress CA when one is mounted.
# Set here, not in the start script, so kubectl exec and overridden commands
# see them.
ENV SSL_CERT_FILE=/opt/app-root/ca-bundle.crt \
    REQUESTS_CA_BUNDLE=/opt/app-root/ca-bundle.crt \
    CURL_CA_BUNDLE=/opt/app-root/ca-bundle.crt \
    GIT_SSL_CAINFO=/opt/app-root/ca-bundle.crt \
    NODE_EXTRA_CA_CERTS=/opt/app-root/ca-bundle.crt

USER 1001

COPY --chown=1001:0 ./ /opt/app-root/src

CMD [ "/opt/app-root/src/start-operator.sh" ]
