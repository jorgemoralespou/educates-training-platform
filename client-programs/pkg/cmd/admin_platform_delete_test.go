package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestDeleteInventory_NoPurge_OnlyCRsAndRelease(t *testing.T) {
	got := deleteInventory(false)
	wantContains := []string{
		"SessionManager/cluster",
		"LookupService/cluster",
		"SecretsManager/cluster",
		"EducatesClusterConfig/cluster",
		"helm release: educates-installer",
	}
	for _, w := range wantContains {
		var found bool
		for _, line := range got {
			if strings.Contains(line, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("inventory missing %q in %v", w, got)
		}
	}
	for _, line := range got {
		if strings.Contains(line, "CRD:") || strings.Contains(line, "namespace:") {
			t.Errorf("non-purge inventory leaked purge-only entry: %q", line)
		}
	}
}

func TestDeleteInventory_Purge_AddsCRDsAndNamespaces(t *testing.T) {
	got := deleteInventory(true)
	for _, want := range []string{
		"CRD: educatesclusterconfigs.config.educates.dev",
		"CRD: secretsmanagers.platform.educates.dev",
		"CRD: lookupservices.platform.educates.dev",
		"CRD: sessionmanagers.platform.educates.dev",
		"namespace: educates-installer",
		"namespace: educates-secrets",
	} {
		var found bool
		for _, line := range got {
			if strings.Contains(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("purge inventory missing %q in %v", want, got)
		}
	}
}

func TestConfirmDelete_YesFlag_NoPrompt(t *testing.T) {
	var buf bytes.Buffer
	if err := confirmDelete(&buf, &PlatformDeleteOptions{Yes: true}); err != nil {
		t.Fatalf("confirmDelete with --yes: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("--yes should not print anything, got: %s", buf.String())
	}
}
