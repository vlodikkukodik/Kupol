// Package version хранит версию сборки; значения подставляются через -ldflags.
package version

var (
	Version = "dev"
	Commit  = "unknown"
)
