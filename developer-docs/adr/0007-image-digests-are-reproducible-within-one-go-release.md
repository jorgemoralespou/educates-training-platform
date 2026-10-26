Image digests are reproducible within one Go release
====================================================

``educates package publish`` and ``educates workshop publish`` build each
image layer as a tar whose bytes depend only on the files published: modes,
owners, timestamps and order are fixed. The registry identifies a layer by
the digest of its compressed bytes, though, and the CLI compresses with the
gzip encoder of the Go standard library, through go-containerregistry. Go
does not keep that encoder's output the same from one release to the next,
and Go 1.27 changed it, so a CLI built with a different Go release can give
different layer, image and index digests for the same files. Two publishes
of one source give the same digests when the CLI that runs them was built
with one Go release. This narrows the reproducibility that
[0003](0003-the-extension-package-image-contract.md) relies on to keep
references by tag safe.

A tag still names the same files. Republishing unchanged content with a CLI
built with a newer Go release moves the tag to an image with the same
content, and a workshop session receives what it received before. Only the
digest changes, which a pipeline comparing digests to tell whether anything
changed sees as a change. Unchanged content is rarely republished with a
different CLI: pipelines publish a workshop or a package when its content
changes, and the case arises mostly while developing, where a CLI built
locally may use a newer Go release than a released one.

The golden-digest test in ``client-programs/pkg/packages/build_golden_test.go``
records the package digests for each Go release the CLI is built with, and
fails on a Go release it holds no digests for. Upgrading Go records the new
release's digests, and the release notes of the CLI version that ships the
upgrade say that image digests change.

Considered options
------------------

Compressing with an encoder whose version ``go.mod`` fixes, such as the gzip
encoder of ``github.com/klauspost/compress``, was rejected. Its output was
the same under Go 1.26 and 1.27, on both ``amd64`` and ``arm64``, so digests
would move only when that module is upgraded. It promises no more stability
across its own versions than Go does, though, and is upgraded whenever
another module requires a newer version, so digests would still move, on a
schedule set by the module graph. The case it covers, unchanged content
republished with a different CLI, is too rare to justify owning the
compression step instead of leaving it to go-containerregistry.

Publishing uncompressed layers was rejected. The layer digest would be the
digest of the tar, which no Go release changes, but every push and pull
would carry the full size of the files, and every consumer of workshop
images, including the vendir in older base images, would need checking
against uncompressed layers.
