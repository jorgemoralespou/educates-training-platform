package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/moby/moby/client"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// resolveImagePackageDelivery settles how the extension packages declared as
// an image reach this deploy and prepares them, returning the compose volumes
// to add and the fetcher entries to write.
//
// It probes the daemon, applies the image pull policy for a mounted package,
// and reports what it chose. A workshop declaring no package images does none
// of this and says nothing.
func resolveImagePackageDelivery(
	ctx context.Context,
	cli *client.Client,
	workshop *unstructured.Unstructured,
	delivery dockerPackageDelivery,
	name string,
	localRepository string,
	version string,
	stdout io.Writer,
) (splitPackages, error) {
	// Which delivery is chosen does not change which packages are declared,
	// so the declaration is read once to find out whether there is anything
	// to do at all.
	declared, err := splitImagePackages(workshop, false, localRepository, name, version)

	if err != nil {
		return splitPackages{}, err
	}

	if len(declared.Names) == 0 {
		return splitPackages{}, nil
	}

	capabilities := daemonCapabilities{}

	// A forced fetch never consults the daemon, so a deploy which is not
	// going to mount does not pay for the probe or fail on it.
	if delivery != dockerPackageDeliveryFetch {
		capabilities = probeDaemonCapabilities(ctx, cli)
	}

	podman := strings.Contains(strings.ToLower(capabilities.ServerVersion), "podman") ||
		isPodmanDaemon(ctx, cli)

	// Podman does not pull the image behind a volume, so mounting is only
	// possible for an image which is already local.
	podmanImageAbsent := false

	if podman && delivery != dockerPackageDeliveryFetch {
		for _, entry := range declared.Fetches {
			if !imagePresentLocally(ctx, cli, entry.Image) {
				podmanImageAbsent = true
				break
			}
		}
	}

	mounting, message, err := resolveDockerPackageDelivery(delivery, capabilities, podmanImageAbsent)

	if err != nil {
		return splitPackages{}, err
	}

	packages, err := splitImagePackages(workshop, mounting, localRepository, name, version)

	if err != nil {
		return splitPackages{}, err
	}

	// A mounted package is pulled by Compose when it is missing, so the pull
	// policy only needs acting on where it asks for something else.
	if mounting {
		if err := applyImagePullPolicies(ctx, cli, workshop, packages, localRepository, name, version, stdout); err != nil {
			return splitPackages{}, err
		}
	}

	fmt.Fprintln(stdout, message)

	for _, line := range describePackageDelivery(packages.Names, mounting) {
		fmt.Fprintln(stdout, line)
	}

	return packages, nil
}

// applyImagePullPolicies honours imagePullPolicy for packages which are being
// mounted. Compose pulls a missing volume image itself, but only when it is
// missing, so Always is the case which needs doing here: republishing the
// same tag is the local authoring loop. Never refuses before the deploy
// rather than letting Compose pull.
func applyImagePullPolicies(
	ctx context.Context,
	cli *client.Client,
	workshop *unstructured.Unstructured,
	packages splitPackages,
	localRepository string,
	name string,
	version string,
	stdout io.Writer,
) error {
	policies, err := imagePullPolicies(workshop, localRepository, name, version)

	if err != nil {
		return err
	}

	for _, mount := range packages.Mounts {
		policy := policies[mount.Source]

		switch policy {
		case "Always":
			fmt.Fprintf(stdout, "Pulling extension package image %s\n", mount.Source)

			if err := pullPackageImage(ctx, cli, mount.Source); err != nil {
				return err
			}

		case "Never":
			if !imagePresentLocally(ctx, cli, mount.Source) {
				return errors.Errorf(
					"the extension package image %s is not in the local image store and "+
						"imagePullPolicy is Never", mount.Source,
				)
			}
		}
	}

	return nil
}

// imagePullPolicies maps each package image reference to the policy declared
// beside it, keyed by the expanded reference so a caller holding a compose
// volume can look it up.
func imagePullPolicies(
	workshop *unstructured.Unstructured,
	localRepository string,
	name string,
	version string,
) (map[string]string, error) {
	policies := map[string]string{}

	workshopVersion, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found {
		workshopVersion = version
	}

	packagesItems, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "workshop", "packages")

	if !found {
		return policies, nil
	}

	for _, packagesItem := range packagesItems {
		item, ok := packagesItem.(map[string]interface{})

		if !ok {
			continue
		}

		tmpImage, found := item["image"]

		if !found || tmpImage == nil {
			continue
		}

		packageImage, ok := tmpImage.(string)

		if !ok {
			continue
		}

		tmpPolicy, found := item["imagePullPolicy"]

		if !found || tmpPolicy == nil {
			continue
		}

		policy, ok := tmpPolicy.(string)

		if !ok {
			return nil, errors.Errorf(
				"unable to parse extension package, imagePullPolicy %v is not a string", tmpPolicy,
			)
		}

		reference := expandPackageImageTokens(packageImage, localRepository, name, workshopVersion)

		policies[reference] = policy
	}

	return policies, nil
}

// pullPackageImage pulls one package image, reporting a failure for want of
// credentials in a way the author can act on.
func pullPackageImage(ctx context.Context, cli *client.Client, reference string) error {
	response, err := cli.ImagePull(ctx, reference, client.ImagePullOptions{})

	if err != nil {
		if isAuthenticationFailure(err) {
			return errors.New(missingCredentialsMessage(reference, registryHostForReference(reference)))
		}

		return errors.Wrapf(err, "unable to pull the extension package image %s", reference)
	}

	defer response.Close()

	// The body reports the pull's progress and must be drained for the pull
	// to run to completion.
	if _, err := io.Copy(io.Discard, response); err != nil {
		return errors.Wrapf(err, "unable to pull the extension package image %s", reference)
	}

	return nil
}

// isAuthenticationFailure reports whether a pull failed because the daemon
// had no credentials for the registry, which is the one failure with a
// specific remedy.
func isAuthenticationFailure(err error) bool {
	message := strings.ToLower(err.Error())

	for _, marker := range []string{"unauthorized", "authentication required", "denied", "forbidden"} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

// imagePresentLocally reports whether an image is already in the daemon's
// image store.
func imagePresentLocally(ctx context.Context, cli *client.Client, reference string) bool {
	_, err := cli.ImageInspect(ctx, reference)

	return err == nil
}

// isPodmanDaemon reports whether the daemon behind the Docker API is Podman,
// which announces itself in the components the version endpoint lists.
func isPodmanDaemon(ctx context.Context, cli *client.Client) bool {
	version, err := cli.ServerVersion(ctx, client.ServerVersionOptions{})

	if err != nil {
		return false
	}

	if strings.Contains(strings.ToLower(version.Platform.Name), "podman") {
		return true
	}

	for _, component := range version.Components {
		if strings.Contains(strings.ToLower(component.Name), "podman") {
			return true
		}
	}

	return false
}
