#!/bin/bash

# Rebuild the CA bundle when the cluster ingress CA is mounted. The bundle
# path is fixed by SSL_CERT_FILE and the related variables set in the image,
# so here we only replace its contents with the system roots plus the
# ingress CA. Rebuilt on container start only: a rotated CA Secret takes
# effect on the next restart.

INGRESS_CA=/opt/app-root/certs/ingress-ca/ca.crt

if [ -f "$INGRESS_CA" ]; then
    cat /etc/ssl/certs/ca-certificates.crt "$INGRESS_CA" > /opt/app-root/ca-bundle.crt
fi

exec python main.py
