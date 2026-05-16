package diag

import "testing"

func TestExecutorTopicDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		SEM0033, // duplicate graph identity label
		SEM0034, // executor slot is unbound, rebound, or unplaced
		SEM0035, // executor slot mismatch
		SEM0036, // invalid vCPU start or enter
		SEM0037, // vCPU overcommit or insufficient target vCPUs
		SEM0038, // path shared across executors
		SEM0039, // topic publisher authority violation
		SEM0040, // topic subscription authority violation
		SEM0041, // topic delivery policy mismatch
		SEM0042, // old executor on-interrupt syntax is not supported
		SEM0043, // interrupt topic route is missing or ambiguous
		SEM0044, // sleeping loop has no wake source
		SEM0045, // reliable topic publish requires explicit backpressure handling
		SEM0046, // topic depth or cache-line layout is invalid
		SEM0047, // executor memory must be owned by executor slot
		SEM0048, // path identity required for publishing path
	}
	for _, code := range codes {
		if code == "" {
			t.Fatal("empty diagnostic code")
		}
	}
}
