//go:build windows

package main

// Start menu shortcuts (.lnk files) through the Windows Shell's IShellLink COM object, called by hand
// through its method table, so the app needs no other dependency to install itself.

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	ole32                = systemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidShellLink  = comGUID{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW   = comGUID{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile  = comGUID{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	coinitApartment = uintptr(0x2)
	clsctxInproc    = uintptr(0x1)
)

// Method numbers in the interfaces' tables (IUnknown's three come first).
const (
	methodQueryInterface    = 0
	methodRelease           = 2
	linkSetDescription      = 7
	linkSetWorkingDirectory = 9
	linkSetIconLocation     = 17
	linkSetPath             = 20
	persistFileSave         = 6
)

// comCall calls method `index` of the COM object at `object`.
func comCall(object uintptr, index int, args ...uintptr) uintptr {
	table := *(*uintptr)(unsafe.Pointer(object))
	method := *(*uintptr)(unsafe.Pointer(table + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	result, _, _ := syscall.SyscallN(method, append([]uintptr{object}, args...)...)
	return result
}

// createShortcut writes a .lnk at `path` that starts `target`, with its icon and a description.
func createShortcut(path, target, workingDirectory, description string) error {
	procCoInitializeEx.Call(0, coinitApartment) // already initialised is fine
	var link uintptr
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInproc,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link))); hr != 0 {
		return fmt.Errorf("could not make a shortcut (0x%x)", hr)
	}
	defer comCall(link, methodRelease)
	comCall(link, linkSetPath, uintptr(unsafe.Pointer(wide(target))))
	comCall(link, linkSetWorkingDirectory, uintptr(unsafe.Pointer(wide(workingDirectory))))
	comCall(link, linkSetDescription, uintptr(unsafe.Pointer(wide(description))))
	comCall(link, linkSetIconLocation, uintptr(unsafe.Pointer(wide(target))), 0)
	var file uintptr
	if hr := comCall(link, methodQueryInterface, uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&file))); hr != 0 {
		return fmt.Errorf("could not save the shortcut (0x%x)", hr)
	}
	defer comCall(file, methodRelease)
	if hr := comCall(file, persistFileSave, uintptr(unsafe.Pointer(wide(path))), 1); hr != 0 {
		return fmt.Errorf("could not save the shortcut (0x%x)", hr)
	}
	return nil
}
