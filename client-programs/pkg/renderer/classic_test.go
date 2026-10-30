package renderer

import (
	"os"
	"path/filepath"
	"testing"
)

// classicRemovedMessage is the removal message a session and the CLI show
// for a workshop whose instructions still use the classic renderer.
const classicRemovedMessage = "This workshop's instructions use the classic renderer, which was removed in Educates 4.0. Convert them to the Hugo renderer as described at https://docs.educates.dev/en/latest/workshop-migration/classic-renderer.html."

func TestRejectClassicWorkshop(t *testing.T) {
	tests := []struct {
		name        string
		files       []string
		wantRemoved bool
	}{
		{
			name:        "workshop.yaml marks a classic workshop",
			files:       []string{"resources/workshop.yaml", "workshop/workshop.yaml", "workshop/content/00-workshop-overview.md"},
			wantRemoved: true,
		},
		{
			name:        "modules.yaml marks a classic workshop",
			files:       []string{"resources/workshop.yaml", "workshop/modules.yaml", "workshop/content/00-workshop-overview.md"},
			wantRemoved: true,
		},
		{
			name:        "a Hugo workshop passes",
			files:       []string{"resources/workshop.yaml", "workshop/config.yaml", "workshop/content/00-workshop-overview.md"},
			wantRemoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()

			for _, name := range tt.files {
				path := filepath.Join(root, filepath.FromSlash(name))

				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			err := RejectClassicWorkshop(root)

			if !tt.wantRemoved {
				if err != nil {
					t.Fatalf("RejectClassicWorkshop(%v) = %v, want nil", tt.files, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("RejectClassicWorkshop(%v) = nil, want the removal message", tt.files)
			}

			if err.Error() != classicRemovedMessage {
				t.Errorf("RejectClassicWorkshop(%v) error = %q, want %q", tt.files, err.Error(), classicRemovedMessage)
			}
		})
	}
}
