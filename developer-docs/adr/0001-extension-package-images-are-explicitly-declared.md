Extension package images are explicitly declared, not inferred
==============================================================

A workshop declares an extension package published as an image by setting
``image`` on the package, rather than Educates inferring it from the existing
``files`` form by inspecting what the reference points at. The field is public
API on the Workshop custom resource, so this is hard to reverse once workshops
are written against it.

Considered options
------------------

The alternative was to keep the existing ``files`` declaration unchanged and
work out at workshop environment creation whether the referenced image was
built as an extension package, by pulling its manifest and looking for the
label the publish command writes. Authors would then move a package from one
delivery to the other by republishing it, without editing any workshop.

That was rejected for two reasons.

The first is that it cannot be made to work reliably. Deciding by inspection
means the session manager must read the image from the control plane, but when
the package is later mounted it is the kubelet on the node which pulls it,
with credentials and a network path the session manager cannot see. A registry
which answers the session manager may not answer the node, and the reverse.
The two would disagree, and the disagreement would surface as a session which
starts without its package.

The second is that there is no safe default when the inspection fails. A
registry which is briefly unreachable at environment creation would have to
resolve to one delivery or the other. Choosing to mount risks a workshop
environment which cannot start sessions; choosing to fetch silently drops the
behaviour the author asked for. Neither is defensible, and the failure arrives
at environment creation, far from the author who could fix it.

An explicit field has neither problem. The declaration is read from the
workshop definition, which is already in hand, and a wrong reference is caught
in the session by the manifest check with a message naming the package.

Consequences
------------

Moving a package between the two forms is an edit to the workshop definition
rather than a republish. That is the cost, and it is accepted: the delivery
mechanism within the image form is still chosen by the platform, so authors do
not edit workshops to follow what a cluster supports.
