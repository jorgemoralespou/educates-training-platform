# Build the manager binary.
# Pin the builder to the build host's platform ($BUILDPLATFORM) so Go
# cross-compiles to the target platform via GOOS/GOARCH below, instead of
# running the toolchain under QEMU emulation for non-native target
# architectures during a multiarch build.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build. TARGETOS/TARGETARCH are supplied by the builder for the platform
# being produced; GOOS/GOARCH cross-compile to it from the native builder.
# Build the whole ./cmd package (not just main.go) so additional
# files in the same package — e.g. cmd/logsink.go — are picked up.
# Single-file `go build cmd/main.go` invocations silently miss
# siblings.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
