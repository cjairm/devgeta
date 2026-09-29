package embedded

// ExtractFunc is a function type for extracting embedded configs
// This allows dependency injection for testing
type ExtractFunc func(destDir string) error

// DefaultExtractor will be set by main package
var DefaultExtractor ExtractFunc

// ContentStamp returns a short fingerprint of the embedded configs' content,
// or "" when it cannot tell. Set by the main package, like DefaultExtractor.
// The extracted tree's directory name carries it (internal/apps/devgeta
// buildStamp), so two builds with the same version but different configs -
// every local build of one commit with uncommitted changes - never share an
// extract, and the second is never mistaken for already deployed.
var ContentStamp func() string
