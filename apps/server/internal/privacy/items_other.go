//go:build !linux && !darwin

package privacy

// Items has nothing to report on a platform Polymux doesn't control.
func Items() []Item { return nil }
