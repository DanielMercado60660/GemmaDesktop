package app

import (
	"gemmadesktop.local/app/internal/execution"
	"gemmadesktop.local/app/internal/harness"
	"testing"
)

func TestRegressionDiscoveryRejectsNoTestsAndBuildFailures(t *testing.T) {
	if regressionDiscovery("python", execution.Result{Stderr: "Ran 0 tests"}, nil) {
		t.Fatal("zero Python tests accepted")
	}
	if regressionDiscovery("node", execution.Result{Stdout: "# tests 0"}, nil) {
		t.Fatal("zero Node tests accepted")
	}
	if assertionFailure("python", execution.Result{Stderr: "ERROR: test_missing\nImportError\nRan 1 test"}, nil) {
		t.Fatal("import failure used as defect evidence")
	}
	if assertionFailure("go", execution.Result{Stdout: `{"Action":"fail","Package":"broken"}`}, []string{"TestNew"}) {
		t.Fatal("build failure used as defect evidence")
	}
}

func TestHostVerificationReceiptOrdering(t *testing.T) {
	verify := harness.ToolObservation{Tool: "verify_task", StartSequence: 3, Sequence: 4, Data: Doc{"command": []string{"python3", "-m", "unittest"}, "verificationPassed": true, "readOnly": true, "sourceDigest": "source", "sourceDigestAfter": "source"}}
	command := []string{"python3", "-m", "unittest"}
	if !harnessVerificationAfterEdits(command, []harness.ToolObservation{{Tool: "edit_file", StartSequence: 1, Sequence: 2}, verify}) {
		t.Fatal("fresh host receipt rejected")
	}
	if harnessVerificationAfterEdits(command, []harness.ToolObservation{verify, {Tool: "run_command", StartSequence: 5, Sequence: 6, Error: "failed after write"}}) {
		t.Fatal("stale receipt accepted")
	}
	verify.Data["sourceDigestAfter"] = "changed"
	if harnessVerificationAfterEdits(command, []harness.ToolObservation{verify}) {
		t.Fatal("concurrent edit accepted")
	}
}
