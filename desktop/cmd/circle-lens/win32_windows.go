//go:build windows

package main

// Thin Win32 bindings, standard library only. DLLs load from the System32 folder by full path, so a
// DLL dropped next to the exe is never picked up instead.

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll") // a KnownDLL, always from System32
	procGetSystemDirectory  = kernel32.NewProc("GetSystemDirectoryW")
	procGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	procGetProcAddress      = kernel32.NewProc("GetProcAddress")
	procCreateMutex         = kernel32.NewProc("CreateMutexW")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
	procGetCurrentProcess   = kernel32.NewProc("GetCurrentProcess")
	procOpenProcess         = kernel32.NewProc("OpenProcess")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")

	procSetProcessWorkingSetSize = kernel32.NewProc("SetProcessWorkingSetSize")

	user32   = systemDLL("user32.dll")
	gdi32    = systemDLL("gdi32.dll")
	shell32  = systemDLL("shell32.dll")
	shcore   = systemDLL("shcore.dll")
	advapi32 = systemDLL("advapi32.dll")
	dwmapi   = systemDLL("dwmapi.dll")
	uxtheme  = systemDLL("uxtheme.dll")

	procRegisterClassEx             = user32.NewProc("RegisterClassExW")
	procCreateWindowEx              = user32.NewProc("CreateWindowExW")
	procDefWindowProc               = user32.NewProc("DefWindowProcW")
	procShowWindow                  = user32.NewProc("ShowWindow")
	procSetTimer                    = user32.NewProc("SetTimer")
	procKillTimer                   = user32.NewProc("KillTimer")
	procLoadCursor                  = user32.NewProc("LoadCursorW")
	procTrackMouseEvent             = user32.NewProc("TrackMouseEvent")
	procGetMonitorInfo              = user32.NewProc("GetMonitorInfoW")
	procFindWindow                  = user32.NewProc("FindWindowW")
	procGetWindowThreadProcessId    = user32.NewProc("GetWindowThreadProcessId")
	procSetWindowPos                = user32.NewProc("SetWindowPos")
	procGetMessage                  = user32.NewProc("GetMessageW")
	procTranslateMessage            = user32.NewProc("TranslateMessage")
	procDispatchMessage             = user32.NewProc("DispatchMessageW")
	procPostMessage                 = user32.NewProc("PostMessageW")
	procPostQuitMessage             = user32.NewProc("PostQuitMessage")
	procPostThreadMessage           = user32.NewProc("PostThreadMessageW")
	procSetWindowsHookEx            = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx         = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx              = user32.NewProc("CallNextHookEx")
	procRegisterHotKey              = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey            = user32.NewProc("UnregisterHotKey")
	procGetCursorPos                = user32.NewProc("GetCursorPos")
	procGetSystemMetrics            = user32.NewProc("GetSystemMetrics")
	procGetAsyncKeyState            = user32.NewProc("GetAsyncKeyState")
	procUpdateLayeredWindow         = user32.NewProc("UpdateLayeredWindow")
	procSetWindowDisplayAff         = user32.NewProc("SetWindowDisplayAffinity")
	procMonitorFromPoint            = user32.NewProc("MonitorFromPoint")
	procCreatePopupMenu             = user32.NewProc("CreatePopupMenu")
	procInsertMenuItem              = user32.NewProc("InsertMenuItemW")
	procGetMenuItemCount            = user32.NewProc("GetMenuItemCount")
	procTrackPopupMenu              = user32.NewProc("TrackPopupMenu")
	procDestroyMenu                 = user32.NewProc("DestroyMenu")
	procSetForegroundWindow         = user32.NewProc("SetForegroundWindow")
	procCreateIconIndirect          = user32.NewProc("CreateIconIndirect")
	procDestroyIcon                 = user32.NewProc("DestroyIcon")
	procRegisterWindowMessage       = user32.NewProc("RegisterWindowMessageW")
	procSystemParametersInfo        = user32.NewProc("SystemParametersInfoW")
	procSetProcessDpiAwareness      = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForSystem             = user32.NewProc("GetDpiForSystem")
	procGetDC                       = user32.NewProc("GetDC")
	procReleaseDC                   = user32.NewProc("ReleaseDC")
	procMessageBox                  = user32.NewProc("MessageBoxW")
	procChangeWindowMessageFilterEx = user32.NewProc("ChangeWindowMessageFilterEx")

	procCreateCompatibleDC   = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection     = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap         = gdi32.NewProc("CreateBitmap")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procDeleteDC             = gdi32.NewProc("DeleteDC")
	procBitBlt               = gdi32.NewProc("BitBlt")
	procCreateFont           = gdi32.NewProc("CreateFontW")
	procSetTextColor         = gdi32.NewProc("SetTextColor")
	procSetBkMode            = gdi32.NewProc("SetBkMode")
	procGetTextExtentPoint32 = gdi32.NewProc("GetTextExtentPoint32W")
	procTextOut              = gdi32.NewProc("TextOutW")
	procGdiFlush             = gdi32.NewProc("GdiFlush")
	procGetTextFace          = gdi32.NewProc("GetTextFaceW")

	procShellNotifyIcon        = shell32.NewProc("Shell_NotifyIconW")
	procShellNotifyIconGetRect = shell32.NewProc("Shell_NotifyIconGetRect")
	procShellExecute           = shell32.NewProc("ShellExecuteW")

	procGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")

	procDwmFlush = dwmapi.NewProc("DwmFlush")

	procRegOpenKeyEx    = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueEx   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValue  = advapi32.NewProc("RegDeleteValueW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procRegCreateKeyEx  = advapi32.NewProc("RegCreateKeyExW")
	procRegDeleteKey    = advapi32.NewProc("RegDeleteKeyW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

func systemDLL(name string) *syscall.LazyDLL {
	buffer := make([]uint16, 260)
	length, _, _ := procGetSystemDirectory.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.NewLazyDLL(syscall.UTF16ToString(buffer[:length]) + `\` + name)
}

const (
	wsPopup = 0x80000000

	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000

	wmDestroy       = 0x0002
	wmActivate      = 0x0006
	wmKeyDown       = 0x0100
	wmKeyUp         = 0x0101
	wmSysKeyDown    = 0x0104
	wmSysKeyUp      = 0x0105
	wmTimer         = 0x0113
	wmMouseLeave    = 0x02A3
	wmQuit          = 0x0012
	wmSettingChange = 0x001A
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmRButtonDown   = 0x0204
	wmRButtonUp     = 0x0205
	wmMButtonDown   = 0x0207
	wmMButtonUp     = 0x0208
	wmMouseWheel    = 0x020A
	wmMouseHWheel   = 0x020E
	wmXButtonDown   = 0x020B
	wmXButtonUp     = 0x020C
	wmHotKey        = 0x0312
	wmApp           = 0x8000

	whMouseLL    = 14
	whKeyboardLL = 13

	swHide           = 0
	swShowNoActivate = 4
	swShow           = 5
	swShowNormal     = 1

	waInactive = 0
	tmeLeave   = 0x2
	idcArrow   = 32512

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010

	ulwAlpha   = 0x2
	acSrcAlpha = 0x1

	wdaExcludeFromCapture = 0x11

	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000

	modAlt      = 0x1
	modControl  = 0x2
	modShift    = 0x4
	modWin      = 0x8
	modNoRepeat = 0x4000

	vkTab      = 0x09
	vkReturn   = 0x0D
	vkShift    = 0x10
	vkSpace    = 0x20
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkControl  = 0x11
	vkEscape   = 0x1B
	vkAdd      = 0x6B
	vkSubtract = 0x6D
	vkOemPlus  = 0xBB
	vkOemMinus = 0xBD

	nimAdd    = 0
	nimModify = 1
	// A click on the tray icon's notification (NIN_BALLOONUSERCLICK, WM_USER + 5).
	ninBalloonUserClick = 0x0405
	nimDelete           = 2
	nifMessage          = 0x01
	nifIcon             = 0x02
	nifTip              = 0x04
	nifInfo             = 0x10

	niifUser      = 0x04
	niifLargeIcon = 0x20

	miimState   = 0x001
	miimID      = 0x002
	miimSubMenu = 0x004
	miimString  = 0x040
	miimBitmap  = 0x080
	miimFType   = 0x100

	mftString     = 0x000
	mftRadioCheck = 0x200
	mftSeparator  = 0x800

	mfsDisabled = 0x0003
	mfsChecked  = 0x0008
	mfsDefault  = 0x1000

	tpmRightButton = 0x2
	tpmReturnCmd   = 0x100

	smCxSmIcon        = 49
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCxVirtualScreen = 78
	smCyVirtualScreen = 79

	monitorDefaultToNearest = 2
	mdtEffectiveDpi         = 0

	spiGetClientAreaAnimation = 0x1042
	spiGetScreenReader        = 0x0046

	errorAlreadyExists = 183

	processSynchronize = 0x00100000 // SYNCHRONIZE: enough to wait for a process to end

	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002
	regSz           = 1
	regDword        = 4

	fwNormal           = 400
	fwMedium           = 500
	fwSemibold         = 600
	clearTypeQuality   = 5
	antialiasedQuality = 4
	transparentBkMode  = 1
	defaultCharset     = 1
	dibRgbColors       = 0
	mbIconError        = 0x10
	mbIconInformation  = 0x40
	createNoWindow     = 0x08000000
	mbYesNo            = 0x04
	mbIconQuestion     = 0x20
	mbIconWarning      = 0x30
	mbDefButton2       = 0x100
	mbSetForeground    = 0x10000
	mbTopmost          = 0x40000
	idYes              = 6
	msgfltAllow        = 1
)

var (
	hwndTopmost              = ^uintptr(0) // (HWND)-1
	dpiAwarenessPerMonitorV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
)

type point struct{ X, Y int32 }

type size struct{ CX, CY int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	Window    uintptr
	HoverTime uint32
}

type notifyIconIdentifier struct {
	Size     uint32
	Wnd      uintptr
	ID       uint32
	GUIDItem [16]byte
}

type message struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type windowClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type mouseHookInfo struct {
	Pt        point
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type blendFunction struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type iconInfo struct {
	IsIcon   int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr
	Color    uintptr
}

type menuItemInfo struct {
	Size            uint32
	Mask            uint32
	Type            uint32
	State           uint32
	ID              uint32
	SubMenu         uintptr
	CheckedBitmap   uintptr
	UncheckedBitmap uintptr
	ItemData        uintptr
	TypeData        *uint16
	Cch             uint32
	Bitmap          uintptr
}

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     uintptr
}

func wide(text string) *uint16 {
	pointer, _ := syscall.UTF16PtrFromString(text)
	return pointer
}

// pointArgument packs a POINT for APIs that take it by value (8 bytes, one register on x64).
func pointArgument(p point) uintptr { return uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32 }

func systemMetric(index int) int32 {
	value, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(value)
}

// dib is a 32-bit top-down DIB section selected into its own memory DC; Pixels is BGRA.
type dib struct {
	dc, bitmap, previous uintptr
	Pixels               []byte
	Width, Height        int
}

func newDIB(width, height int) (*dib, bool) {
	header := bitmapInfoHeader{Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	header.Size = uint32(unsafe.Sizeof(header))
	screen, _, _ := procGetDC.Call(0)
	dc, _, _ := procCreateCompatibleDC.Call(screen)
	procReleaseDC.Call(0, screen)
	var bits unsafe.Pointer
	bitmap, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&header)), dibRgbColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dc == 0 || bitmap == 0 || bits == nil {
		if bitmap != 0 {
			procDeleteObject.Call(bitmap)
		}
		if dc != 0 {
			procDeleteDC.Call(dc)
		}
		return nil, false
	}
	previous, _, _ := procSelectObject.Call(dc, bitmap)
	return &dib{dc: dc, bitmap: bitmap, previous: previous, Pixels: unsafe.Slice((*byte)(bits), width*height*4), Width: width, Height: height}, true
}

// Detach deselects the bitmap and frees the DC, handing the caller the bitmap (for menus and icons).
func (d *dib) Detach() uintptr {
	procSelectObject.Call(d.dc, d.previous)
	procDeleteDC.Call(d.dc)
	return d.bitmap
}

func (d *dib) Close() { procDeleteObject.Call(d.Detach()) }
