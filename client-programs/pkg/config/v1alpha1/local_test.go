package v1alpha1

import "testing"

func TestIsDevVersion(t *testing.T) {
	release := []string{"4.0.0", "4.0.0-alpha.1", "v1.2.3", "1.2.3+build.7", "0.0.1-rc.2"}
	for _, v := range release {
		if isDevVersion(v) {
			t.Errorf("isDevVersion(%q) = true, want false (release)", v)
		}
	}
	dev := []string{"latest", "develop", "dev", "main", "sha-abc1234", "4.0", ""}
	for _, v := range dev {
		if !isDevVersion(v) {
			t.Errorf("isDevVersion(%q) = false, want true (dev)", v)
		}
	}
}

func TestApplyCLIDefaults_DevBuild(t *testing.T) {
	c := (&EducatesLocalConfig{
		ImageVersions: []ImageVersion{
			{Name: "session-manager", Image: "example.com/custom/session-manager:hacking"},
		},
	}).ApplyCLIDefaults("latest", "localhost:5001")

	if c.Operator.Image.Repository != "localhost:5001/educates-operator" {
		t.Errorf("operator repository = %q", c.Operator.Image.Repository)
	}
	if c.Operator.Image.Tag != "latest" {
		t.Errorf("operator tag = %q", c.Operator.Image.Tag)
	}
	if c.Operator.Image.PullPolicy != "Always" {
		t.Errorf("operator pullPolicy = %q, want Always in dev mode", c.Operator.Image.PullPolicy)
	}

	if len(c.ImageVersions) != len(LocalDevImageNames) {
		t.Fatalf("imageVersions has %d entries, want %d (one per LocalDevImageNames, user entry deduped)",
			len(c.ImageVersions), len(LocalDevImageNames))
	}
	byName := map[string]string{}
	for _, iv := range c.ImageVersions {
		byName[iv.Name] = iv.Image
	}
	if byName["session-manager"] != "example.com/custom/session-manager:hacking" {
		t.Errorf("user-supplied session-manager override was clobbered: %q", byName["session-manager"])
	}
	if byName["training-portal"] != "localhost:5001/educates-training-portal:latest" {
		t.Errorf("training-portal default = %q", byName["training-portal"])
	}
	if byName["secrets-manager"] != "localhost:5001/educates-secrets-manager:latest" {
		t.Errorf("secrets-manager default = %q", byName["secrets-manager"])
	}
}

func TestApplyCLIDefaults_ReleaseBuild(t *testing.T) {
	c := (&EducatesLocalConfig{}).ApplyCLIDefaults("4.0.0-alpha.1", "ghcr.io/educates")

	if c.Operator.Image.Repository != "ghcr.io/educates/educates-operator" {
		t.Errorf("operator repository = %q", c.Operator.Image.Repository)
	}
	if c.Operator.Image.Tag != "4.0.0-alpha.1" {
		t.Errorf("operator tag = %q", c.Operator.Image.Tag)
	}
	if c.Operator.Image.PullPolicy != "" {
		t.Errorf("operator pullPolicy = %q, want empty (chart auto-derives) for release builds", c.Operator.Image.PullPolicy)
	}
	if len(c.ImageVersions) != 0 {
		t.Errorf("release build appended imageVersions defaults: %v", c.ImageVersions)
	}
}
