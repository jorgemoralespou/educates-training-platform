package progress

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNonTTY_AppendsEveryStateChange(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 3, false)

	s1 := r.Start("ECC apply")
	s1.Done("Ready")

	s2 := r.Start("SecretsManager apply")
	s2.Update("Installing")
	s2.Update("Validating")
	s2.Done("Ready")

	out := buf.String()
	// Every state change should be its own line (no \r mid-stream).
	if strings.Contains(out, "\r") {
		t.Errorf("non-TTY mode should not emit \\r:\n%q", out)
	}
	for _, want := range []string{
		"[1/3] → ECC apply",
		"[1/3] ✓ ECC apply: Ready",
		"[2/3] → SecretsManager apply",
		"[2/3] → SecretsManager apply: Installing",
		"[2/3] → SecretsManager apply: Validating",
		"[2/3] ✓ SecretsManager apply: Ready",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestTTY_OverwritesWithCarriageReturn(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 2, true)

	s := r.Start("ECC apply")
	s.Update("Installing")
	s.Done("Ready")

	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Errorf("TTY mode should emit \\r for in-place updates:\n%q", out)
	}
	// Final state should still be present.
	if !strings.Contains(out, "✓ ECC apply: Ready") {
		t.Errorf("final state missing:\n%s", out)
	}
}

func TestNoCounter_WhenTotalZero(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 0, false)
	r.Start("LookupService delete").Done("gone")
	out := buf.String()
	if strings.Contains(out, "[1/0]") || strings.Contains(out, "[1/") {
		t.Errorf("total=0 should hide counter:\n%s", out)
	}
	if !strings.Contains(out, "✓ LookupService delete: gone") {
		t.Errorf("expected counter-less final line:\n%s", out)
	}
}

func TestFail_RendersErrorMessage(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 1, false)
	r.Start("ECC apply").Fail(errors.New("boom"))
	if !strings.Contains(buf.String(), "✗ ECC apply: boom") {
		t.Errorf("fail line missing:\n%s", buf.String())
	}
}

func TestNote_HasNoCounter(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 5, false)
	r.Note("syncing cached local secrets")
	out := buf.String()
	if strings.Contains(out, "[1/5]") {
		t.Errorf("Note should not advance the step counter:\n%s", out)
	}
	if !strings.Contains(out, "· syncing cached local secrets") {
		t.Errorf("note prefix missing:\n%s", out)
	}
}
