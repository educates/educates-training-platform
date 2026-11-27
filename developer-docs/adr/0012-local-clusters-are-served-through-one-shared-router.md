Local clusters are served through one shared router on one host address
=======================================================================

Superseded in part by
[0013 - Local cluster API servers are reached through the router by TLS server name](0013-local-cluster-api-servers-are-reached-through-the-router-by-tls-server-name.md):
the router also publishes port 6443 on the router address, for the API
servers of the local clusters.

Every local cluster is reached through one router, the container
``educates-router`` on the ``kind`` Docker network, which the CLI creates with
the first local cluster and removes with the last. The router publishes ports
80 and 443 once, on the router address, and forwards each connection to the
control-plane node of the cluster that owns the name it asks for: HTTPS by the
server name in the TLS handshake, passed through unchanged, so that each
cluster's ingress still presents its own certificate, and plain HTTP by the
``Host`` header. kind nodes publish nothing on the host.

Each cluster owns its ingress domain, declared or derived, and its
``resolver.extraDomains``. A domain matches itself and every name under it,
and the most specific domain wins, as in the resolver. Clusters have no host
addresses of their own: every cluster's names resolve to the router address,
which ``router.yaml`` in the data home holds, and which must be a specific,
non-loopback address of a host interface. A cluster without a domain is served
at the ``nip.io`` name of the router address, ``<address with dashes>.nip.io``
for the default local cluster and ``<name>.<address with dashes>.nip.io`` for
a named one.

A cluster may set ``ingress.externalTLSTermination``, as the cloud scenario
kinds do. It then issues no certificate of its own and serves plain HTTP while
its URLs use ``https``, and the router ends TLS for its names with a
certificate from the local CA, as a cloud load balancer would in front of it.

The router runs HAProxy from a long-term support branch. The CLI renders its
configuration from every cluster's config into a folder of the data home, and
applies a change with a reload, which leaves open connections, such as a
session's terminals, on the previous worker.

The router address, ``router.yaml`` and the removal of
``cluster.listenAddress`` are on every user's machine, and kind fixes the
ports a node publishes when it creates the cluster, so reversing this would
mean migrating every data home and re-creating every cluster again.

Considered options
------------------

Publishing each cluster's ports 80 and 443 on an address of its own, as
[0009](0009-local-clusters-share-one-data-home-and-host-services.md) decided,
stopped working with Docker Desktop. macOS lets a process that is not root
bind a port below 1024 only on the wildcard address, and Docker Desktop has
had no privileged helper since 4.98. From 4.100.0 it binds such a published
port on the wildcard address whatever address the publish names, and filters
the connections it accepts by their destination. A second cluster's publish of
port 80 or 443 then fails because the port is in use, so only one local
cluster can run at a time. The runtime builds every URL on port 80 or 443, so
clusters cannot be told apart by port either. One publish of each port is what
Docker Desktop serves, so the clusters share it and are told apart by name.

Publishing the router on every host address, ``0.0.0.0``, would have let each
cluster keep an address of its own for the resolver to answer with. The
addresses would then tell nothing apart, and every training portal and session
terminal would be reachable from other machines on the network by naming its
host.

Having each node publish unprivileged host ports for the router to reach
through the host was rejected. It needs a port allocated and recorded for each
cluster, and passes every connection through Docker's port forwarding twice,
while a router on the ``kind`` network reaches every node directly by its
container name.

A process on the host, such as the CLI run as a launchd agent, can bind
``*:80`` and ``*:443`` on macOS without root and route each connection by the
local address it arrived on. That would keep an address per cluster and need
no Docker setting. It was rejected because it works only on macOS, where Linux
needs root for those ports, because the CLI would have to install, supervise
and upgrade a long-running daemon, and because it listens on every interface.

``cloud-provider-kind`` gives each ``LoadBalancer`` Service a load balancer
container and an address of its own. On macOS it reaches ports 80 and 443 only
through a tunnel that runs as root, and its addresses follow each Service. It
provides load balancers inside a cluster, not one entry point that routes
several clusters by name.

Ending TLS at the router for every cluster was rejected. The router would hold
every cluster's keys, and the clusters would stop serving the certificates
their cert-manager issues from each domain's intermediate CA, as
[0005](0005-local-ca-roots-stay-on-the-laptop.md) describes. TLS passes
through unless a cluster asks for the router to end it.

For the router itself, agentgateway honours the lifetime of the answers from
Docker's embedded DNS, 600 seconds, so a cluster restarted with a new address
could stay unreachable for ten minutes, and it sets no ``X-Forwarded-Proto``.
Envoy meets every need, at the cost of several configuration files, defaults
that cut long-lived websockets, and reloads that depend on watching files.
nginx needs more of its defaults raised and states no support window, and
Traefik is the largest image with the shortest support. HAProxy resolves the
cluster nodes' names every second whatever the answer's lifetime, reloads on a
signal, keeps five years of support on its long-term branches, and has kept its
configuration format stable for decades.

Consequences
------------

A cluster needs no host address and no alias of its own: a named cluster is
created with its name, and optionally a domain. One address serves every
cluster, and an alias such as ``10.10.10.1`` is the durable choice, since
``nip.io`` names contain the address. Moving the router to another address
re-creates only the router. Clusters whose domain the resolver serves are
unaffected, while a cluster on a ``nip.io`` domain gets a new domain and needs
deploying again. On Docker Desktop 4.97 and later, the router's address still
needs "Allow privileged port forwarding to external machines".

Two clusters claiming one domain stays an error, and is now the only clash
between clusters. Every cluster passes through the router, a lone default
cluster included. A cluster created before has its ports published by kind on
its node, and cannot run beside the router: creating the router fails while
anything holds its ports, naming such a cluster, and the cluster is created
again. ``cluster.listenAddress`` and the ``--listen-address`` option are
removed, and a config in the data home that still has the field is moved once
into ``router.yaml``, with the previous config kept as a backup beside it.

The runtime is unchanged: URLs stay on ports 80 and 443, and Envoy keeps its
host ports on each node. Envoy sees the router as every client, as it already
saw Docker's port forwarding. The operator gains a CA trust Secret beside the
``None`` certificates provider, so that a cluster whose TLS ends outside it
trusts the CA of the certificate presented there. For a cluster whose TLS
ends at the router, the CLI gives it the local root. With TLS passed through,
the router can only refuse a connection for a stopped cluster's name, while
plain HTTP gets a response naming the cluster. ``educates cluster workshop
serve`` listens on the router address, on a port of its own for each run.
Docker workshops keep their own published port, outside the router.

Rendering the router's configuration from every cluster's config is a rule
across clusters, so it lives in ``client-programs/pkg/localclusters``, beside
the resolver's rendering, and ``educates local router status`` and ``update``
show and apply it.
