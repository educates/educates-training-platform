Version 3.5.2
=============

Features Changed
----------------

* Local registry and local mirrors now get a fixed IP on the kind network
  for better support Docker Desktop restarts.

* Initial check of port 80/443 are now done in a simpler (more performant) way

* When local cluster detects a disconnected install it will skip-image-resolution
  automatically. A message will be printed in the output.
