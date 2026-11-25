Developer Documentation
=======================

The Educates project consists of the following Git repositories:

* Educates Training Platform - https://github.com/educates/educates-training-platform
* Educates GitHub Actions - https://github.com/educates/educates-github-actions

The Educates Training Platform repository (this repository), holds all source code for building and making releases of the core platform, as well as documentation.

The Educates GitHub Actions repository holds GitHub actions to assist in publishing workshops to GitHub container registry.

If wanting to contribute to Educates, you can build and deploy a local copy of Educates by following the [build instructions](build-instructions.md).

To check a change by hand in real workshop sessions, on a local cluster of its own built from your change, use the harnesses described in [hack/harnesses](../hack/harnesses/README.md). They cover the runtime components with few automated tests, such as session-manager, the training portal and tunnel-manager.

To look at a component's states by hand, such as every failing state of the secrets-manager's resources, use the validation kits described in [hack/kits](../hack/kits/README.md). A kit leaves its states in an existing cluster for you to inspect, checks that the cluster shows them as described, and removes them when asked.

For how Git branches and version tags are managed, and the workflow for contributing features and fixes, see the [branching strategy](branching-strategy.md).

For details on the design of Educates and how it works check out notes on it's [platform architecture](platform-architecture.md).

For decisions which shaped the platform, and the reasoning behind them, see the [architecture decision records](adr/README.md).

If you want to learn about future directions the Educates project may take, check out the [project roadmap](project-roadmap.md).

For maintainers, steps required to create a release of Educates are detailed in the [release procedures](release-procedures.md).
