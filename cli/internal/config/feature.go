package config

import "errors"

// ErrRefinementDisabled is returned when an operation requires prompt refinement
// but the features.refinement flag is off in the active configuration.
var ErrRefinementDisabled = errors.New(
	"restruct: refinement is not yet enabled in this release — set features.refinement: true in config.yaml to opt in",
)

// GuardRefinement returns ErrRefinementDisabled if the refinement feature flag is off.
// Callers should print the error and exit non-zero.
func GuardRefinement(cfg *Config) error {
	if cfg == nil || !cfg.RefinementEnabled() {
		return ErrRefinementDisabled
	}
	return nil
}
