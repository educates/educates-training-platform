Lookup Service
==============

This directory holds the source code for the Educates lookup service, a Go
program. It provides a high level REST API for accessing workshops, where
workshops may be spread across one or more training portals, including
across clusters.

`make build` builds the service, `make test` runs its tests, the REST suite
against envtest among them, and `make help` lists the other targets. The
root Makefile's `ci-lookup-service` target runs the checks CI gates on.
