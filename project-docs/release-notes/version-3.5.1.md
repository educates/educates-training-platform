Version 3.5.1
=============

Bugs Fixed
----------

* We had to revert kind dependency to 0.29 (Kubernetes 1.33.1) due
  to a bug in vcluster integration where the kubelet could not execute
  livenessprobess and cluster was not working as expected.
