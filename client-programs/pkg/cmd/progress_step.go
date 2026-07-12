package cmd

import (
	"os"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
)

// runStep brackets a single operation as one progress step: it opens the
// step on rep, hands the Step to fn (so the callee can surface
// intermediate detail via Update), and closes it Done(summary) on
// success or Fail(err) on error. The step's label morphs in place on a
// TTY, so fn must not write to the same stream itself — callees report
// through the passed Step instead.
func runStep(rep progress.Reporter, label, summary string, fn func(progress.Step) error) error {
	step := rep.Start(label)
	if err := fn(step); err != nil {
		step.Fail(err)
		return err
	}
	step.Done(summary)
	return nil
}

// stepOnStdout brackets a single operation against a fresh stdout
// reporter — the standalone registry/mirror commands that render one
// operation and exit. verbose turns off in-place line morphing so each
// sub-operation detail line is committed rather than overwritten.
func stepOnStdout(verbose bool, label, summary string, fn func(progress.Step) error) error {
	rep := progress.New(os.Stdout, 0, isStdoutTTY(os.Stdout) && !verbose)
	return runStep(rep, label, summary, fn)
}
