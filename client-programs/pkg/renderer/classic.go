package renderer

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

// classicRendererRemoved is the message shown for a workshop whose
// instructions use the classic renderer. A session shows the same words.
const classicRendererRemoved = "This workshop's instructions use the classic renderer, which was removed in Educates 4.0. Convert them to the Hugo renderer as described at https://docs.educates.dev/en/latest/workshop-migration/classic-renderer.html."

// classicMarkers are the files whose presence in a workshop's workshop
// directory selected the classic renderer.
var classicMarkers = []string{"workshop.yaml", "modules.yaml"}

// RejectClassicWorkshop returns an error carrying the removal message when
// the workshop at workshopRoot is a classic workshop, one whose workshop
// directory holds workshop.yaml or modules.yaml.
func RejectClassicWorkshop(workshopRoot string) error {
	for _, marker := range classicMarkers {
		if _, err := os.Stat(filepath.Join(workshopRoot, "workshop", marker)); err == nil {
			return errors.New(classicRendererRemoved)
		}
	}

	return nil
}
