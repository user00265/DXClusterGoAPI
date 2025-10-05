package version

import (
	"fmt"
	"strings"
)

// These variables are populated at build time using ldflags.
// Example: go build -ldflags "-X 'github.com/user00265/dxclustergoapi/internal/version.GitCommit=f80cf83' -X 'github.com/user00265/dxclustergoapi/internal/version.BuildVersion=1.0.0'" ...
var (
	// ProjectName is the name of the project.
	ProjectName = "DXClusterGoAPI"

	// ProjectGitHubURL is the GitHub repository URL.
	ProjectGitHubURL = "https://github.com/user00265/dxclustergoapi"

	// BuildVersion represents the semantic version of the build.
	// This should be set via ldflags with a semver tag (e.g., v1.0.0).
	// If not set, it defaults to "unknown".
	BuildVersion = "unknown"

	// GitCommit represents the short Git commit hash.
	// This should be set via ldflags with the git commit hash.
	GitCommit = "unknown"
)

// ProjectVersion constructs the full project version string.
// If BuildVersion is a valid semver and GitCommit is available, it's "vX.Y.Z+COMMIT".
// Otherwise, it's just "unknown".
var ProjectVersion = func() string {
	if BuildVersion != "unknown" && GitCommit != "unknown" {
		// Remove leading 'v' if present, then append commit
		return fmt.Sprintf("%s+%s", strings.TrimPrefix(BuildVersion, "v"), GitCommit[:7])
	}
	return "unknown"
}()

// UserAgent is the full User-Agent string to be used in HTTP requests.
var UserAgent = fmt.Sprintf("%s/%s (+%s)", ProjectName, ProjectVersion, ProjectGitHubURL)

// This init function ensures that UserAgent is constructed AFTER all build-time vars
// are potentially set. If this were a simple global var, it would be initialized
// before ldflags could inject values.
func init() {
	if BuildVersion != "unknown" && GitCommit != "unknown" {
		ProjectVersion = fmt.Sprintf("%s+%s", strings.TrimPrefix(BuildVersion, "v"), GitCommit[:7])
	} else {
		ProjectVersion = "unknown"
	}
	UserAgent = fmt.Sprintf("%s/%s (+%s)", ProjectName, ProjectVersion, ProjectGitHubURL)
}
