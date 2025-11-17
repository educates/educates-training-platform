#!/bin/bash

# Don't run these steps again if we have already generated settings.

if [ -f $HOME/.local/share/workshop/kubernetes-settings.sh ]; then
    exit 0
fi

# Locations of the in cluster Kubernetes configuration files.

TOKEN_FILE="/var/run/secrets/kubernetes.io/serviceaccount/token"
CA_FILE="/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
NAMESPACE_FILE="/var/run/secrets/kubernetes.io/serviceaccount/namespace"

# If we are not running in a Kubernetes cluster, or are connecting to a virtual
# cluster, and a kubeconfig has been provided we need to use that. We can't
# count on the directory where the kubeconfig is located being writable so copy
# it to $HOME/.kube/config. Only do this though if there isn't already an
# existing kubeconfig file. It is expected the kubeconfig file is mounted at
# the location /opt/kubeconfig/config.

if [ -f /opt/kubeconfig/config ]; then
    if [ ! -f $HOME/.kube/config ]; then
        mkdir -p $HOME/.kube
        cp /opt/kubeconfig/config $HOME/.kube/config
    fi
fi

# If we don't have a kubeconfig file or the in cluster Kubernetes configuration
# files we can bail out straight away.

if [ ! -f $HOME/.kube/config -a ! -f $TOKEN_FILE ]; then
    exit 0
fi

# Determine the Kubernetes server version and try to use the matching kubectl
# version. Note that if the client and server version are more than one version
# different, then this will log a warning when run. The point of this though is
# to align the versions so that that warning doesn't happen all the time when
# using kubectl from the command line.

KUBECTL_VERSION=$(kubectl version -o json | jq -re '[.serverVersion.major,.serverVersion.minor]|join(".")' | sed -e 's/^\([1-9]*\.[1-9]*\)\.*$/\1/')

case "$KUBECTL_VERSION" in
1.2*)
    KUBECTL_VERSION=1.31
    ;;
1.3[01])
    KUBECTL_VERSION=1.31
    ;;
1.32)
    KUBECTL_VERSION=1.32
    ;;
1.33)
    KUBECTL_VERSION=1.33
    ;;
1.34)
    KUBECTL_VERSION=1.34
    ;;
1.3*)
    KUBECTL_VERSION=1.34
    ;;
esac

if [ -z "$KUBECTL_VERSION" ]; then
    KUBECTL_VERSION=1.34
fi

# Restrict access permissions on kubeconfig file as some clients will complain
# if it is readable by group or others.

if [ -f $HOME/.kube/config ]; then
    chmod 0600 $HOME/.kube/config
fi

# Determine the server URL and current namespace when using a kubeconfig file.

if [ -f $HOME/.kube/config ]; then
    CURRENT_CONTEXT=$(kubectl config current-context)
    CURRENT_CLUSTER=$(kubectl config view -o jsonpath="{.contexts[?(@.name == '$CURRENT_CONTEXT')]}" | jq -re '.context.cluster')
    CURRENT_NAMESPACE=$(kubectl config view -o jsonpath="{.contexts[?(@.name == '$CURRENT_CONTEXT')]}" | jq -re '.context.namespace')
    KUBERNETES_API_URL=$(kubectl config view -o jsonpath="{.clusters[?(@.name == '$CURRENT_CLUSTER')]}" | jq -re '.cluster.server')
fi

# When using the in cluster Kubernetes config, generate a kubeconfig file so
# that clients which require one will work.

if [ ! -f $HOME/.kube/config ]; then
    KUBERNETES_API_URL="https://$KUBERNETES_PORT_443_TCP_ADDR:$KUBERNETES_PORT_443_TCP_PORT"

    if [ -f $CA_FILE ]; then
        KUBECTL_CA_ARGS="--certificate-authority $CA_FILE"
    else
        KUBECTL_CA_ARGS="--insecure-skip-tls-verify"
    fi

    CURRENT_CLUSTER="educates"

    kubectl config set-cluster $CURRENT_CLUSTER $KUBECTL_CA_ARGS --server "$KUBERNETES_API_URL"

    if [ ! -z "$SESSION_NAMESPACE" ]; then
        CURRENT_NAMESPACE=$SESSION_NAMESPACE
    else
        if [ -f $NAMESPACE_FILE ]; then
            CURRENT_NAMESPACE=$(cat $NAMESPACE_FILE)
        else
            CURRENT_NAMESPACE=default
        fi
    fi

    CURRENT_USER=educates

    if [ ! -z "$KUBERNETES_BEARER_TOKEN" ]; then
        kubectl config set-credentials $CURRENT_USER --token=$KUBERNETES_BEARER_TOKEN
    else
        if [ -f "$TOKEN_FILE" ]; then
            kubectl config set-credentials $CURRENT_USER --token=$(cat $TOKEN_FILE)
        fi
    fi

    CURRENT_CONTEXT=educates

    kubectl config set-context $CURRENT_CONTEXT --cluster $CURRENT_CLUSTER --user $CURRENT_USER --namespace=$CURRENT_NAMESPACE

    kubectl config use-context $CURRENT_CONTEXT
fi

# Determine if there is a namespace corresponding to what was set or calculated
# as the session namespace.

if [ -z "$SESSION_NAMESPACE" ]; then
    SESSION_NAMESPACE=$CURRENT_NAMESPACE
fi

DEFAULT_NAMESPACE=$SESSION_NAMESPACE

set +eo pipefail

kubectl get ns "$DEFAULT_NAMESPACE" >/dev/null 2>&1

if [ "$?" != "0" ]; then
    DEFAULT_NAMESPACE=default
fi

set -eo pipefail

# Save away configuration for later reading in when profiles are processed.

cat >$HOME/.local/share/workshop/kubernetes-settings.sh <<EOF
export KUBECTL_VERSION="$KUBECTL_VERSION"
export KUBERNETES_API_URL="$KUBERNETES_API_URL"
export SESSION_NAMESPACE="$SESSION_NAMESPACE"
export DEFAULT_NAMESPACE="$DEFAULT_NAMESPACE"
EOF
