(creating-a-workshop)=
Creating a Workshop
===================

To assist you if you want to create workshops with your own content, the Educates command line tool bundles a template which can be used to create a new workshop.

The ``educates`` command line tool also gives the commands used most often
while creating and deploying a workshop a shorter alias at its top level, such
as ``educates new-workshop`` for ``educates workshop new``. This page names
each command in full and gives its alias where the command first appears.
Running ``educates --help`` lists all the aliases.

Generating the workshop
-----------------------

To generate a new workshop run the ``educates workshop new`` command, or its
alias ``educates new-workshop``, passing it as argument a directory path to
where the workshop content should be placed.

```
educates workshop new lab-new-workshop
```

The last component of the supplied path will be used as the workshop name.

As the workshop name must conform to what is valid for a RFC 1035 label name, as detailed in [Kubernetes object name and ID](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/), the last component of the supplied path should:

* contain only lowercase alphanumeric characters or '-'
* start with an alphabetic character
* end with an alphanumeric character

Although a RFC 1035 label name allows for up to 63 characters, it is recommended that the name be restricted to 25 characters or less. This is because Educates will in various cases add a prefix or suffix to the name and if your name is too long, the name with prefix or suffix will exceed 63 characters and be rejected by Kubernetes in the situation it is being used.

If you are hosting the workshop content in a Git hosting service such as GitHub, the name of the hosted Git repository must match the name of the workshop if you intend making use of the GitHub actions supplied by the Educates project for publishing workshops.

It is also recommended that workshop names always be prefixed with `lab-`. This ensures the directory or Git repository is more easily identified as being that for a workshop amongst any other Git repositories.

Rendering of instructions
-------------------------

Educates renders workshop instructions with [Hugo](https://gohugo.io/), which
generates them as static HTML files using custom layouts provided by Educates.
Workshop instructions rendered by Hugo are written in Markdown.

When you ran ``educates workshop new`` it generated the configuration and
instructions files for Markdown pages rendered by Hugo, using the ``hugo``
template, which is the only template the command holds.

Workshops written for the classic renderer, which was removed in Educates 4.0,
need to be converted before they can be used. See [Converting a classic
workshop to Hugo](converting-a-classic-workshop).

Deploying new workshop
----------------------

To deploy a new workshop, from within the workshop directory, in this case the
``lab-new-workshop`` directory, first run the following command, or its alias
``educates publish-workshop``:

```
educates workshop publish
```

This will build an OCI image artifact containing the workshop content files and push it to the local image registry created with the local Kubernetes cluster.

Note that by default the whole workshop directory will be packaged up and included in the OCI image artefact. What is included can be customized by supplying in the workshop definition a specification for what should be included.

Having published the workshop you can then create the workshop environment in
the Kubernetes cluster by running the following command, or its alias
``educates deploy-workshop``:

```
educates cluster workshop deploy
```

The deployed workshop will pull the workshop content files published to the image registry for each workshop session.

To access the training portal from your web browser and select the workshop,
run the following command, or its alias ``educates browse-workshops``:

```
educates cluster portal open
```

If necessary, you can view the password for accessing the training portal
instance by running the following command, or its alias
``educates view-credentials``:

```
educates cluster portal password
```

To view instead the admin credentials, the username and password for logging
into the admin pages of the training portal, add the ``--admin`` option:

```
educates cluster portal password --admin
```

The admin credentials are read from the secret that the status of the
training portal names under ``status.educates.credentialSecrets.admin``, in
the namespace of the training portal, ``<portal-name>-ui``. The identity of
the kube context the command is run against therefore needs ``get`` access on
secrets in that namespace. If it does not have it, the command fails with an
error naming the secret and the namespace it could not read.

Workshop content layout
-----------------------

The workshop template when used creates the following files in the top level directory:

* ``README.md`` - A file telling everyone what the workshop is about. Replace the current content provided in the sample workshop with your own.

Key sub directories and the files contained within them are:

* ``workshop`` - Directory under which your workshop files reside.
* ``workshop/content`` - Directory under which your workshop instructions resides.
* ``resources`` - Directory under which Kubernetes custom resources are stored for deploying the workshop using Educates.
* ``resources/workshop.yaml`` - The custom resource for Educates which describes your workshop and requirements it may have when being deployed.

A workshop may optionally also have the file:

* ``workshop/config.yaml`` - Configuration file with details of available modules which make up your workshop, data variables for use in content, and selectable paths through the workshop instructions.

If ``workshop/config.yaml`` doesn't exist or no configuration is included
within it, the pages of the workshop instructions are ordered by file name, or
by page weights if they are defined in the front matter of the pages.

If your workshop instructions use images, follow the Hugo convention and place
the images in the ``workshop/static`` directory, or use page bundles and
include the image for a page in the directory of its page bundle.

A workshop may consist of other configuration files, and directories with other types of content, but this is the minimal set of files to get you started.

Root directory for exercises
----------------------------

Because of possible proliferation of files and directories at the top level of the repository and thus potentially the home directory for the user when running the workshop environment, you can place files required for exercises during the workshop into the ``exercises`` sub directory below the root of the repository.

When such an ``exercises`` sub directory exists, the initial working directory for the embedded terminal when created will be set to be ``$HOME/exercises`` instead of ``$HOME``. Further, if the embedded editor is enabled, the sub directory will be opened as the workspace for the editor and only directories and files in that sub directory will be visible through the default view of the editor.

Note that the ``exercises`` directory isn't set as the home directory of the user. This means that if a user inadvertently runs ``cd`` with no arguments from the terminal, they will end up back in the home directory.

To try and avoid confusion and provide a means for a user to easily get back to where they need to be, it is recommended if instructing users to change directories, to always provide a full path relative to the home directory. Thus use a path of the form ``~/exercises/example-1`` or ``$HOME/exercises/example-1`` rather than ``example-1``, to the ``cd`` command if changing directories. By using a full path, they can execute the command again and know they will end up back in the required location.

(modifying-workshop-content)=
Modifying workshop content
--------------------------

After having made any changes to the workshop content you want to test the changes, you need to rebuild the OCI image artifact containing the workshop content files.

Make a change to the instructions in the file `workshop/content/00-workshop-overview.md`. Then run:

```
educates workshop publish
```

If you are currently in a workshop session, end that session. Now create a new workshop session from the training portal. You should see the changes you have made.

For minor changes, instead of ending the workshop session and creating a new one, you can instead after publishing the update to the workshop content files, from the terminal inside of the workshop session, run:

```
update-workshop
```

This will pull a new copy of the workshop content files into the workshop session. You can then either refresh your browser window, or to update just the view of the current workshop instructions, hold down the `<SHIFT>` key while clicking on the reload icon in the top banner of the workshop session dashboard.

You can also activate a live reload mode for workshop instructions. To
activate this run the following command, or its alias
``educates serve-workshop``:

```
educates cluster workshop serve --patch-workshop
```

This command will patch the workshop definition which exists in the cluster,
triggering the deployment of a new workshop environment, where workshop
sessions will be configured to connect back and load workshop instructions
from a web server run by the ``educates cluster workshop serve`` command.

To render the instructions with the values of a workshop session's
variables, the command fetches them from the session, using the session's
config password. For a session created by a training portal, it reads that
password from a secret in the namespace of the session's workshop
environment, so the identity of the kube context the command runs with needs
``get`` access on secrets in that namespace. On a local cluster created with
the ``educates`` command line tool you have that access already.

To access the workshop, as before create a new workshop session from the training portal. When you now modify the local copy of any pages making up the workshop instructions, the view of the instructions will be automatically regenerated and the browser page refreshed to show the latest version, without needing to republish the workshop content.

Once you have tested the local changes and are happy with them, you can
interrupt and exit the ``educates cluster workshop serve`` process using
``<CTRL-C>``. This will cause the workshop definition in the cluster to be
restored and the published version of the workshop once more used. Then
republish the workshop using ``educates workshop publish`` and the updated
content will then be used for subsequent workshop sessions created from the
training portal.

Note that the patched workshop configuration created when using the `--patch-workshop` option will only source workshop instructions from the local machine. If you have other workshop files such as setup scripts or code files to be used in exercises, it will still be necessary to go through the process of republishing the workshop content and creating a new workshop session so the changes are picked up. 

Modifying workshop definition
-----------------------------

If you need to modify the workshop definition found in the
``resources/workshop.yaml`` file and want to test those changes, you need to
update the workshop definition in the Kubernetes cluster. To do this run the
following command, or its alias ``educates update-workshop``:

```
educates cluster workshop update
```

When this is done, if the changes would affect the workshop environment or what is setup for each workshop session, the old workshop environment will be shutdown and a new workshop environment started.

Once the update to the workshop definition has been triggered, if you are currently in a workshop session, end that session. Now create a new workshop session from the training portal.

Deleting the deployment
-----------------------

If you have stopped working on the workshop and wish to delete it from the
Kubernetes cluster to preserve resources, you can run the following command,
or its alias ``educates delete-workshop``:

```
educates cluster workshop delete
```
