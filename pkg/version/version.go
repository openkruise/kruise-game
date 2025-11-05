package version

// Version holds the semantic version of the binary.
// The value is overridden at build time using -ldflags.
var Version = "dev"

// GitCommit records the git commit used to build the binary.
// This can also be supplied via -ldflags when available.
var GitCommit = "unknown"
