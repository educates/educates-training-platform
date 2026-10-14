#!/bin/bash

# Rebuild the CA bundle when the cluster ingress CA is mounted. The bundle
# path is fixed by SSL_CERT_FILE and the related variables set in the image,
# so here we only replace its contents with the system roots plus the
# ingress CA. Rebuilt on container start only: a rotated CA Secret takes
# effect on the next restart.

INGRESS_CA_DIR=/opt/app-root/certs/ingress-ca

# The CA Secret normally carries the CA under ca.crt. A kubernetes.io/tls
# Secret holding a CA certificate and key, as produced by some tools, has it
# under tls.crt instead, so accept that when ca.crt is absent.

if [ -f "$INGRESS_CA_DIR/ca.crt" ]; then
    INGRESS_CA="$INGRESS_CA_DIR/ca.crt"
elif [ -f "$INGRESS_CA_DIR/tls.crt" ]; then
    INGRESS_CA="$INGRESS_CA_DIR/tls.crt"
else
    INGRESS_CA=""
fi

if [ -n "$INGRESS_CA" ]; then
    cat /etc/ssl/certs/ca-certificates.crt "$INGRESS_CA" > /opt/app-root/ca-bundle.crt
fi

# Start the service.

exec python -m service.main
