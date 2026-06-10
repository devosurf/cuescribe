//go:build darwin

package hardware

import (
	"os/exec"
	"strconv"
	"strings"
)

// Detect reads the chip name and physical memory via sysctl. Fields are left
// zero when a value cannot be read; callers treat that as unknown hardware.
func Detect() Info {
	var info Info
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "machdep.cpu.brand_string", "hw.memsize").Output()
	if err != nil {
		return info
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 {
		info.Chip = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		if bytes, err := strconv.ParseUint(strings.TrimSpace(lines[1]), 10, 64); err == nil {
			info.RAMGB = int(bytes >> 30)
		}
	}
	return info
}
