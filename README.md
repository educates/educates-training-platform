Educates Training Platform
==========================

Educates is a platform for hosting hands-on, interactive workshop environments.
Each workshop user gets their own isolated session with step-by-step
instructions, integrated terminals, an embedded VS Code editor, and access to
whatever runtime environment the workshop requires — from a simple container
through to a full virtual machine.

Educates can be deployed to a Kubernetes cluster to host workshops at scale, or
a single workshop can be run locally using a container runtime such as Docker.
Whether you are running a supervised workshop at a conference, providing
self-paced training through a public learning portal, or packaging up a product
demo, Educates gives every user a real, live environment to work in rather than
just slides and screenshots.

Although Educates was originally created for Kubernetes-focused training, it can
be used to teach any topic — web applications, databases, programming languages,
DevOps tooling, or anything else that benefits from a hands-on environment.

Key capabilities
----------------

Workshop content is authored as Markdown files and can be distributed from a
hosted Git repository, an OCI-compatible image registry, or a plain web server.
The instructions are rendered alongside the user's live environment in an
integrated dashboard, and can include clickable commands that execute
automatically in the correct terminal, copyable text snippets, and embedded
slide decks.

Each workshop session can be configured with the level of infrastructure access
that the topic requires:

* **Container only** — the user works inside a single container, suitable for
  workshops on programming languages, CLI tools, or applications that don't
  need Kubernetes.

* **Kubernetes namespace** — the user gets a dedicated namespace in a shared
  Kubernetes cluster, with resource quotas and RBAC applied, suitable for
  workshops that involve deploying workloads to Kubernetes.

* **Virtual Kubernetes cluster** — a per-session virtual cluster (using
  [vcluster](https://www.vcluster.com/)) provides full cluster-level access
  without the cost of provisioning a separate physical cluster.

* **Local virtual machine** — a VM running on the Kubernetes nodes can be
  provisioned using [KubeVirt](https://kubevirt.io/), giving users a complete
  Linux environment with administrator access.

* **Remote virtual machine** — integrations with infrastructure-management
  operators such as [Crossplane](https://www.crossplane.io/) can be used from a
  workshop session to provision a distinct VM on an external infrastructure
  provider.

These options can be combined, so a single workshop can give users access to a
container, a Kubernetes namespace, and a VM at the same time if the training
material calls for it.

When deployed to Kubernetes, Educates provides a training portal where users can
browse available workshops and launch sessions on demand. A REST API is also
available, allowing custom front ends or integration with external identity
providers.

Quick start
-----------

Install the Educates CLI and create a local environment:

```
educates create-cluster
```

Deploy a sample workshop and open it in your browser:

```
educates deploy-workshop -f https://github.com/educates/lab-k8s-fundamentals/releases/latest/download/workshop.yaml
educates browse-workshops
```

See the [quick start guide](https://docs.educates.dev/) for full details,
including host requirements and configuration options.

![Workshop dashboard with terminal](project-docs/project-details/dashboard-terminal.png)

Educates documentation
----------------------

For detailed instructions on how to deploy and make use of Educates see the
[Educates user documentation](https://docs.educates.dev/).

Getting help with Educates
--------------------------

If you have questions about using Educates, use the `#educates` channel under
the [Kubernetes community Slack](https://kubernetes.slack.com/).

If you have found a bug in Educates or want to request a feature, you can use
our [GitHub issue
tracker](https://github.com/educates/educates-training-platform/issues).

Contributing to Educates
------------------------

If you would like to contribute to Educates, check out our [contribution
guidelines](CONTRIBUTING.md) and [developer
documentation](developer-docs/README.md).

Adopters of Educates
------------------------

If you're using Educates Training Platform in your organization, please [let us know](./ADOPTERS.md).
