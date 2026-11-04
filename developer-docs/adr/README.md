Architecture Decision Records
=============================

An architecture decision record captures a decision which shaped the platform,
and the reasoning behind it, so that a reader who finds the result later does
not have to guess why it is the way it is.

Records are numbered in the order they were written and are not edited once
accepted, other than to note that a later record supersedes one. A decision
which is revisited gets a new record rather than an edit to the old one, so
the history of what was believed and when remains readable.

Not every decision needs one. A record is worth writing when all three of
these hold:

* Reversing the decision later would be costly.
* A reader would otherwise look at the result and wonder why it was done that
  way.
* There was a real alternative, rejected for reasons worth remembering.

A decision which is easy to reverse, or which anyone would have made the same
way, does not need a record.

Records
-------

* [0001 - Extension package images are explicitly declared](0001-extension-package-images-are-explicitly-declared.md)
* [0002 - Package images are fetched by a purpose built fetcher](0002-package-images-are-fetched-by-a-purpose-built-fetcher.md)
* [0003 - The extension package image contract](0003-the-extension-package-image-contract.md)
* [0004 - CA trust reaches components only through a key-free Secret the operator owns](0004-ca-trust-reaches-components-only-through-a-key-free-secret.md)
* [0005 - Local CA roots stay on the laptop; clusters receive per-domain intermediates](0005-local-ca-roots-stay-on-the-laptop.md)
* [0006 - How each cluster service is provided is fixed for the life of an install](0006-cluster-service-providers-are-fixed-for-the-life-of-an-install.md)
* [0007 - Image digests are reproducible within one Go release](0007-image-digests-are-reproducible-within-one-go-release.md)
* [0008 - Image names are resolved by the session-manager chart's inventory, never by the operator](0008-image-names-are-resolved-by-the-session-manager-chart.md)
