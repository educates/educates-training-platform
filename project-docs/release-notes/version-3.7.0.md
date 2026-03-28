Version 3.7.0
=============

New Features
------------

* Added new editor clickable actions for workshop instructions:
  `editor:prepend-lines-to-file`, `editor:append-lines-after-line`,
  `editor:insert-lines-before-match`, `editor:insert-lines-before-selection`,
  `editor:append-lines-after-selection`, and `editor:delete-text-selection`.

Features Changed
----------------

* The `editor:insert-lines-after-line` clickable action has been deprecated in
  favour of `editor:append-lines-after-line`. The new name is consistent with
  the naming convention where "insert" means before and "append" means after.
  The old action name will continue to work but should be replaced with the new
  name in workshop content.

* When the workshop session container fetches the OAuth access token from the
  training portal, it will now use internal Kubernetes service for the training
  portal instead of using the public URL. This means it should work even when
  pods in the cluster cannot access the external load balancer for the cluster
  ingress router.

Bugs Fixed
----------

* Because Coder version of VS Code keeps editor state in browser cookies, if
  you started a workshop and got the same hostname as was used for a workshop
  in the past, it could try and open a editor on a file from the previous
  workshop when the VS Code editor is started up. This file would not exist
  if was a completely different workshop, or was created by later steps in the
  workshop. Thus see an editor pane with an error in it. What will now be done
  is that the helper extension for the editor will now close all editors when
  VS Code starts up the first time. You may see the editor windows come up
  momentarily and then be removed, but they will at least not perist.
