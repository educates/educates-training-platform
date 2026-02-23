---
orphan: true
---

Version 2.4.0
=============

New Features
------------

* Added the ability to specify a snippet of HTML code to be included in the HTML
  `head` section of training portal, workshop dashboard and workshop
  instructions pages. For more details see [Overriding styling of the
  workshop](overriding-styling-of-the-workshop).

* Added support for monitoring the training portal and workshop sessions with
  Microsoft Clarity. Note that Clarity doesn't support any concept of events.
  Further, currently CSS data will not be available to Clarity for workshop
  sessions as it is behind user authentication for the session. This means
  page depictions in recordings and heatmaps will not be correct. For more
  details see [Tracking using Microsoft
  Clarity](tracking-using-microsoft-clarity).

* Added support for monitoring the training portal and workshop sessions with
  Amplitude. Only events pertaining to workshop sessions are sent to Amplitude,
  and not events which are generated internally to the training portal or
  operator. For more details see [Tracking using
  Amplitude](tracking-using-amplitude).

* Added the ability to specify the name of a persistent volume claim to be used
  for storage by the workshop, docker and registry. This is in place of
  specifying the storage size and having Educates worry about creation of the
  persistent volume claim. Do note that the registry is a separate deployment to
  the workshop (where docker daemon also runs as side car container) so you
  cannot share a persistent volume claim of type ``ReadWriteOnce`` between the
  registry and workshop unless you know you have a single node cluster. For more
  details see [Mounting a persistent volume](mounting-a-persistent-volume),
  [Enabling ability to use docker](enabling-ability-to-use-docker) and [Enabling
  session image registry](enabling-session-image-registry).

* Added the new CLI sub command ``educates admin secrets import`` to allow
  arbitrary secrets to be added to the local secrets cache.

Features Changed
----------------

* If using Google Analytics you must now be using Google Analytics 4. Use of the
  older Universal Analytics is no longer supported. Do note that Google is
  retiring Universal Analytics in July 2023, so you need to migrate to Google
  Analytics 4 regardless. For more details see [Tracking using Google
  Analytics](tracking-using-google-analytics).

* When events are reported to Google Analytics or the webhook consumer, the
  names of some of the event properties have been renamed to to align with
  naming conventions for workshops and workshop environments.

  ```text
  session_namespace -> session_name
  workshop_namespace -> environment_name
  ```

  A new event property of ``session_owner`` has also been added. This will be
  the internal identifier used by the training portal to identify the user
  across workshop sessions. Alternatively, if a front end portal had supplied
  its own user identifer when creating a workshop session using the REST API of
  the training portal, it will be used instead. Do note that it is recommended
  that any such user ID if supplied by a front end portal still be effectively
  anonymous, eg., a uuid, and not an email address.

  The names of properties included in supplemental data for some events have
  also changed, such as properties identifying pages within workshop
  instructions, embedded terminals and clickable actions.

Bugs Fixed
----------

* When specifying storage for a workshop session, or docker was enabled, and a
  custom workshop image was being used, files from the custom workshop image
  were not being copied from the workshop home directory into the persistent
  volume, or shared container storage, as expected. The workshop content would
  therefore be missing. This mechanism was broken in version 2.3.0 of Educates.

* Events denoting that a workshop had been finished or terminated, generated for
  analytics from a workshop session, could be lost and not delivered as the
  immediate page redirection that followed would occur before the tasks to
  deliver the events got to run. A workshop session now has a cleanup phase
  whereby time is allowed for sending the events when a workshop session is
  being ended.

* If using the Chrome browser and the embedded VS Code editor was enabled, all
  attempts to paste into a shell terminal created within VS Code would fail.
  This started occurring with a version of Chrome released towards the end of
  2022. It is now possible to paste into the shell terminal, however the Chrome
  browser will ask you the first time for permission to do it for the site
  before allowing it.

* When syncing secrets using the CLI from the local secret cache to the cluster,
  secret data was being ignored if it was declared using the ``stringData``
  property of a Kubernetes secret, rather than the ``data`` property.

* The ingress protocol property was not being reported in Google Analytics
  events correctly.
