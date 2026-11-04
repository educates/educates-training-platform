Image names are resolved by the session-manager chart's inventory, never by the operator
========================================================================================

Educates names the images it runs by short names, such as
``training-portal``, ``base-environment`` and ``jdk21-environment``. The
session-manager chart holds the one inventory of those names, the
``session-manager.imageVersions`` helper: it composes each Educates image from
the image registry and the chart's application version, pins the upstream
images, and merges per-name overrides, keeping a name it does not know. Every
surface that names an image by short name hands the name to the chart
unresolved. The operator passes the per-name overrides of
``SessionManager.spec.images.overrides`` through, and the names given to the
image pre-puller, and never turns a name into an image reference. The chart
resolves them, and the session manager runtime reads the resolved inventory
from the configuration the chart renders, which is also how it resolves a
workshop's environment image. A reference written in full is used as written.

The rule spans the operator, the chart, the runtime and the CLI, so reversing
it later would mean moving the inventory, and every resolution, into another
component.

Considered options
------------------

The first alternative was for the operator to resolve names. It would need
its own copy of the inventory in Go, kept in step with the chart on every
image added, every version bump and every rule about which images the
registry prefix relocates. Two copies of the inventory drift, and the chart
would still need its own for installs of the runtime chart without the
operator, so the copy in the operator would only add a place to get wrong.

The second alternative was to accept only full image references wherever a
user names an image. That gives up what the names provide: relocation by the
image registry, overrides by name, and references that follow the installed
version on upgrade. Full references stay accepted where a user names an image
the inventory does not hold.

Consequences
------------

An unknown name can only be found when the chart renders. The render fails
with a message naming it, and the ``SessionManager`` reports ``Ready`` as
``False`` with reason ``InstallFailed``, rather than the custom resource
being rejected when applied. Adding an image to the inventory is a change to
the chart alone. Tests of the operator assert the names it passes on, and
tests that render the chart assert the references they resolve to. The three
images that live outside the inventory, the session-manager pod image, the
image pre-puller's own image and the node CA injector, are routed by the
operator to the chart values that control them, from references the user
writes in full.
