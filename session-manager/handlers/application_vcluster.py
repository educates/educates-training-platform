import os

import yaml

from .helpers import xget, parse_image_reference

from .operator_config import (
    OPERATOR_API_GROUP,
    LOFTSH_VCLUSTER_IMAGE,
    LOFTSH_KUBERNETES_V1_31_IMAGE,
    LOFTSH_KUBERNETES_V1_32_IMAGE,
    LOFTSH_KUBERNETES_V1_33_IMAGE,
    LOFTSH_KUBERNETES_V1_34_IMAGE,
    CLUSTER_STORAGE_GROUP,
)

K8S_DEFAULT_VERSION = "1.33"

K8S_VERSIONS = {
    "1.31": LOFTSH_KUBERNETES_V1_31_IMAGE,
    "1.32": LOFTSH_KUBERNETES_V1_32_IMAGE,
    "1.33": LOFTSH_KUBERNETES_V1_33_IMAGE,
    "1.34": LOFTSH_KUBERNETES_V1_34_IMAGE,
}

# Scenarios
# - Expose k8s api_server in vcluster via a TLS ingress on host.
# - Enable internal vcluster to use it's own ingresses, so deploy an internal ingress controller.  


def vcluster_workshop_spec_patches(workshop_spec, application_properties):
    policy = xget(workshop_spec, "session.namespaces.security.policy", "baseline")

    return {
        "spec": {
            "session": {
                "namespaces": {
                    "security": {"policy": policy, "token": {"enabled": False}}
                },
                # TODO: Remove octant console
                "applications": {"console": {"octant": {"version": "latest"}}},
                "variables": [
                    {
                        "name": "vcluster_secret",
                        "value": "$(session_namespace)-vc-kubeconfig",
                    },
                    {
                        "name": "vcluster_namespace",
                        "value": "$(session_namespace)-vc",
                    },
                ],
            }
        }
    }


def vcluster_environment_objects_list(workshop_spec, application_properties):
    return []


def vcluster_session_objects_list(workshop_spec, application_properties):
    def relpath(*paths):
        return os.path.join(os.path.dirname(__file__), *paths)

    syncer_memory = xget(application_properties, "resources.syncer.memory", "1Gi")
    syncer_storage = xget(application_properties, "resources.syncer.storage", "5Gi")

    k8s_version = xget(application_properties, "version", K8S_DEFAULT_VERSION)

    if k8s_version not in K8S_VERSIONS:
        k8s_version = K8S_DEFAULT_VERSION

    k8s_image = K8S_VERSIONS.get(k8s_version)

    ingress_enabled = xget(application_properties, "ingress.enabled", False)

    ingress_subdomains = xget(application_properties, "ingress.subdomains", [])
    ingress_subdomains = sorted(ingress_subdomains + ["default"])

    map_services_from_virtual = xget(application_properties, "services.fromVirtual", [])
    map_services_from_host = xget(application_properties, "services.fromHost", [])

    # TODO: We need to see whether we need to sync ingress_classes and storage_classes
    if ingress_enabled:
        sync_ingress_resources = False
    else:
        sync_ingress_resources = True

    vcluster_objects = xget(application_properties, "objects", [])

    # If ingress controller is enabled for vcluster, add Contour objects

    if ingress_enabled:
        # We need to read the Contour resources objects from files stored in
        # the "../packages/contour/upstream" directory relative to this source
        # file, and add them to the vcluster objects list. The files are:
        #
        #   00-common.yaml
        #   01-contour-config.yaml
        #   01-crds.yaml
        #   02-job-certgen.yaml
        #   02-role-contour.yaml
        #   02-rbac.yaml
        #   02-service-contour.yaml
        #   03-contour.yaml
        #   03-envoy.yaml
        #
        # We ignore "02-service-envoy.yaml" as we need to replace it with a
        # version which exposes the service as a ClusterIP instead of a
        # LoadBalancer.

        contour_objects = []

        with open(
            relpath("../packages/contour/upstream/00-common.yaml"), encoding="utf-8"
        ) as f:
            contour_objects.extend(yaml.safe_load_all(f))

        with open(
            relpath("../packages/contour/upstream/01-contour-config.yaml"),
            encoding="utf-8",
        ) as f:
            contour_objects.extend(yaml.safe_load_all(f))

        with open(
            relpath("../packages/contour/upstream/01-crds.yaml"), encoding="utf-8"
        ) as f:
            contour_objects.extend(yaml.safe_load_all(f))

        with open(
            relpath("../packages/contour/upstream/02-job-certgen.yaml"),
            encoding="utf-8",
        ) as f:
            contour_objects.extend(yaml.safe_load_all(f))

        with open(
            relpath("../packages/contour/upstream/02-role-contour.yaml"), encoding="utf-8"
        ) as f:
            contour_objects.extend(yaml.safe_load_all(f))

        with open(
            relpath("../packages/contour/upstream/02-rbac.yaml"), encoding="utf-8"
        ) as f:
            contour_objects.extend(yaml.safe_load_all(f))

        with open(
            relpath("../packages/contour/upstream/02-service-contour.yaml"),
            encoding="utf-8",
        ) as f:
            contour_objects.extend(yaml.safe_load_all(f))

        with open(
            relpath("../packages/contour/upstream/03-contour.yaml"), encoding="utf-8"
        ) as f:
            for obj in yaml.safe_load_all(f):
                if obj.get("kind") == "Deployment":
                    obj["spec"]["replicas"] = 1

                contour_objects.append(obj)

        with open(
            relpath("../packages/contour/upstream/03-envoy.yaml"), encoding="utf-8"
        ) as f:
            # For the case of the envoy DaemonSet, we need to remove the
            # hostPort properties from the container port definitions, as
            # we do not allow hostPort and do not need it since we will proxy
            # to the envoy service as a ClusterIP.

            for obj in yaml.safe_load_all(f):
                if obj.get("kind") == "DaemonSet":
                    for container in obj["spec"]["template"]["spec"]["containers"]:
                        for port in container.get("ports", []):
                            port.pop("hostPort", None)

                contour_objects.append(obj)

        vcluster_objects.extend(contour_objects)

        # Add the Contour service with a ClusterIP instead of a LoadBalancer

        contour_service = {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "name": "envoy",
                "namespace": "projectcontour",
            },
            "spec": {
                "type": "ClusterIP",
                "ports": [
                    {
                        "name": "http",
                        "port": 80,
                        "protocol": "TCP",
                        "targetPort": 8080,
                    },
                    {
                        "name": "https",
                        "port": 443,
                        "protocol": "TCP",
                        "targetPort": 8443,
                    },
                ],
                "selector": {
                    "app": "envoy",
                },
            },
        }

        vcluster_objects.append(contour_service)

        # Now need to tell vcluster to map the envoy service from the internal
        # projectcontour namespace to the external namespace for the sessions
        # virtual cluster.

        map_services_from_virtual.append(
            {
                "from": "projectcontour/envoy",
                "to": "my-vcluster-envoy",
            }
        )

    # Load vcluster.yaml configuration - Load reference config verbatim and customize for Educates
    with open(
        relpath("../packages/vcluster/vcluster-all-config.yaml"), encoding="utf-8"
    ) as f:
        vcluster_config = yaml.safe_load(f)

    #
    # CONFIGURATION CUSTOMIZATION
    #
    # Things we WANT this cluster to have:
    # - sync service accounts of the vcluster to the host cluster
    # - sync ingresses of the vcluster to the host cluster when dedicated vcluster ingress is enabled
    # - sync storage classes of the host cluster to the vcluster so they can be used by the vcluster
    # - sync ingress classes of the host cluster to the vcluster so they can be used by the vcluster
    # - replicate services to/from the host to/from the vcluster if they are configured in the workshop
    # - expose kubeconfig of the vcluster to the host cluster and save it into a secret for use in the workshop session
    #
    # Things we DO NOT WANT this cluster to have:
    # - Expose the api server of the vcluster as ingress on the host cluster
    # - We don't want policies to be controlled by the virtual cluster as we do use the workshop policies
    #
    #
    # Things that are configured directly via kubernetes resources, and hence no need to tweak in configuration
    # - rbac. This is why the rbac don't need to be enabled/disabled explicitly
    # - distro. We use kubernetes distro, but it's directly configured in the statefulset
    #

    # Disable rbac as it's already been precreated by Educates. This is also done directly as resources
    # vcluster_config["rbac"]["role"]["enabled"] = False
    # vcluster_config["rbac"]["clusterRole"]["enabled"] = False
    # vcluster_config["rbac"]["enableVolumeSnapshotRules"]["enabled"] = False

    # Enable specific k8s distro. This is done directly in the statefulset
    #vcluster_config["controlPlane"]["distro"]["k8s"]["enabled"] = True
    #vcluster_config["controlPlane"]["distro"]["k8s"]["image"]["tag"] = parse_image_reference(k8s_image)["tag"]

    # Sync mapping of resources
    # TO_HOST_SYNC
    #vcluster_config["sync"]["toHost"]["services"] = {"enabled": True}
    #vcluster_config["sync"]["toHost"]["endpoints"] = {"enabled": True}
    #vcluster_config["sync"]["toHost"]["endpointSlices"] = {"enabled": True}
    #vcluster_config["sync"]["toHost"]["persistentVolumeClaims"] = {"enabled": True}
    #vcluster_config["sync"]["toHost"]["configMaps"] = {"enabled": True}
    #vcluster_config["sync"]["toHost"]["secrets"] = {"enabled": True}
    vcluster_config["sync"]["toHost"]["serviceAccounts"] = {"enabled": True}
    vcluster_config["sync"]["toHost"]["ingresses"] = {"enabled": sync_ingress_resources}
    # FROM_HOST_SYNC    
    vcluster_config["sync"]["fromHost"]["storageClasses"] = {"enabled": True}
    vcluster_config["sync"]["fromHost"]["ingressClasses"] = {"enabled": True}

    # Map services from virtual cluster to host cluster
    vcluster_config["networking"]["replicateServices"]["toHost"] = map_services_from_virtual
    vcluster_config["networking"]["replicateServices"]["fromHost"] = map_services_from_host

    # TODO: Fix the generated Kubeconfig, as it's using localhost:8443 as server instead of the one specified here
    vcluster_config["exportKubeConfig"] = {
        "context": "my-vcluster",
        "server": "https://my-vcluster.$(vcluster_namespace)",
        "additionalSecrets": [
            {
                "name": "$(vcluster_namespace)-kubeconfig",
                "server": "https://my-vcluster.$(vcluster_namespace)"
            }
        ],
    }
    vcluster_config["policies"]["resourceQuota"]["enabled"] = False
    vcluster_config["policies"]["limitRange"]["enabled"] = False

    # vcluster_config["controlPlane"]["ingress"]["enabled"] = True
    # vcluster_config["controlPlane"]["ingress"]["host"] = f"$(session_namespace)-vc-api.{INGRESS_DOMAIN}"
    # vcluster_config["controlPlane"]["ingress"]["spec"]["tls"] = ingress_tls
    # Add extra SANs to the proxy
    vcluster_config["controlPlane"]["proxy"]["extraSANs"] = ["my-vcluster.$(vcluster_namespace)-vc", "my-vcluster.$(vcluster_namespace)-vc.svc.cluster.local"]

    # TODO: Work integration with cert-manager

    vcluster_config["experimental"]["deploy"]["vcluster"]["manifests"] = yaml.dump_all(vcluster_objects, Dumper=yaml.Dumper)

    # Definition of vcluster objects:
    # - Namespace for vcluster
    # - SecretCopier to copy the vcluster kubeconfig to the workshop namespace
    # - ServiceAccount for vcluster creation
    # - ServiceAccount for workloads in vcluster
    # - Secret with vcluster configuration
    # - ClusterRole, Role, RoleBinding and ClusterRoleBinding for vcluster creation
    # - vcluster and vcluster-headless services to access vCluster from the host cluster
    # - vCluster statefulset to create the vCluster
    objects = [
        {
            "apiVersion": "v1",
            "kind": "Namespace",
            "metadata": {
                "name": "$(session_namespace)-vc",
                "annotations": {
                    "secretgen.carvel.dev/excluded-from-wildcard-matching": "",
                    f"training.{OPERATOR_API_GROUP}/session.role": "custom",
                    f"training.{OPERATOR_API_GROUP}/session.budget": "custom",
                    f"training.{OPERATOR_API_GROUP}/session.policy": "baseline",
                },
            },
        },
        {
            "apiVersion": f"secrets.{OPERATOR_API_GROUP}/v1beta1",
            "kind": "SecretCopier",
            "metadata": {"name": "$(session_namespace)-vc-kubeconfig"},
            "spec": {
                "rules": [
                    {
                        "sourceSecret": {
                            "name": "$(session_namespace)-vc-kubeconfig",
                            "namespace": "$(session_namespace)-vc",
                        },
                        "targetNamespaces": {
                            "nameSelector": {"matchNames": ["$(workshop_namespace)"]}
                        },
                        "targetSecret": {"name": "$(vcluster_secret)"},
                        "reclaimPolicy": "Delete",
                    }
                ]
            },
        },
        {
            "apiVersion": "v1",
            "kind": "ServiceAccount",
            "metadata": {
                "name": "vc-my-vcluster",
                "namespace": "$(session_namespace)-vc",
            },
            # TODO: Add ImagePullSecrets capability
        },
        {
            "apiVersion": "v1",
            "kind": "ServiceAccount",
            "metadata": {
                "name": "vc-workload-my-vcluster",
                "namespace": "$(session_namespace)",
            },
            # TODO: Add ImagePullSecrets capability
        },
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {
                "name": "vc-config-my-vcluster",
                "namespace": "$(session_namespace)-vc",
            },
            "stringData": {
                "config.yaml": yaml.dump(vcluster_config),
            },
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRole",
            "metadata": {
                "name": "my-vcluster-$(session_namespace)-vc",
            },
            "rules": [
                {
                    "apiGroups": ["networking.k8s.io"],
                    "resources": ["ingressclasses"],
                    "verbs": ["get", "list", "watch"],
                },
                {
                    "apiGroups": ["storage.k8s.io"],
                    "resources": ["storageclasses"],
                    "verbs": ["get", "list", "watch"],
                },
                {
                    "apiGroups": [""],
                    "resources": ["services", "endpoints"],
                    "verbs": ["get", "list", "watch"],
                },
            ],
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "Role",
            "metadata": {
                "name": "my-vcluster",
                "namespace": "$(session_namespace)-vc",
            },
            "rules": [
                {
                    "apiGroups": [""],
                    "resources": [
                        "configmaps",
                        "secrets",
                        "services",
                        "serviceaccounts",
                        "pods",
                        "pods/attach",
                        "pods/portforward",
                        "pods/exec",
                        "pods/status",
                        "endpoints",
                        "persistentvolumeclaims",
                    ],
                    "verbs": [
                        "create",
                        "delete",
                        "patch",
                        "update",
                        "get",
                        "list",
                        "watch",
                    ],
                },
                {
                    "apiGroups": [""],
                    "resources": ["events", "pods/log"],
                    "verbs": ["get", "list", "watch"],
                },
                {
                    "apiGroups": ["discovery.k8s.io"],
                    "resources": ["endpointslices"],
                    "verbs": ["get", "list", "watch", "update"],
                },
                {
                    "apiGroups": ["networking.k8s.io"],
                    "resources": ["ingresses"],
                    "verbs": [
                        "create",
                        "delete",
                        "patch",
                        "update",
                        "get",
                        "list",
                        "watch",
                    ],
                },
                {
                    "apiGroups": ["apps"],
                    "resources": ["statefulsets", "replicasets", "deployments"],
                    "verbs": ["get", "list", "watch"],
                },
            ],
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "my-vcluster",
                "namespace": "$(session_namespace)-vc",
            },
            "subjects": [
                {
                    "kind": "ServiceAccount",
                    "name": "vc-my-vcluster",
                    "namespace": "$(session_namespace)-vc",
                }
            ],
            "roleRef": {
                "kind": "Role",
                "name": "my-vcluster",
                "apiGroup": "rbac.authorization.k8s.io",
            },
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "Role",
            "metadata": {
                "name": "my-vcluster",
                "namespace": "$(session_namespace)",
            },
            "rules": [
                {
                    "apiGroups": [""],
                    "resources": [
                        "configmaps",
                        "secrets",
                        "services",
                        "pods",
                        "pods/attach",
                        "pods/portforward",
                        "pods/exec",
                        "endpoints",
                        "persistentvolumeclaims",
                    ],
                    "verbs": [
                        "create",
                        "delete",
                        "patch",
                        "update",
                        "get",
                        "list",
                        "watch",
                    ],
                },
                {
                    "apiGroups": [""],
                    "resources": ["events", "pods/log"],
                    "verbs": ["get", "list", "watch"],
                },
                {
                    "apiGroups": ["networking.k8s.io"],
                    "resources": ["ingresses"],
                    "verbs": [
                        "create",
                        "delete",
                        "patch",
                        "update",
                        "get",
                        "list",
                        "watch",
                    ],
                },
                {
                    "apiGroups": ["apps"],
                    "resources": ["statefulsets", "replicasets", "deployments"],
                    "verbs": ["get", "list", "watch"],
                },
            ],
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "my-vcluster",
                "namespace": "$(session_namespace)",
            },
            "subjects": [
                {
                    "kind": "ServiceAccount",
                    "name": "vc-my-vcluster",
                    "namespace": "$(session_namespace)-vc",
                }
            ],
            "roleRef": {
                "kind": "Role",
                "name": "my-vcluster",
                "apiGroup": "rbac.authorization.k8s.io",
            },
        },
        {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRoleBinding",
            "metadata": {
                "name": "my-vcluster-$(session_namespace)-vc",
            },
            "subjects": [
                {
                    "kind": "ServiceAccount",
                    "name": "vc-my-vcluster",
                    "namespace": "$(session_namespace)-vc",
                }
            ],
            "roleRef": {
                "kind": "ClusterRole",
                "name": "my-vcluster-$(session_namespace)-vc",
                "apiGroup": "rbac.authorization.k8s.io",
            },
        },
        {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "name": "my-vcluster",
                "namespace": "$(session_namespace)-vc",
            },
            "spec": {
                "type": "ClusterIP",
                "ports": [
                    {
                        "name": "https",
                        "port": 443,
                        "targetPort": 8443,
                        "protocol": "TCP",
                    },
                    {
                        "name": "kubelet",
                        "port": 10250,
                        "targetPort": 8443,
                        "protocol": "TCP",
                    }
                ],
                "selector": {"app": "vcluster", "release": "my-vcluster"},
            },
        },
        {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "name": "my-vcluster-headless",
                "namespace": "$(session_namespace)-vc",
            },
            "spec": {
                "ports": [
                    {
                        "name": "https",
                        "port": 443,
                        "targetPort": 8443,
                        "protocol": "TCP",
                    }
                ],
                "clusterIP": "None",
                "selector": {"app": "vcluster", "release": "my-vcluster"},
            },
        },
        {
            "apiVersion": "apps/v1",
            "kind": "StatefulSet",
            "metadata": {
                "name": "my-vcluster",
                "namespace": "$(session_namespace)-vc",
            },
            "spec": {
                "serviceName": "my-vcluster-headless",
                "replicas": 1,
                "selector": {
                    "matchLabels": {"app": "vcluster", "release": "my-vcluster"}
                },
                "volumeClaimTemplates": [
                    {
                        "metadata": {"name": "data"},
                        "spec": {
                            "accessModes": ["ReadWriteOnce"],
                            "storageClassName": None,
                            "resources": {"requests": {"storage": syncer_storage}},
                        },
                    }
                ],
                "template": {
                    "metadata": {
                        "labels": {"app": "vcluster", "release": "my-vcluster"}
                    },
                    "spec": {
                        "terminationGracePeriodSeconds": 10,
                        "nodeSelector": {},
                        "affinity": {},
                        "tolerations": [],
                        "serviceAccountName": "vc-my-vcluster",
                        "volumes": [
                            {"name": "helm-cache", "emptyDir": {}},
                            {"name": "binaries", "emptyDir": {}},
                            {"name": "tmp", "emptyDir": {}},
                            {"name": "certs", "emptyDir": {}},
                            {
                                "name": "vcluster-config",
                                "secret": {"secretName": "vc-config-my-vcluster"},
                            },
                        ],
                        "securityContext": {
                            "fsGroup": CLUSTER_STORAGE_GROUP,
                            "supplementalGroups": [CLUSTER_STORAGE_GROUP],
                        },
                        "initContainers": [
                            {
                                "name": "kubernetes",
                                "image": k8s_image,
                                "command": ["cp"],
                                "args": ["-r", "/kubernetes/.", "/binaries/"],
                                "volumeMounts": [
                                    {"mountPath": "/binaries", "name": "binaries"}
                                ],
                                "securityContext": {},
                                "resources": {
                                    "limits": {"cpu": "100m", "memory": "256Mi"},
                                    "requests": {"cpu": "40m", "memory": "64Mi"},
                                },
                            }
                        ],
                        "containers": [
                            {
                                "name": "syncer",
                                "image": LOFTSH_VCLUSTER_IMAGE,
                                "livenessProbe": {
                                    "httpGet": {
                                        "path": "/healthz",
                                        "port": 8443,
                                        "scheme": "HTTPS",
                                    },
                                    "initialDelaySeconds": 60,
                                    "periodSeconds": 2,
                                    "timeoutSeconds": 3,
                                    "failureThreshold": 60,
                                },
                                "readinessProbe": {
                                    "httpGet": {
                                        "path": "/readyz",
                                        "port": 8443,
                                        "scheme": "HTTPS",
                                    },
                                    "periodSeconds": 2,
                                    "timeoutSeconds": 3,
                                    "failureThreshold": 60,
                                },
                                "startupProbe": {
                                    "httpGet": {
                                        "path": "/readyz",
                                        "port": 8443,
                                        "scheme": "HTTPS",
                                    },
                                    "periodSeconds": 6,
                                    "timeoutSeconds": 3,
                                    "failureThreshold": 300,
                                },
                                "securityContext": {
                                    "allowPrivilegeEscalation": False,
                                    "runAsNonRoot": True,
                                    "runAsGroup": 1001,
                                    "runAsUser": 12345,
                                },
                                "resources": {
                                    "limits": {
                                        "ephemeral-storage": syncer_storage,
                                        "memory": syncer_memory,
                                    },
                                    "requests": {
                                        "cpu": "200m",
                                        "ephemeral-storage": syncer_storage,
                                        "memory": syncer_memory,
                                    },
                                },
                                "env": [
                                    {"name": "VCLUSTER_NAME", "value": "my-vcluster"},
                                    {
                                        "name": "POD_NAME",
                                        "valueFrom": {
                                            "fieldRef": {"fieldPath": "metadata.name"}
                                        },
                                    },
                                    {
                                        "name": "POD_IP",
                                        "valueFrom": {
                                            "fieldRef": {"fieldPath": "status.podIP"}
                                        },
                                    },
                                    {
                                        "name": "NODE_NAME",
                                        "valueFrom": {
                                            "fieldRef": {"fieldPath": "spec.nodeName"}
                                        },
                                    },
                                    {
                                        "name": "NODE_IP",
                                        "valueFrom": {
                                            "fieldRef": {"fieldPath": "status.hostIP"}
                                        },
                                    },
                                ],
                                "volumeMounts": [
                                    {"name": "data", "mountPath": "/data"},
                                    {"name": "binaries", "mountPath": "/binaries"},
                                    {"name": "certs", "mountPath": "/pki"},
                                    {"name": "helm-cache", "mountPath": "/.cache/helm"},
                                    {"name": "vcluster-config", "mountPath": "/var/lib/vcluster"},
                                    {"name": "tmp", "mountPath": "/tmp"},
                                ],
                            },
                        ],
                    },
                },
            },
        },
    ]

    if ingress_enabled:
        ingress_body = {
            "apiVersion": "networking.k8s.io/v1",
            "kind": "Ingress",
            "metadata": {
                "name": "contour-$(session_namespace)",
                "namespace": "$(session_namespace)-vc",
                "annotations": {
                    "nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
                    "nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
                    "projectcontour.io/websocket-routes": "/",
                    "projectcontour.io/response-timeout": "3600s",
                },
            },
            "spec": {
                "rules": [
                    {
                        "host": "*.$(session_namespace).$(ingress_domain)",
                        "http": {
                            "paths": [
                                {
                                    "path": "/",
                                    "pathType": "Prefix",
                                    "backend": {
                                        "service": {
                                            "name": "my-vcluster-envoy",
                                            "port": {"number": 80},
                                        }
                                    },
                                }
                            ]
                        },
                    }
                ]
            },
        }

        for subdomain in filter(len, ingress_subdomains):
            ingress_body["spec"]["rules"].append(
                {
                    "host": f"*.{subdomain}.$(session_namespace).$(ingress_domain)",
                    "http": {
                        "paths": [
                            {
                                "path": "/",
                                "pathType": "Prefix",
                                "backend": {
                                    "service": {
                                        "name": "my-vcluster-envoy",
                                        "port": {"number": 80},
                                    }
                                },
                            }
                        ]
                    },
                }
            )

        objects.append(ingress_body)

    return objects


def vcluster_pod_template_spec_patches(workshop_spec, application_properties):
    return {
        "containers": [
            {
                "name": "workshop",
                "volumeMounts": [
                    {"name": "kubeconfig", "mountPath": "/opt/kubeconfig"}
                ],
            }
        ],
        "volumes": [
            {
                "name": "kubeconfig",
                "secret": {"secretName": "$(vcluster_secret)"},
            }
        ],
    }
