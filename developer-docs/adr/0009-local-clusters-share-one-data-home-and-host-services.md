Local clusters share one data home and host services, each on its own address
=============================================================================

The CLI runs a default local cluster and any number of named ones beside
it. The default local cluster is the kind cluster ``educates``, with the kube
context ``kind-educates``, and a named local cluster ``<name>`` is the kind
cluster ``educates-<name>``, with the context ``kind-educates-<name>``. A
command selects a cluster with ``--local-cluster <name>`` or the
``EDUCATES_LOCAL_CLUSTER`` environment variable, the option taking
precedence, and acts on the default cluster with neither. No current cluster
is stored. Every cluster belongs to one CLI data home: the default cluster's
config is ``config.yaml`` at its top, a named cluster's is in
``clusters/<name>/``, and a kind cluster ``educates-<name>`` counts as a local
cluster only while that folder exists.

Every cluster declares in ``cluster.listenAddress`` a specific IP address of
the host, neither unset, ``0.0.0.0`` nor a loopback address. kind publishes
the cluster's ports 80 and 443 on it, the cluster's ingress domain resolves to
it, and ``educates local cluster create`` and ``start`` refuse an address
another cluster binds. One registry on ``localhost:5001``, one set of registry
mirrors, one DNS resolver and one CA root serve every cluster. Each cluster
contributes its names to the resolver, at its listen address, and the
resolver has no settings of its own. Settings that span the host live in
host-level files of the data home, never in a cluster's config:
``mirrors.yaml`` declares the registry mirrors, is written by ``educates local
mirror deploy`` and ``delete``, and every mirror is linked into every cluster;
the ``*.conf`` files in ``dnsmasq.d/`` hold the user's own resolver names, and
the CLI never writes there.

The cluster names, the layout of the data home and the config schema are on
every user's machine, so reversing this after release would mean migrating
every data home and re-creating clusters: kind fixes a cluster's name and the
address its ports are published on when it creates the cluster, and the
config schema has already dropped ``resolver.targetAddress`` and
``cluster.registryMirrors`` in favour of the host-level files.

Considered options
------------------

Two facts shaped every option. The runtime serves ingress only on ports 80
and 443: the session manager builds every workshop URL on one of them, with
no port in it. Clusters side by side therefore cannot be told apart by port,
and each needs a host address of its own. And kind makes the cluster it
created last the current kube context, so commands that follow the current
context silently move to the newest cluster.

Running each cluster from a data home of its own, through
``EDUCATES_CLI_DATA_HOME``, was rejected. A second data home still creates
the kind cluster ``educates``, binds the same host ports and manages the same
registry, mirror and resolver containers as the first, so the two collide.
Making each data home independent would also give each its own CA root to
import into the trust stores, where
[0005](0005-local-ca-roots-stay-on-the-laptop.md) keeps one root per data
home, trusted once, with an intermediate CA for each ingress domain.

Giving each cluster its own registry, mirror set, resolver or Docker network
was rejected. An image published to the local registry is the same image for
every cluster, and testing two versions of one workshop needs two tags
whichever registry holds them. kind puts every cluster on its one ``kind``
network, so one registry and one set of mirrors already reach all of them,
and mirrors fill their caches once. The Docker workshop path and the Docker
Desktop extension rely on the shared registry and on the ``educates`` and
``kind`` networks, so they keep working unchanged. A mirror container is named
after the registry it mirrors, so two clusters could not each run their own
mirror of one registry without new names. And only one resolver can listen
on port 53 of the loopback address, where the host's resolver files point.

Storing a current cluster, as kubectl stores a current context, was rejected.
It would be one setting shared by every terminal, the problem kind's context
switching already causes: a command in one terminal would follow whichever
cluster was selected last in another. An option and an environment variable
keep the choice to one command or one terminal, and with neither the commands
behave as they did with one cluster.

Counting every kind cluster named ``educates-<name>`` as a local cluster was
rejected. A kind cluster made by hand can carry such a name, and the clash
checks, the lifecycle commands and ``delete --all``'s count of the clusters
still using the shared containers would then act on it. The CLI makes the
folder ``clusters/<name>/`` when it creates a named cluster, so the folder
records which kind clusters are the CLI's without adding anything to kind's
containers.

Publishing a cluster on a loopback address, the earlier default
``127.0.0.1`` or an alias such as ``127.0.0.2``, was rejected. Nothing
customises in-cluster DNS for the ingress domain, so pods resolve it as the
host does, and inside a pod a loopback address reaches the pod itself: the
cluster could not reach its own ingress. A default cluster on ``127.0.0.1``
whose ``nip.io`` domain named the host's IP reached nothing at all, since the
domain answered an address on which nothing listened.

Defaulting to ``0.0.0.0``, as 3.x did by leaving the address to kind, was
rejected because it claims ports 80 and 443 on every host address and leaves
none for another cluster. Keeping it as an opt-in was considered for Colima,
which Educates does not test, so it is refused like a missing or loopback
address. Requiring an ingress domain, with no ``nip.io`` fallback, was
considered and dropped, since the problems came from the listen address and
not from the domain: on a specific address, a cluster with no domain of its
own is served on the ``nip.io`` name of that address, with no resolver or CA
to set up.

Having the CLI add the address to a host interface was rejected, since it
needs ``sudo``, which the CLI never runs. The user adds an address once; the
CLI writes the host's IP for the default cluster, and checks that an address
is on a host interface before kind publishes on it.

Keeping ``resolver.targetAddress``, so that a cluster's names could resolve
to an address other than its listen address, was rejected. It moves every
name of the cluster, its ingress domain included, so it cannot send one extra
domain to a virtual machine, which is what it was used for. A map from each
extra domain to an address, in a cluster's config, was rejected too: a named
cluster's entries are rendered only while its kind cluster exists, so a name
for a virtual machine would vanish with an unrelated cluster. Editing the
rendered ``dnsmasq.conf`` by hand does not work either, since creating and
deleting a cluster render it again. Names that resolve elsewhere go in the
user's own files in ``dnsmasq.d/``, which the resolver container mounts and
the rendered config includes.

Declaring registry mirrors in each cluster's config, as
``cluster.registryMirrors`` did, or in a top-level list of each config, was
rejected. One container serves each mirrored registry for every cluster, and
every mirror container that exists is linked into every node of every
cluster, which is harmless: a node with a mirror's configuration tries the
mirror first and then the registry the image names. A mirror declared in one
cluster's config therefore reached clusters whose config said nothing of it,
and a config misstated what its cluster got. ``mirrors.yaml`` declares the
mirrors once for the host. ``educates local mirror deploy`` and ``delete``
write it, since a file no command wrote let a mirror be deployed and deleted
without a trace, and they rewrite only the entries they touch, so hand edits
and comments survive.

Consequences
------------

The default cluster keeps its names and files, and needs no migration, with
one exception: it too needs a specific listen address. ``educates local
config init``, and ``educates local cluster create`` when the default cluster
has no config, write the host's IP. A config without a usable address fails
where it is used, suggesting the host's IP, and the v3 migration writes the
host's IP for a missing or ``0.0.0.0`` address. A cluster created earlier on
``0.0.0.0`` blocks every other cluster until it is re-created, and the clash
checks name it and say so. A laptop's IP changes between networks, so an
alias such as ``10.10.10.1`` added to an interface is the durable choice. On
Docker Desktop 4.97 and later, ports below 1024 on a non-loopback address
need the setting "Allow privileged port forwarding to external machines", so
create and start refuse while it is off.

The commands for the shared host services, ``educates local registry``,
``local mirror``, ``local resolver`` and every ``local secrets`` command but
``sync``, take no ``--local-cluster`` and act on every cluster. ``educates
local cluster delete --all`` removes the shared registry, resolver and mirrors
only with the last local cluster, and otherwise names the clusters still
using them. Creating and deleting a cluster render the resolver's config
again. Two clusters claiming one domain is an error naming both, while a
named cluster may take a subdomain of another's, since dnsmasq answers from
the most specific domain. ``educates local resolver view`` shows each name
with the cluster or file it comes from.

A config in the data home that still has ``cluster.registryMirrors`` is moved
once into ``mirrors.yaml``, keeping the previous config as a backup beside
it, and a config outside the data home fails, showing the ``educates local
mirror deploy`` commands that record its mirrors. A config that still sets
``resolver.targetAddress`` fails with an error saying what replaced it.

With a cluster selected, the ``educates cluster`` and ``educates admin``
commands use its kube context unless ``--context`` names one, and selecting a
cluster never changes the current context. ``EDUCATES_CLI_DATA_HOME`` still
moves the data home, but it is not a way to run several clusters.

The rules that span clusters, selection, identity, which kind clusters
count, the listen address and clash checks, the resolver's rendering, mirror
linking and when the shared containers may go, live in
``client-programs/pkg/localclusters``, which takes the observed state of the
host as plain values and is tested without Docker. A new setting that spans
the host goes in a host-level file of the data home, beside ``mirrors.yaml``
and ``dnsmasq.d/``, rather than in a cluster's config, and a new rule across
clusters goes in that package.
