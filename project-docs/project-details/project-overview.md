Project Overview
================

Educates is a platform for hosting hands-on, interactive workshop environments. Each workshop user gets their own isolated session with step-by-step instructions, integrated terminals, an embedded VS Code editor, and access to whatever runtime environment the workshop requires — from a simple container through to a full virtual machine.

Educates can be deployed to a Kubernetes cluster to host workshops at scale, or a single workshop can be run locally using a container runtime such as Docker. Whether you are running a supervised workshop at a conference, providing self-paced training through a public learning portal, or packaging up a product demo, Educates gives every user a real, live environment to work in.

Although Educates was originally created to support a team of developer advocates who needed to train users in Kubernetes and showcase developer tools, it can be used to teach any topic — web applications, databases, programming languages, DevOps tooling, or anything else that benefits from a hands-on environment.

Source code repositories
------------------------

The source code repository for the Educates training platform can be found at:

* [https://github.com/educates/educates-training-platform](https://github.com/educates/educates-training-platform)

Latest project release
----------------------

To determine what is the most recent released version of Educates, see:

* [https://github.com/educates/educates-training-platform/releases/latest](https://github.com/educates/educates-training-platform/releases/latest)

Getting help with Educates
--------------------------

If you have questions about using Educates, use the `#educates` channel under
the [Kubernetes community Slack](https://kubernetes.slack.com/).

If you have found a bug in Educates or want to request a feature, you can use
our [GitHub issue
tracker](https://github.com/educates/educates-training-platform/issues).

Use case scenarios
------------------

Educates has been designed to support a range of training and demo scenarios:

* **Supervised workshops** — A workshop run at a conference, at a customer site, or online. The workshop has a set time period and a known maximum number of users. Once the training has completed, the Kubernetes cluster created for the workshop can be destroyed.

* **Temporary learning portal** — Short-duration workshops made available as hands-on demos at a conference vendor booth. Users select a topic, work through the workshop on demand, and the session is destroyed when they finish to free up resources. The cluster is torn down when the event ends.

* **Permanent learning portal** — Similar to a temporary portal, but run on an ongoing basis as a public site where anyone can come and learn at any time.

* **Personal training or demos** — An individual runs a workshop on their own Kubernetes cluster to learn a topic, or uses a workshop packaged as a product demo to present to a customer. For quick local use, a single workshop can also be run directly using a container runtime such as Docker, without needing a full Kubernetes cluster.

Workshop environment options
-----------------------------

Each workshop session can be configured with the level of infrastructure access that the topic requires:

* **Container only** — The user works inside a single container. This is suitable for workshops on programming languages, CLI tools, or applications that don't need Kubernetes.

* **Kubernetes namespace** — The user gets a dedicated namespace in a shared Kubernetes cluster, with resource quotas and role based access control (RBAC) applied. This is suitable for workshops that involve deploying workloads to Kubernetes without needing cluster-level access.

* **Virtual Kubernetes cluster** — A per-session virtual cluster (using [vcluster](https://www.vcluster.com/)) provides full cluster-level access, including cluster admin privileges, without the cost of provisioning a separate physical cluster.

* **Local virtual machine** — A VM running on the Kubernetes nodes can be provisioned using [KubeVirt](https://kubevirt.io/), giving users a complete Linux environment with administrator access.

* **Remote virtual machine** — Integrations with infrastructure-management operators such as [Crossplane](https://www.crossplane.io/) can be used from a workshop session to provision a distinct VM on an external infrastructure provider.

These options can be combined, so a single workshop can give users access to a container, a Kubernetes namespace, and a VM at the same time if the training material calls for it.

Workshop content and the user experience
-----------------------------------------

Workshop instructions are written as Markdown files. Content can be distributed from a hosted Git repository, an OCI-compatible image registry, or a plain web server. The instructions are rendered using Hugo and displayed alongside the user's live environment in an integrated dashboard.

The workshop dashboard includes:

* **Clickable commands** — Instructions can be annotated so that when a user clicks on a command it is automatically executed in the correct terminal, avoiding mistakes from manual entry.

* **Copyable text** — Text snippets can be marked as copyable so that clicking them copies the text to the browser clipboard, ready for pasting into a terminal or other application.

* **Integrated terminals** — One or more terminal sessions are available directly in the dashboard.

* **Embedded editor** — An IDE based on VS Code can be enabled for users to edit files during the workshop.

* **Kubernetes web console** — A web-based console for interacting with the Kubernetes cluster can be enabled for Kubernetes-focused workshops.

* **Slide deck** — Slides can be included alongside the instructions to support instructor-led workshops. HTML-based presentation tools such as ``reveal.js`` or ``impress.js`` can be used, or a PDF can be embedded.

* **Custom dashboard tabs** — Additional web-based applications specific to the topic of the workshop can be integrated into the dashboard.

For a detailed look at how the platform is structured, including content distribution, the workshop session dashboard, Kubernetes deployment internals, and local deployment options, see the [Platform Architecture](platform-architecture) page.
