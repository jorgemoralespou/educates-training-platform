Workshop Configuration
======================

There are two main components to a workshop. The first is the workshop definition, which defines the setup requirements for deploying the workshop and how to configure the Educates environment for that workshop. The second is the workshop files, consisting of the workshop instructions, setup files for the workshop, and any exercise files required for the workshop.

Workshop setup requirements
---------------------------

Workshop images can be deployed directly to a container runtime. To manage deployments into a Kubernetes cluster, the Educates operator is provided. Configuration for the Educates operator is defined by a ``Workshop`` custom resource definition. When using the workshop template to create the initial files for a workshop, this definition is found in the ``resources/workshop.yaml`` file.

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-markdown-sample
spec:
  title: Markdown Sample
  description: A sample workshop using Markdown
  workshop:
    files:
    - git:
        url: https://github.com/educates/lab-markdown-sample
        ref: origin/main
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
  session:
    namespaces:
      budget: small
    applications:
      console:
        enabled: true
      editor:
        enabled: true
```

In this sample workshop files are downloaded from a Git repository hosted on GitHub. This is specified in the ``workshop.files`` section of the workshop definition. The workshop files will be overlaid on top of the standard workshop base image.

As well as the standard workshop base image, Educates also provides workshop base images for working with Java and Python. To select the workshop base image with Java JDK 17 support you would use:

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-markdown-sample
spec:
  title: Markdown Sample
  description: A sample workshop using Markdown
  workshop:
    image: jdk17-environment:*
    files:
    - git:
        url: https://github.com/educates/lab-markdown-sample
        ref: origin/main
      includePaths:
      - /workshop/**
      - /exercises/**
      - /README.md
  session:
    namespaces:
      budget: small
    applications:
      console:
        enabled: true
      editor:
        enabled: true
```

In this case the alternate workshop image was specified by setting the ``workshop.image`` property.

As well as selecting one of the alternate workshop base images provided by Educates, you could also nominate a custom workshop image of your own:

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-markdown-sample
spec:
  title: Markdown Sample
  description: A sample workshop using Markdown
  workshop:
    image: ghcr.io/educates/lab-markdown-sample:latest
  session:
    namespaces:
      budget: small
    applications:
      console:
        enabled: true
      editor:
        enabled: true
```

When using a custom workshop image it can include the workshop files, or you can still download them separately and overlay them on top.

As well as being used to specify the workshop base image to use and the source of any workshop files, the ``Workshop`` definition is used to configure the workshop environment and workshop session. This includes details such as whether the embedded editor or Kubernetes web console is enabled, but also details such as additional resources to deploy with a workshop environment or session, what memory and storage requirements a workshop session has, or what quota is available when deploying workloads into the Kubernetes cluster.

For more details see the separate documentation on the [Workshop definition](workshop-definition).

Instructions rendering
----------------------

Workshop instructions are rendered with Hugo, which generates them as static
HTML files using custom layouts provided by Educates. Hugo only supports the
use of Markdown. The pages making up the workshop instructions are placed in
the ``workshop/content`` directory.

A workshop whose instructions were written for the classic renderer, which was
removed in Educates 4.0, shows a page saying so in place of its instructions,
and needs to be converted as described in [converting a classic workshop to
Hugo](converting-a-classic-workshop).

(hugo-renderer-configuration)=
Hugo renderer configuration
---------------------------

Specifying the navigation path through the workshop instructions explicitly,
in a separate configuration file, is optional.

By default all pages will be included within the navigation path of the
workshop instructions. The order in which pages are included will be
calculated based on the pathname of the pages within the ``workshop/content``
directory. If you would prefer instead to use Hugo's default page ordering
mechanism, you can add page weight fields in the metadata of each page to
dictate ordering. In that case the ordering will be based on page weight
followed by page title. To specify the title for a page, you can add a title
field in the metadata of each page.

If you would prefer to specify the order of pages via a YAML file, only have certain pages included, or have the option of multiple pathways which can be selected, a ``workshop/config.yaml`` file can be provided which defines this information.

If you elect to use the ``workshop/config.yaml``, the structure it would contain where using it to define the navigation pathways would be as follows:

```yaml
pathways:
  default: workshop

  paths:
    workshop:
      title: "Workshop"

      steps:
      - 00-workshop-overview
      - 01-workshop-instructions
      - 99-workshop-summary

  modules:
    00-workshop-overview:
      title: Workshop Overview
    01-workshop-instructions:
      title: Workshop Instructions
    99-workshop-summary:
      title: Workshop Summary
```

The ``pathways.modules`` section is optional, even when specifying the
navigation pathway. Its entries are keyed by the name of a page, and the
``title`` of an entry replaces the title from the front matter of the page in
the page heading and in the table of contents. A page of the pathway without
an entry in ``pathways.modules`` uses the title in its own front matter.

If you want multiple possible pathways, you can list the steps for each in separate sections under ``pathways.paths``. The ``pathways.default`` value should specify the default pathway. You can override the workshop title in the ``title`` field of a specific pathway.

To select which pathway is used when the workshop is started, you can set the ``PATHWAY_NAME`` environment variable in the workshop definition to the name of the pathway.

At the bottom of each page but the last, a "Continue" button takes the user to
the next page. On the last page, a "Finish Workshop" button is shown when the
``RESTART_URL`` environment variable gives the session somewhere to return the
user to. The training portal sets it, so that at the completion of a workshop
the user is directed back to the training portal. The labels of these buttons
cannot be changed.
