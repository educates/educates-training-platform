Local cluster API servers are reached through the router by TLS server name
===========================================================================

The router of
[0012](0012-local-clusters-are-served-through-one-shared-router.md) also
publishes port 6443 on the router address, and sends each connection on it to
the API server of the local cluster whose control-plane node container the
server name in the TLS handshake names, such as ``educates-control-plane`` or
``educates-<name>-control-plane``, passed through untouched to the node's port
6443. The API server certificate kubeadm issues for a kind cluster already
names that container, so a client that gives the container's name as its TLS
server name verifies the certificate as it is, while the host of its URL is
the router address. No cluster is changed for it.

``educates admin lookup kubeconfig`` writes ``https://<router address>:6443``
for a local cluster, with the container's name as ``tls-server-name``, so the
host and the pods of every local cluster reach the API server at the same
address, and a Lookup Service on one local cluster monitors another with the
kubeconfig as it is. Both kubectl and the Lookup Service verify the API
server's certificate against ``tls-server-name``. The kube context kind writes
keeps reaching the API server at a loopback port of the host, which needs
neither the router nor the router address.

Kubeconfigs that name the router address and port 6443 are written into
Secrets of other clusters, and every router publishes the port, so reversing
this would mean re-creating every router and re-issuing every such
kubeconfig.

Considered options
------------------

The address kind writes into a cluster's internal kubeconfig,
``https://<kind-name>-control-plane:6443``, reaches the API server from the
pods of the other local clusters, over Docker's ``kind`` network, by the name
Docker's DNS gives the container. The host can't resolve that name, so a
kubeconfig written for a Lookup Service can't be checked with kubectl on the
host, and there is no one address for both. It remains what the kubeconfig
names for a cluster the router serves nothing of.

Port 443 with a name reserved under each cluster's ingress domain, such as
``kube.<domain>``, matched ahead of the domains and passed through to the API
server, needs no new port. It takes that name from every domain, so a training
portal or session ingress of that name loses HTTPS. Under derived domains a
named cluster called ``kube`` has the default cluster's API server name for
its domain, which would have to be refused. And the certificate has to carry
the name, which brings the costs of the next option.

Routing port 6443 by the cluster's domain needs the domain in the API server
certificate, added at create with a ``ClusterConfiguration`` patch of
``apiServer.certSANs``. kind appends such a patch's names to its own list since
v0.32.0, and replaced the list before. Clusters created before the change lack
the name, and a domain changed after create leaves the certificate stale,
which is what happens to every ``nip.io`` cluster when the router address
changes. The CLI would then have to check the certificate before naming the
domain, and fall back. Routing by the container's name needs none of this: the
certificate never changes, whatever the domain does. It was rejected while the
Lookup Service kept no ``tls-server-name`` from a kubeconfig, and became the
choice once it verified against it.

The kubeconfig names the router address rather than a cluster's domain. The
server name decides the route, so the host only has to reach the router, and
an address needs no DNS, where a ``nip.io`` name needs public DNS. A
kubeconfig is generated again after the router address changes, as a
``nip.io`` cluster's URLs change then too.

Consequences
------------

The router publishes ports 80, 443 and 6443 on the router address, and its
start is the check that all three are free. A router created with only 80 and
443 is re-created by ``educates local cluster create``, ``educates local
cluster start`` and ``educates local router update``, and ``educates local
router status`` advises it. ``educates local cluster create`` refuses a
``cluster.apiServer.port`` of 6443 on the router address or on every host
address, which would take the router's port. Port 6443 is no privileged port,
so Docker Desktop's privileged port setting does not concern it.

A connection that is not TLS, gives no server name, or names no local
cluster's control-plane container is closed. A client that can't give a TLS
server name other than its URL's host can't reach a cluster's API server
through the router.

A Lookup Service's watch of another cluster's API server holds one connection
through the router, which client-go pings every 30 seconds, so it is never
idle and never closes by itself. It keeps the HAProxy worker that accepted it
across reloads, as a session's terminal does, until that worker outlives
``mworker-max-reloads`` and is stopped, and the client reconnects through the
current worker.

Choosing the cluster from the routes is the router's rendering, in
``client-programs/pkg/localclusters``, and the kubeconfig's address comes from
the same rules, so a cluster's API server is served exactly when the router
serves its names.
