package pii

import (
	"regexp"
)

var (
	// Regex for standard SSN format: 123-45-6789
	ssnRegex = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	
	// Regex for generic 6+ digits string as requested "like 123456"
	genericSSNRegex = regexp.MustCompile(`\b\d{6,9}\b`)

	// Regex for Email
	emailRegex = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)
)

const (
	MaskedVisualToken  = "*****"
)

// DetectPII returns a slice of all sensitive information found in the input.
func DetectPII(input string) []string {
	var matches []string
	matches = append(matches, emailRegex.FindAllString(input, -1)...)
	matches = append(matches, ssnRegex.FindAllString(input, -1)...)
	matches = append(matches, genericSSNRegex.FindAllString(input, -1)...)
	return matches
}

// MaskPII replaces sensitive information with a visual mask (e.g., *****)
// for the final output displayed to the user.
func MaskPII(input string) string {
	// Mask Emails
	input = emailRegex.ReplaceAllString(input, MaskedVisualToken)
	
	// Mask strict SSN format
	input = ssnRegex.ReplaceAllString(input, MaskedVisualToken)
	
	// Mask generic numbers representing SSN
	input = genericSSNRegex.ReplaceAllString(input, MaskedVisualToken)

	return input
}
