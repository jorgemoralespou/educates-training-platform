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

func TestTTY_ClearsToEOLAndCommitsWithNewline(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 0, true)

	s := r.Start("registry mirror ghcr.io")
	s.Update("a much longer intermediate phase line")
	s.Done("ready")

	out := buf.String()
	// Every TTY rewrite must erase to end of line so a shorter final
	// line can't leave residue from a longer intermediate one (the
	// `rror`/`Cot` garble). No space-padding is used anymore.
	if !strings.Contains(out, "\r✓ registry mirror ghcr.io: ready\033[K\n") {
		t.Errorf("final line must rewrite, clear-to-EOL, then commit a newline:\n%q", out)
	}
	// The intermediate render must also clear to EOL.
	if !strings.Contains(out, "\033[K") {
		t.Errorf("expected erase-to-EOL escape on TTY renders:\n%q", out)
	}
	// Final visible state must end the line with a newline so the next
	// writer starts clean.
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("committed step must end in newline:\n%q", out)
	}
}

func TestTTY_MultibyteSymbolsDoNotMisalign(t *testing.T) {
	// The symbols (→ ✓ ✗) and labels may carry multi-byte runes. The
	// clear-to-EOL approach must not depend on byte-vs-column width, so
	// just assert the rewrite carries the full intended text intact.
	var buf bytes.Buffer
	r := New(&buf, 0, true)
	r.Start("wait SecretsManager/cluster Ready ⏳").Done("Ready 💚")
	out := buf.String()
	if !strings.Contains(out, "✓ wait SecretsManager/cluster Ready ⏳: Ready 💚\033[K\n") {
		t.Errorf("multibyte content must survive the rewrite intact:\n%q", out)
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

func TestStartConcurrent_NonTTY_AppendsEachStateChange(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 0, false)

	steps := r.StartConcurrent("Installing LookupService", "Installing SessionManager")
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	steps[1].Update("Reconciling")
	steps[0].Done("")
	steps[1].Done("")

	out := buf.String()
	if strings.Contains(out, "\r") || strings.Contains(out, "\033[") {
		t.Errorf("non-TTY group must not emit \\r or cursor escapes:\n%q", out)
	}
	for _, want := range []string{
		"→ Installing LookupService",
		"→ Installing SessionManager",
		"→ Installing SessionManager: Reconciling",
		"✓ Installing LookupService",
		"✓ Installing SessionManager",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestStartConcurrent_TTY_RepaintsBlockInPlace(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, 0, true)

	steps := r.StartConcurrent("Installing LookupService", "Installing SessionManager")
	steps[0].Update("Reconciling")
	steps[0].Done("")
	steps[1].Done("")

	out := buf.String()
	// A two-line block repaints by moving the cursor up two lines before
	// redrawing, so both lines animate together rather than scrolling.
	if !strings.Contains(out, "\033[2A") {
		t.Errorf("expected a cursor-up-2 escape for the two-line block repaint:\n%q", out)
	}
	// Each rewrite clears to EOL so a shorter line leaves no residue.
	if !strings.Contains(out, clearEOL) {
		t.Errorf("expected erase-to-EOL escape on TTY repaints:\n%q", out)
	}
	// Both lines must end in their final ✓ state.
	if !strings.Contains(out, "✓ Installing LookupService") {
		t.Errorf("LookupService final state missing:\n%q", out)
	}
	if !strings.Contains(out, "✓ Installing SessionManager") {
		t.Errorf("SessionManager final state missing:\n%q", out)
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
