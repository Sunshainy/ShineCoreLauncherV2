//go:build !windows

package system

func SystemMemoryMB() int {
	return 4096
}
