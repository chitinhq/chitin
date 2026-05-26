package queue

import "testing"

func TestValidateReasonKindAutoMergeReasons(t *testing.T) {
	for _, reason := range []string{
		"auto_merge_ci_failed",
		"auto_merge_conflict",
		"auto_merge_ci_timeout",
		"auto_merge_failed",
	} {
		if err := ValidateReasonKind(reason); err != nil {
			t.Errorf("ValidateReasonKind(%q) error = %v", reason, err)
		}
	}
}
