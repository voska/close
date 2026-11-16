package policy

// Windows deployment policies are unsupported. Unix filesystem policies have
// no authority on Windows, where ordinary CLI behavior remains unchanged.
func Load() (*Policy, error) { return nil, nil }
