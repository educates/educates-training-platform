# Customize the configuration for the web console for accessing the Kubernetes
# cluster, disabling the web console if we are not running in a Kubernetes
# cluster or a kubeconfig has not been mounted into the container.
#
# TODO: Some of this should be moved into the gateway application now as no
# longer need these details in the workshop renderer.

ENABLE_CONSOLE_KUBERNETES=false

export ENABLE_CONSOLE_KUBERNETES

if [ x"$ENABLE_CONSOLE" != x"true" -o ! -f $HOME/.kube/config ]; then
    ENABLE_CONSOLE=false
    return
fi

# The Kubernetes dashboard is the only console which is available. The vendor
# property is still accepted so existing workshop definitions remain valid, but
# any value other than "kubernetes" is ignored and the Kubernetes dashboard is
# used in its place.

CONSOLE_VENDOR=$(workshop-definition -r '(.spec.session.applications.console.vendor // "kubernetes")')

if [ x"$CONSOLE_VENDOR" != x"kubernetes" ]; then
    echo "WARNING: Console vendor \"$CONSOLE_VENDOR\" is not supported, using \"kubernetes\"." 1>&2
    CONSOLE_VENDOR=kubernetes
fi

ENABLE_CONSOLE_KUBERNETES=true

if [ x"$DEFAULT_NAMESPACE" != x"" ]; then
    CONSOLE_URL="$INGRESS_PROTOCOL://console-$SESSION_NAMESPACE.$INGRESS_DOMAIN$INGRESS_PORT_SUFFIX/#/overview?namespace=$DEFAULT_NAMESPACE"
else
    CONSOLE_URL="$INGRESS_PROTOCOL://console-$SESSION_NAMESPACE.$INGRESS_DOMAIN$INGRESS_PORT_SUFFIX/"
fi

CONSOLE_PORT=10083

export CONSOLE_URL
export CONSOLE_PORT
export CONSOLE_VENDOR
