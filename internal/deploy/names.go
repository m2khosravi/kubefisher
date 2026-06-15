package deploy

import (
	"fmt"
	"regexp"
	"strings"
)

var slugSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

// ModelSlug converts a HuggingFace model ID to a DNS-1123-safe resource name.
func ModelSlug(model string) string {
	s := strings.ToLower(strings.TrimSpace(model))
	s = strings.ReplaceAll(s, "/", "-")
	s = slugSanitizer.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "model"
	}
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-")
	}
	return s
}

func validateDeployOptions(opts DeployOptions) error {
	if strings.TrimSpace(opts.Model) == "" {
		return fmt.Errorf("--model is required")
	}
	if opts.Replicas < 1 {
		return fmt.Errorf("--replicas must be >= 1")
	}
	return nil
}
