OpenVEX documents
=================

This directory holds [OpenVEX](https://github.com/openvex/spec) documents
recording vulnerability assessments for the Educates container images. Each
document states, for one CVE, whether Educates is actually affected and why.

Every file matching `vex/*.openvex.json` is picked up automatically:

* Locally, the scan script
  (`.claude/skills/educates-vulnerability-triage/scripts/scan-images.sh`)
  passes each document to Trivy via `--vex`, so a `not_affected` or `fixed`
  statement removes the finding from the SARIF report and summary table.
* In CI, `.github/actions/chainloop-attest` passes the same documents to
  the Trivy scan whose SARIF report feeds the Chainloop policy gate, so a
  statement here stops the gate failing on that CVE.
* In CI, all documents are also merged into a single OpenVEX document and
  attached to each published image as a cosign attestation
  (`cosign attest --type openvex`), so downstream consumers get the same
  suppression with `trivy image --vex oci`.

Authoring a document
--------------------

One file per CVE, named `CVE-YYYY-NNNNN.openvex.json`. Template:

```json
{
  "@context": "https://openvex.dev/ns/v0.2.0",
  "@id": "https://github.com/educates/educates-training-platform/vex/CVE-YYYY-NNNNN",
  "author": "Educates maintainers",
  "timestamp": "2026-07-23T00:00:00Z",
  "version": 1,
  "statements": [
    {
      "vulnerability": {"name": "CVE-YYYY-NNNNN"},
      "products": [
        {"@id": "pkg:golang/stdlib"}
      ],
      "status": "not_affected",
      "justification": "vulnerable_code_not_in_execute_path",
      "impact_statement": "Explain concretely why the vulnerable code cannot be reached or exploited in Educates."
    }
  ]
}
```

Bump `version` and update `timestamp` whenever an existing document is
edited.

Product matching
----------------

Trivy matches statements against findings by package URL (PURL). Guidance:

* Prefer the **package PURL without a version** as the product, for example
  `pkg:golang/stdlib` or `pkg:deb/debian/libfoo`. A version-less PURL
  matches all versions of the package, and is registry independent, so the
  same statement applies to `localhost:5001` builds and published
  `ghcr.io` images alike. Copy the exact PURL from the SARIF report or from
  `trivy image --format json` output.
* Only scope a statement to a single image when the assessment genuinely
  differs between images. Use the image as the product and the package as a
  subcomponent, and omit `repository_url` so the statement matches the
  image name regardless of registry:

  ```json
  "products": [
    {
      "@id": "pkg:oci/educates-session-manager",
      "subcomponents": [{"@id": "pkg:golang/stdlib"}]
    }
  ]
  ```

Statuses and justifications
---------------------------

Trivy filters findings for statements with status `not_affected` or
`fixed`. Every `not_affected` statement must carry:

* a `justification` — one of the OpenVEX machine-readable values:
  `component_not_present`, `vulnerable_code_not_present`,
  `vulnerable_code_not_in_execute_path`,
  `vulnerable_code_cannot_be_controlled_by_adversary`,
  `inline_mitigations_already_exist`;
* an `impact_statement` — a human-readable explanation of why the
  justification holds, specific enough to be reviewed in the pull request
  like code. "Not exploitable" is not an explanation.

Verifying a statement works
---------------------------

Re-scan an affected image and confirm the finding is gone from the summary
table:

```bash
bash .claude/skills/educates-vulnerability-triage/scripts/scan-images.sh session-manager
```

Then confirm Trivy filtered it for the stated reason rather than the CVE
simply disappearing from the database:

```bash
trivy image --vex vex/CVE-YYYY-NNNNN.openvex.json --show-suppressed \
  localhost:5001/educates-session-manager:latest
```

The finding must appear in the suppressed section with the statement's
status and justification.
