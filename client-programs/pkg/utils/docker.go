package utils

import (
	"strings"

	"github.com/moby/moby/api/types/container"
)

// GetContainerName returns a container's primary name with the leading
// slash Docker prefixes stripped, or "unknown" when the container has no
// name.
func GetContainerName(c container.Summary) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}

	return "unknown"
}
