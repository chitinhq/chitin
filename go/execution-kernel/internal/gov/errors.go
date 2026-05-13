package gov

import "fmt"

// ErrMissingTarget is returned when a required Target field is missing in normalization.
func ErrMissingTarget(tool string) error {
	return fmt.Errorf("required Target missing for tool: %s", tool)
}
