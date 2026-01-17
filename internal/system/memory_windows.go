//go:build windows

package system

import (
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func SystemMemoryMB() int {
	status := memoryStatusEx{}
	status.Length = uint32(unsafe.Sizeof(status))
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	r1, _, _ := proc.Call(uintptr(unsafe.Pointer(&status)))
	if r1 == 0 {
		return 4096
	}
	return int(status.TotalPhys / (1024 * 1024))
}
