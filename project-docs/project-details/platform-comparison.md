Platform Comparison
===================

This page compares Educates with other platforms in the interactive hands-on training space. It is written from the Educates project's perspective and is intended to help people evaluating Educates understand where it sits relative to other options. The information here is believed to be accurate at the time of writing but features and pricing of commercial platforms can change — check each platform's own site for the latest details.

Comparison summary
-------------------

```{list-table}
:header-rows: 1
:widths: 20 16 16 16 16 16

* -
  - Educates
  - Instruqt
  - Killercoda
  - Strigo
  - CloudShare
* - **Open source**
  - Yes (Apache 2.0)
  - No
  - No
  - No
  - No
* - **Hosting model**
  - Self-hosted
  - SaaS only
  - SaaS only
  - SaaS only
  - SaaS only
* - **Initial setup cost**
  - $ (your infrastructure)
  - $$$
  - Free
  - $$$
  - $$$$
* - **Ongoing cost**
  - $ (infrastructure + management)
  - $$$–$$$$
  - Free–$
  - $$$–$$$$
  - $$$$
* - **Content as code**
  - Yes (Markdown in Git)
  - Yes (Markdown/YAML)
  - Yes (Markdown in Git)
  - Partial (GitHub integration)
  - No
* - **Content distribution**
  - Git, OCI registry, or web server
  - Platform-hosted
  - Git repository
  - Platform-hosted
  - Platform-hosted
* - **Learner environment**
  - Container, K8s namespace, vcluster, VM (KubeVirt), remote VM (Crossplane)
  - Cloud VMs (AWS, Azure, GCP), containers, Kubernetes
  - Linux VMs, Docker, Kubernetes
  - Cloud VMs (AWS, Azure, GCP), Linux, Windows
  - Cloud VMs, complex multi-VM environments
* - **Kubernetes native**
  - Yes (runs on Kubernetes)
  - No
  - No
  - No
  - No
* - **Embedded terminal**
  - Yes
  - Yes
  - Yes
  - Yes
  - Yes
* - **Embedded IDE**
  - Yes (VS Code)
  - Yes
  - Yes
  - Yes (code editor)
  - No
* - **Instructor-led support**
  - Yes (training portal, shared sessions)
  - Yes (instructor dashboard, messaging)
  - No
  - Yes (full virtual classroom with video)
  - Yes (video, chat, over-the-shoulder)
* - **Training portal**
  - Yes (built-in)
  - Yes
  - Yes (public catalog)
  - Yes
  - Yes
* - **REST API**
  - Yes
  - Yes (GraphQL)
  - No
  - Yes
  - Yes
* - **Custom branding**
  - Full control (self-hosted)
  - Yes (via API)
  - No
  - Yes (branded courses)
  - Limited (logo only)
* - **Data sovereignty**
  - Full control (your cluster)
  - Vendor-managed
  - Vendor-managed
  - Vendor-managed
  - Vendor-managed
```

Platform notes
---------------

### Educates

Educates is the only open source, self-hosted option in this comparison. It is deployed to your own Kubernetes cluster (or run locally using Docker for single workshops), which means you control the infrastructure, the data, and the costs. There are no per-seat or per-hour licensing fees — your costs are your infrastructure and the time spent managing it. Educates is Kubernetes-native, which makes it a natural fit for Kubernetes and cloud-native training, but it can also host workshops on any topic. Workshop content is authored as Markdown files and can be distributed from Git, an OCI registry, or a web server, supporting a full content-as-code workflow.

The trade-off is that you are responsible for running and maintaining the Kubernetes cluster. For organisations that already operate Kubernetes, this is straightforward. For those that don't, the Educates CLI simplifies local development and can perform opinionated installs to cloud providers, but there is still more operational overhead than a managed SaaS platform.

### Instruqt

[Instruqt](https://instruqt.com/) is a SaaS platform aimed primarily at go-to-market teams at technology companies who want to drive product adoption through hands-on labs. It supports cloud VM environments across AWS, Azure, and GCP, and provides a visual content editor alongside Markdown/YAML configuration. Instruqt has strong analytics and CRM integrations (Salesforce, HubSpot) which make it popular for product-led growth use cases.

Instruqt uses enterprise pricing with no public free tier. Published estimates suggest costs starting around $15,000 for a block of lab hours, making it one of the more expensive options and best suited to organisations with substantial training budgets.

### Killercoda

[Killercoda](https://killercoda.com/) is the successor to Katacoda, which was the most widely known free interactive learning platform before it was retired in 2022. Killercoda provides free access to Linux and Kubernetes environments with a 60-minute session limit on the free tier. Content is authored as Markdown with bash setup scripts and stored in Git repositories.

Killercoda is a good option for individual learners and community-contributed tutorials. However, it has limited features for organisations: there is no self-hosting, no REST API, no custom branding, and no instructor-led workshop support. It is best suited to public, self-paced learning content rather than private or commercial training delivery.

### Strigo

[Strigo](https://strigo.io/) is a SaaS platform focused on instructor-led virtual training. Its standout feature is a full virtual classroom with enterprise-grade video conferencing, breakout rooms, chat, and real-time progress tracking — making it a strong choice when the primary need is live, facilitated training sessions. Lab environments support AWS, Azure, and GCP VMs.

Strigo uses custom pricing with no public tiers. It is aimed at software companies delivering customer education and partner enablement. The platform does not support self-hosting and has some limitations around networking between lab resources.

### CloudShare

[CloudShare](https://www.cloudshare.com/) is a SaaS platform designed for provisioning complex, multi-VM training environments. It is well suited to enterprise software training where the lab needs to replicate a real-world deployment of a multi-component product. It includes in-app video conferencing and AI-guided learning paths.

CloudShare uses usage-based pricing at enterprise-tier costs, making it one of the more expensive options in this comparison. Branding customisation is limited. It is best suited to organisations that need to spin up complex, multi-machine environments and have the budget for a premium managed service.

### KodeKloud

[KodeKloud](https://kodekloud.com/) is a learning platform rather than a workshop hosting platform — it provides a catalog of pre-built courses and labs on DevOps, Kubernetes, and cloud topics. Individual subscriptions start at around $15/month. KodeKloud is included here because it appears in searches alongside the other platforms, but it serves a different purpose: it is a place to consume existing training content, not a platform for organisations to host their own workshops. Third parties cannot independently create and distribute their own content on KodeKloud.

Where Educates fits
--------------------

Educates is the strongest choice when one or more of the following apply:

* **You want to self-host.** Educates is the only option that runs on your own infrastructure. This gives you full control over data, security, network policies, and compliance — important for organisations with data sovereignty requirements or air-gapped environments.

* **You want to avoid per-seat or per-hour SaaS costs.** With Educates, your ongoing costs are your Kubernetes infrastructure and the operational effort to manage it. There are no licensing fees, and scaling is a matter of cluster capacity rather than pricing tiers.

* **You need Kubernetes-native integration.** Educates runs on Kubernetes and provides first-class support for Kubernetes namespaces, virtual clusters, and integration with Kubernetes operators for VMs. If you are training people on Kubernetes itself, or on products that run on Kubernetes, Educates provides the most natural environment.

* **You want a content-as-code workflow.** Workshop content lives in Git, can be reviewed via pull requests, versioned, and distributed through standard infrastructure (Git, OCI registries, web servers). There is no dependency on a proprietary content editor or platform-hosted storage.

* **You need flexible environment options.** From a simple container to a full VM provisioned via Crossplane, Educates covers a wide range of session environments that can be mixed and matched within a single workshop.

A SaaS platform may be a better fit in the following situations:

* **You don't have Kubernetes expertise.** Educates requires a running Kubernetes cluster for its full feature set. If your team doesn't already operate Kubernetes and doesn't want to learn, a managed SaaS platform removes that operational burden entirely.

* **You want zero infrastructure management.** With Educates you are responsible for provisioning, scaling, and maintaining the cluster. For small teams or one-off events, this overhead may outweigh the cost savings compared to a pay-per-use SaaS service.

* **You need built-in video conferencing.** Educates does not include video or virtual classroom features. If instructor-led training with integrated video, breakout rooms, and screen sharing is a primary requirement, Strigo and CloudShare provide this out of the box.

* **You need CRM and marketing integrations.** If the goal is product-led growth with analytics feeding into Salesforce or HubSpot, Instruqt is purpose-built for that workflow and provides integrations that Educates does not.
