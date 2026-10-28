(converting-a-classic-workshop)=
Converting a Classic Workshop to Hugo
=====================================

In Educates 4.0, Hugo is the only renderer for workshop instructions. The
classic renderer, which a workshop selected by shipping a
``workshop/workshop.yaml`` or ``workshop/modules.yaml`` file, was removed. A
session of a workshop still in the classic format shows a page saying so in
place of its instructions, and the ``educates cluster workshop serve`` and
``educates workshop render`` commands fail on it with the same message.

This page goes through each feature of the classic renderer and gives its
Hugo equivalent, or says that there is none and what to use instead. For the
full description of workshop instructions rendered by Hugo, see [Hugo
renderer configuration](hugo-renderer-configuration) and [workshop
instructions](workshop-instructions). A workshop created with
``educates workshop new`` shows the layout of a Hugo workshop for comparison.

Outline of a conversion
-----------------------

Converting a classic workshop takes these steps, each described in a section
below:

1. Rewrite any AsciiDoc pages as Markdown pages.
2. Give each page a ``title`` in its front matter, and set the order of the
   pages.
3. Move data variables, and any workshop file variants, into a
   ``workshop/config.yaml`` file.
4. Replace Liquid and ``%var%`` references to data variables with the
   ``param`` shortcode, declaring a data variable for each ``ENV_`` variable
   used.
5. Move images which sit next to pages into a page bundle or the
   ``workshop/static`` directory.
6. Move embedded HTML and scripts into shortcodes.
7. Delete ``workshop/workshop.yaml``, ``workshop/modules.yaml`` and
   ``workshop/config.js``, and remove ``WORKSHOP_FILE`` and
   ``LOCAL_RENDERER_TYPE`` from ``session.env`` in the ``Workshop``
   definition.

Page lists and ordering
-----------------------

A classic workshop listed its pages in ``workshop/modules.yaml``, giving each
a title under ``name`` and optionally a label for its continue button under
``exit_sign``. The ``workshop/workshop.yaml`` file named the workshop and
listed, under ``modules.activate``, which of those pages were used and in what
order. A page which was not activated was not served.

```yaml
# workshop/modules.yaml
modules:
  00-workshop-overview:
    name: Workshop Overview
    exit_sign: Start Workshop
  01-workshop-instructions:
    name: Workshop Instructions
  99-workshop-summary:
    name: Workshop Summary
    exit_sign: Finish Workshop
```

```yaml
# workshop/workshop.yaml
name: Workshop

modules:
  activate:
  - 00-workshop-overview
  - 01-workshop-instructions
  - 99-workshop-summary
```

A Hugo workshop needs neither file. Every Markdown page under
``workshop/content`` is part of the navigation, ordered by the path name of
each page, or by Hugo's ``weight`` front matter field when any page sets it.
The title of a page goes in the ``title`` field of its front matter:

```markdown
---
title: Workshop Overview
---

This is the initial landing page for your workshop.
```

The classic renderer derived a title from the file name of a page which had
no ``name``. Hugo does not, so a page without a ``title`` shows no heading,
and its path appears in the table of contents.

When the classic workshop activated only some of its pages, or ordered them
differently from their file names, rename the files, set ``weight`` on each
page, or list the pages as the steps of a pathway in
``workshop/config.yaml``:

```yaml
# workshop/config.yaml
pathways:
  default: workshop

  paths:
    workshop:
      title: Workshop
      steps:
      - 00-workshop-overview
      - 01-workshop-instructions
      - 99-workshop-summary
```

Each step is the path of a page relative to ``workshop/content``, without its
extension, the same as the name of a classic module. Only the pages a pathway
lists are part of its navigation.

The ``name`` in ``workshop/workshop.yaml`` was the title shown above the
table of contents. A Hugo workshop shows the ``title`` of the ``Workshop``
definition there instead, or the ``title`` of the pathway when one is in use.

Workshop file variants
----------------------

A classic workshop could ship several workshop files, such as
``workshop/workshop-java.yaml`` and ``workshop/workshop-python.yaml``, each
with its own name, list of pages and data variable values under ``vars``. The
``WORKSHOP_FILE`` environment variable selected one of them, and
``workshop.yaml`` was used when it was not set.

```yaml
# workshop/workshop-python.yaml
name: Python Workshop

modules:
  activate:
  - 00-workshop-overview
  - 01-python-setup
  - 99-workshop-summary

vars:
  LANGUAGE: python
```

```yaml
# Workshop definition
spec:
  session:
    env:
    - name: WORKSHOP_FILE
      value: workshop-python.yaml
```

In a Hugo workshop each variant becomes a pathway in ``workshop/config.yaml``,
with its own ``title``, ``steps`` and ``params``:

```yaml
# workshop/config.yaml
pathways:
  default: java

  paths:
    java:
      title: Java Workshop
      steps:
      - 00-workshop-overview
      - 01-java-setup
      - 99-workshop-summary
      params:
      - name: LANGUAGE
        value: java

    python:
      title: Python Workshop
      steps:
      - 00-workshop-overview
      - 01-python-setup
      - 99-workshop-summary
      params:
      - name: LANGUAGE
        value: python
```

The ``PATHWAY_NAME`` environment variable selects the pathway, and
``pathways.default`` names the pathway used when it is not set:

```yaml
# Workshop definition
spec:
  session:
    env:
    - name: PATHWAY_NAME
      value: python
```

The name of the selected pathway is available as the ``pathway_name`` data
variable, and the ``pathway`` shortcode shows a part of a page only for one
pathway, as shown under [Liquid templating](converting-classic-liquid).

Data variables
--------------

A classic workshop declared its own data variables under ``config.vars`` in
``workshop/modules.yaml``. A Hugo workshop declares them under ``params`` in
``workshop/config.yaml``, with the same ``name``, ``value`` and ``aliases``
fields. This classic declaration:

```yaml
# workshop/modules.yaml
config:
  vars:
  - name: LANGUAGE
    value: java
  - name: REPOSITORY
    value: https://github.com/example/app
    aliases:
    - APP_REPOSITORY
```

becomes:

```yaml
# workshop/config.yaml
params:
- name: LANGUAGE
  value: java
- name: REPOSITORY
  value: https://github.com/example/app
  aliases:
  - APP_REPOSITORY
```

As before, ``value`` is the default, and an environment variable with the
same name as the data variable overrides it. When ``aliases`` is given, the
environment variables it lists are checked instead, in order, and the first
one which is set provides the value. A pathway can also have ``params`` of its
own, which override global entries of the same name when that pathway is
selected.

A page references a data variable with the ``param`` shortcode, so
``{{ REPOSITORY }}`` becomes ``{{< param REPOSITORY >}}``.

(converting-classic-environment-variables)=
Environment variables
---------------------

The classic renderer made every environment variable available as a data
variable named after it with an ``ENV_`` prefix, without any declaration. Hugo
has no such automatic variables. Instead, declare an entry under ``params`` in
``workshop/config.yaml`` named after the environment variable, and it takes
its value from that variable. A classic page which used:

```text
Install version {{ ENV_KUBECTL_VERSION }} of kubectl.
```

needs this entry:

```yaml
# workshop/config.yaml
params:
- name: KUBECTL_VERSION
```

and then uses:

```text
Install version {{< param KUBECTL_VERSION >}} of kubectl.
```

To give the data variable a different name, list the environment variable
under ``aliases``. Naming the entry after the old ``ENV_`` name keeps each
reference the same apart from its delimiters:

```yaml
# workshop/config.yaml
params:
- name: ENV_KUBECTL_VERSION
  aliases:
  - KUBECTL_VERSION
```

```text
Install version {{< param ENV_KUBECTL_VERSION >}} of kubectl.
```

Workshop instructions are rendered when the workshop session starts, after
any ``setup.d`` scripts have run, and a ``params`` entry sees the environment
variables set at that point. These include:

* environment variables set on the workshop container through
  ``session.env`` or ``session.envFrom`` in the ``Workshop`` definition;
* [request parameters](passing-parameters-to-a-session) passed to a session,
  once injected into the workshop container with ``session.envFrom``;
* ``NAME=VALUE`` lines written to the file named by the ``WORKSHOP_ENV``
  environment variable from a ``setup.d`` script, as described in [running
  steps on container start](running-steps-on-container-start).

Environment variables set in ``workshop/profile``, or in a terminal, are not
available.

AsciiDoc pages
--------------

The classic renderer accepted pages written in AsciiDoc, with an ``.adoc``
extension. Hugo renders Markdown pages only, and there is no equivalent, so
each AsciiDoc page has to be rewritten in Markdown with an ``.md`` extension.

Clickable actions in AsciiDoc pages were source blocks with a ``role``:

```text
[source,bash,role=execute]
----
echo "Execute command."
----

[source,bash,role=terminal:execute]
----
command: echo "Execute command."
----
```

In Markdown they are fenced code blocks with the name of the action in place
of the language:

~~~text
```execute
echo "Execute command."
```

```terminal:execute
command: echo "Execute command."
```
~~~

HTML in AsciiDoc passthrough blocks, delimited by ``++++``, moves into a
shortcode, as described under [embedded HTML and
scripts](converting-classic-embedded-html).

(converting-classic-liquid)=
Liquid templating
-----------------

The classic renderer ran every page through the Liquid template engine before
rendering it, so a page could interpolate data variables, render parts of
itself conditionally, and include other files. Hugo does not run Liquid, and
leaves ``{{ name }}`` and ``{% ... %}`` in a page as literal text, so every
such construct has to be replaced.

A data variable reference, including one inside a code block or clickable
action, uses the ``param`` shortcode instead. A classic clickable action such
as:

~~~text
```dashboard:open-url
url: {{ ingress_protocol }}://myapp-{{ session_name }}.{{ ingress_domain }}
```
~~~

becomes:

~~~text
```dashboard:open-url
url: {{< param ingress_protocol >}}://myapp-{{< param session_name >}}.{{< param ingress_domain >}}
```
~~~

A condition on a data variable which a workshop file variant set, such as:

```text
{% if LANGUAGE == 'python' %}
Install the dependencies with `pip install -r requirements.txt`.
{% endif %}
```

becomes the ``pathway`` shortcode, once the variants are pathways:

```text
{{< pathway python >}}
Install the dependencies with `pip install -r requirements.txt`.
{{< /pathway >}}
```

Any other condition, an include, or a Liquid filter has no built in
equivalent and needs a shortcode of your own, placed in the
``workshop/layouts/shortcodes`` directory. For example, a shortcode in
``workshop/layouts/shortcodes/if-param.html`` which shows its content only
when a data variable has a given value:

```text
{{ if eq (index .Site.Params (.Get 0)) (.Get 1) }}
{{- .Inner | markdownify }}
{{ end }}
```

is used in a page as:

```text
{{< if-param LANGUAGE python >}}
Install the dependencies with `pip install -r requirements.txt`.
{{< /if-param >}}
```

Liquid ``{% raw %}`` and ``{% endraw %}`` tags are no longer needed, and show
up as text if left in place, so remove them. Only text which looks like a Hugo
shortcode needs escaping, by writing it as ``{{</* name */>}}``.

The %var% syntax
----------------

The classic renderer also replaced a data variable name surrounded by ``%``
characters with its value. Hugo leaves such text as it is, so replace each
reference with the ``param`` shortcode. A classic page which used:

```text
The session namespace is %session_namespace%.
```

uses instead:

```text
The session namespace is {{< param session_namespace >}}.
```

A reference to an ``ENV_`` variable, such as ``%ENV_KUBECTL_VERSION%``, also
needs a ``params`` entry, as described under [environment
variables](converting-classic-environment-variables).

The config.js file
------------------

A classic workshop could provide a ``workshop/config.js`` file, whose
JavaScript code listed the pages, set their titles and button labels, and set
data variables, including values computed from environment variables. There is
no equivalent, as Hugo runs no code from the workshop when it renders the
instructions. Move each part of the file to its Hugo form:

* page lists, titles and variants to front matter and pathways, as described
  above;
* fixed data variables to ``params`` entries in ``workshop/config.yaml``;
* computed values to an environment variable, written to ``WORKSHOP_ENV`` by a
  ``setup.d`` script, or set in ``session.env`` of the ``Workshop``
  definition, whose values can reference session data variables such as
  ``$(session_name)``. A ``params`` entry then makes the environment variable
  available to the instructions.

For example, this ``workshop/config.js`` computed a URL from two environment
variables:

```javascript
function initialize(workshop) {
    workshop.load_workshop();

    workshop.data_variable('APP_URL',
        `https://app-${process.env.SESSION_NAME}.${process.env.INGRESS_DOMAIN}`);
}

exports.default = initialize;

module.exports = exports.default;
```

The same value comes from an executable ``workshop/setup.d/01-app-url.sh``
script:

```shell
#!/bin/bash

echo "APP_URL=https://app-$SESSION_NAME.$INGRESS_DOMAIN" >> $WORKSHOP_ENV
```

with a ``params`` entry for it in ``workshop/config.yaml``:

```yaml
params:
- name: APP_URL
```

after which a page references it as ``{{< param APP_URL >}}``.

Button labels
-------------

The ``exit_sign`` field of a page in ``workshop/modules.yaml`` set the label
of the button at the bottom of that page. There is no equivalent. The
``educates`` theme labels the button "Continue" on every page but the last,
and "Finish Workshop" on the last page when the session has somewhere to
return to, such as the training portal. That destination still comes from the
``RESTART_URL`` environment variable.

Clickable actions
-----------------

Clickable actions in Markdown pages use the same syntax with Hugo: a fenced
code block named after the action, with a YAML body for the namespaced
actions. Every action the classic renderer supported, including the older
``execute`` and ``copy`` forms, works the same way with Hugo. What changes is
around the actions:

* data variables inside an action use the ``param`` shortcode, as shown under
  [Liquid templating](converting-classic-liquid);
* the marker for copying inline code, ``{{copy}}``, becomes the ``copy``
  shortcode;
* actions in AsciiDoc pages become fenced code blocks, as shown under
  AsciiDoc pages;
* actions triggered from a script embedded in a page need a shortcode, as
  described under [embedded HTML and
  scripts](converting-classic-embedded-html).

For example, inline code marked for copying with:

```text
Text to ``copy``{{copy}}.
```

is marked with the shortcode instead:

```text
Text to ``copy``{{< copy >}}.
```

Images next to pages
--------------------

The classic renderer served the ``workshop/content`` directory as it was, so
an image placed next to a page loaded by its file name alone. Hugo publishes a
page ``workshop/content/name.md`` at the path ``name/``, so the same file name
resolves under the page's own path and the image does not load. A classic
workshop with these files:

```text
workshop/content/01-workshop-instructions.md
workshop/content/architecture.png
```

had the page reference the image as:

```text
![Architecture](architecture.png)
```

Either turn the page into a page bundle, a directory holding the page as
``index.md`` with its images beside it, which keeps the reference unchanged
and the page's path, and so its pathway step, the same:

```text
workshop/content/01-workshop-instructions/index.md
workshop/content/01-workshop-instructions/architecture.png
```

or move the image into the ``workshop/static`` directory and reference it
through the ``baseurl`` shortcode:

```text
workshop/content/01-workshop-instructions.md
workshop/static/images/architecture.png
```

```text
![Architecture]({{< baseurl >}}/images/architecture.png)
```

See [embedding images and static
assets](embedding-images-and-static-assets) for more details.

(converting-classic-embedded-html)=
Embedded HTML and scripts
-------------------------

The classic renderer passed HTML written in a Markdown page through to the
rendered page. Hugo, as Educates runs it, leaves raw HTML out of Markdown
pages, so HTML elements and ``<script>`` elements written in a page disappear.
Move the HTML into a shortcode in the ``workshop/layouts/shortcodes``
directory, and use the shortcode in the page. For example, a classic page
could expose the editor tab with:

```text
<script>
window.addEventListener("load", function() {
    educates.expose_dashboard("Editor");
});
</script>
```

The same script in ``workshop/layouts/shortcodes/expose-editor.html`` is used
in the page as:

```text
{{< expose-editor >}}
```
