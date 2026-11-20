Educates server components are ported to Go one at a time
=========================================================

The installer operator, the ``educates`` CLI and the node CA injector are
written in Go. They share one ``go.work``, one toolchain, controller-runtime
and client-go, and one way of building images. The server components that run
the platform, the session manager, secrets manager, training portal, tunnel
manager and Lookup Service, are written in Python on kopf, aiohttp and Django.
Two languages mean two toolchains, two dependency trees to keep patched and
two ways of building images, and the training portal and the Lookup Service
have no automated tests.

Educates therefore moves its server components to Go, one component at a
time. A component stays in Python until its port lands, and no release ships
two implementations of one component.

The Lookup Service is ported first. It is about 4,000 of the roughly 26,000
lines of Python, it reaches the rest of the platform only through the
training resources it watches, the portal credentials they name and the
training portal's REST API, and those contracts can be tested from outside
the service. Its port is a drop-in
replacement: it keeps the image name, the chart, the ``lookup.educates.dev``
resources and the REST API, and there is no switch between the Python and Go
implementations.

Each port follows the conventions the Lookup Service sets:

* The component keeps its directory, which becomes the Go module
  ``github.com/educates/educates-training-platform/<component>``, listed in
  ``go.work`` so that ``make ci`` builds and tests it.
* Its image is built in stages from the Go toolchain image into
  ``gcr.io/distroless/static:nonroot``, as the operator's image is.
* It watches resources, and manages its own, with controller-runtime and
  client-go. It serves HTTP with the standard library's ``net/http``, and logs
  through controller-runtime's logr.
* It reads resources that another component owns as unstructured objects,
  projected into types of its own. Go API types for a resource are written by
  the port of the component that owns the resource. Code moves into a module
  shared between components only when a second component needs the same
  contract.

Considered options
------------------

Keeping the components in Python costs nothing now, but it keeps two
toolchains for good and leaves the runtime unable to share code or patterns
with the operator and the CLI. It also keeps the limits of kopf, such as the
one ADR 0010 records: a portal on kopf cannot list and watch with server-side
selectors.

Rewriting every component at once would leave about 26,000 lines of Python,
with few tests, to be replaced and verified together, and a regression could
not be traced to one component.

Porting the training portal first would put the largest user-facing component,
and the riskiest port, ahead of a smaller one that proves the conventions.

Extracting shared modules up front, such as one for the training portal's REST
API or for the training resources' Go types, would fix their shape before a
second consumer exists to test it. It would also turn the first port into a
change to the CLI, which already has its own client for that API.
