// Package version holds build-time version information for the bot.
package version

// Set at build time via -ldflags, with fallbacks for local dev builds.
var (
	Version = "dev"
	Commit  = "none"
)

// String returns the version for display.
func String() string {
	if Version != "dev" {
		return Version
	}
	if Commit != "none" {
		return Commit
	}
	return Version
}
