// Package oci holds what the CLI's commands which publish OCI images share:
// a reproducible layer builder and the options for reaching a registry.
//
// The layer builder writes reproducible layers: the same entries, file
// contents and timestamp always give the same bytes, so a republish of
// unchanged content gives the same digests.
package oci
