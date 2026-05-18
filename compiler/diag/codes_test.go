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

func TestLanguageExpressivenessDiagnosticCodesExist(t *testing.T) {
	codes := []string{
		SEM0076, // generic declaration has duplicate type parameter
		SEM0077, // generic type arity mismatch
		SEM0078, // unknown type parameter or type argument
		SEM0079, // generic or enum type arguments cannot be inferred
		SEM0080, // unsized type used where layout is required
		SEM0081, // missing trait implementation
		SEM0082, // trait method signature mismatch
		SEM0083, // ambiguous or overlapping impl
		SEM0084, // non-exhaustive match
		SEM0085, // impossible enum variant pattern
		SEM0086, // const expression overflow
		SEM0087, // non-const operand in const expression
		SEM0088, // invalid sizeof or alignof operand
		SEM0089, // static assertion failed
		SEM0090, // slot count or reservation size overflow
		SEM0091, // slots or slice lifetime escape
		SEM0092, // protected memory-region view construction is not allowed here
		SEM0093, // raw Slots memory cannot be read directly
		SEM0094, // enum variant constructor is invalid
		SEM0095, // match or if-let pattern binding is invalid
		SEM0096, // protected view field access is not allowed here
	}
	for _, code := range codes {
		if code == "" {
			t.Fatal("empty diagnostic code")
		}
	}
}
