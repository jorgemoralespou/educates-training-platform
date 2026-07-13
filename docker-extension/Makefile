# Builds the Educates Docker Desktop extension image. The extension
# image builds FROM the educates-cli image at
# IMAGE_REPOSITORY/educates-cli:PACKAGE_VERSION, so that image must
# already exist for every platform being built — the root
# `make build-docker-extension` target builds and pushes it first with
# the same knobs.
#
# Knobs (env vars or `make VAR=value`), matching the root Makefile:
#   IMAGE_REPOSITORY  registry for images (default localhost:5001)
#   PACKAGE_VERSION   image tag (default latest)
#   TARGET_PLATFORMS  image platforms; DEFAULTS TO THE CURRENT HOST
#                     ARCHITECTURE ONLY. Multi-arch is explicit opt-in:
#                     TARGET_PLATFORMS=linux/amd64,linux/arm64
#   PUSH_IMAGES       false = load into the Docker daemon instead of
#                     pushing (single platform only; default true)

IMAGE_REPOSITORY ?= localhost:5001
PACKAGE_VERSION ?= latest

EXTENSION_IMAGE = $(IMAGE_REPOSITORY)/educates-docker-extension:$(PACKAGE_VERSION)

# Docker Desktop derives a Docker Compose project name from the image
# reference an extension is installed under, and rejects names carrying
# characters like the colon of a registry port. The extension is
# therefore installed under an alias whose repository component is
# sanitized (localhost:5001 becomes localhost_5001); when the repository
# needs no sanitizing the alias is the image itself.
IMAGE_REPOSITORY_ALIAS := $(shell echo $(IMAGE_REPOSITORY) | tr '[:upper:]' '[:lower:]' | sed 's/:/_/g' | sed 's/[^a-z0-9_-]//g' | sed 's/^[^a-z0-9]/a&/')
EXTENSION_IMAGE_ALIAS = $(IMAGE_REPOSITORY_ALIAS)/educates-docker-extension:$(PACKAGE_VERSION)

UNAME_MACHINE := $(shell uname -m)

TARGET_MACHINE = $(UNAME_MACHINE)

ifeq ($(UNAME_MACHINE),x86_64)
TARGET_MACHINE = amd64
endif

BUILDX_BUILDER = educates-multiarch-builder

# Image platforms: current host architecture only, unless TARGET_PLATFORMS
# is set explicitly. Never silently multi-arch — cross-arch builds run
# under QEMU emulation and belong in CI or an explicit opt-in.
ifeq ($(TARGET_PLATFORMS),)
IMAGE_PLATFORMS = linux/$(TARGET_MACHINE)
else
IMAGE_PLATFORMS = $(TARGET_PLATFORMS)
endif

# PUSH_IMAGES=false loads the image into the Docker daemon (`--load`
# semantics, single platform only) instead of pushing to the registry.
ifeq ($(PUSH_IMAGES),false)
DOCKER_BUILDER =
IMAGE_PLATFORMS = linux/$(TARGET_MACHINE)
else
DOCKER_BUILDER = --builder ${BUILDX_BUILDER} --push
endif

INFO_COLOR = \033[0;36m
NO_COLOR   = \033[m
DEV_UI_SOURCE?=http://localhost:3000

print-vars: ## Show the resolved build configuration
	@echo "IMAGE_REPOSITORY:      $(IMAGE_REPOSITORY)"
	@echo "PACKAGE_VERSION:       $(PACKAGE_VERSION)"
	@echo "EXTENSION_IMAGE:       $(EXTENSION_IMAGE)"
	@echo "EXTENSION_IMAGE_ALIAS: $(EXTENSION_IMAGE_ALIAS)"
	@echo "IMAGE_PLATFORMS:       $(IMAGE_PLATFORMS)"
	@echo "DOCKER_BUILDER:        $(DOCKER_BUILDER)"

build-extension: setup-buildx ## Build service image to be deployed as a desktop extension
	docker build --progress plain --platform $(IMAGE_PLATFORMS) \
	    $(DOCKER_BUILDER) \
	    --build-arg REPOSITORY=$(IMAGE_REPOSITORY) --build-arg TAG=$(PACKAGE_VERSION) \
		-t $(EXTENSION_IMAGE) \
		.
ifneq ($(DOCKER_BUILDER),)
	docker pull $(EXTENSION_IMAGE)
endif

# Retag the built image under the sanitized alias the extension is
# installed as. The retag is unconditional so the alias never points at
# a stale digest from an earlier build.
.PHONY: ensure-alias-tag
ensure-alias-tag: build-extension
	@if [ "$(EXTENSION_IMAGE)" != "$(EXTENSION_IMAGE_ALIAS)" ]; then \
		echo "Tagging $(EXTENSION_IMAGE) as $(EXTENSION_IMAGE_ALIAS)"; \
		docker tag $(EXTENSION_IMAGE) $(EXTENSION_IMAGE_ALIAS); \
	fi

install-extension: ensure-alias-tag ## Install the extension
	docker extension install --force $(EXTENSION_IMAGE_ALIAS)

update-extension: ensure-alias-tag ## Update the extension
	docker extension update --force $(EXTENSION_IMAGE_ALIAS)

setup-buildx: ## Create buildx builder for multi-arch build, if not exists
	docker buildx create --name $(BUILDX_BUILDER) --driver docker-container --driver-opt default-load=true --driver-opt network=host --use || true
	docker buildx inspect $(BUILDX_BUILDER) --bootstrap

clean-buildx: ## Remove the buildx builder
	docker buildx rm $(BUILDX_BUILDER) || true

.PHONY: debug
debug: ## Enable debug in the extension
	docker extension dev debug $(EXTENSION_IMAGE_ALIAS)

.PHONY: source
source: ## Replace the UI source of the extension
	docker extension dev ui-source $(EXTENSION_IMAGE_ALIAS) $(DEV_UI_SOURCE)

.PHONY: dev-enable
dev-enable: source debug

.PHONY: dev-reset
dev-reset: ## Reset development status of the extension
	docker extension dev reset $(EXTENSION_IMAGE_ALIAS)

help: ## Show this help
	@echo Please specify a build target. The choices are:
	@grep -E '^[0-9a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "$(INFO_COLOR)%-30s$(NO_COLOR) %s\n", $$1, $$2}'

.PHONY: help
