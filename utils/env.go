package utils

import (
	"os"
	"strings"
)

// EnvBool returns true if the named environment variable is set to "1" or "true" (case-insensitive).
func EnvBool(name string) bool {
	v := os.Getenv(name)
	return v == "1" || strings.EqualFold(v, "true")
}
