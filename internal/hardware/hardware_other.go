//go:build !darwin

package hardware

// Detect reports unknown hardware on non-macOS platforms.
func Detect() Info {
	return Info{}
}
