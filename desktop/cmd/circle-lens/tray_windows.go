//go:build windows

package main

// The tray icon and its menu. The menu is native, so it looks like Windows 11's own (rounded, shadowed,
// keyboard-friendly) and follows light or dark mode. It has an icon on the lead item, radio bullets for
// one-of choices, a bold default action, and a line that says what the lens is doing right now.

import (
	"fmt"
	"image/color"
	"syscall"
	"unsafe"

	"circlelens/internal/icon"
	"circlelens/internal/shortcut"
)

const (
	commandOpen = 1 + iota
	commandEnabled
	commandStartup
	commandQuit
	commandRecord
	commandFast
	commandRim
	commandInstall
	commandAutoUpdate
	commandUpdate
	commandRecordZoomIn
	commandRecordZoomOut
	commandRecordClose
	commandCloseOff
	commandCloseEsc
	commandResetZoomKeys
	commandGuide
	commandNews
	commandWebsite
	commandShortcut = 500
	commandLoops    = 100
	commandZoom     = 200
	commandSize     = 300
	commandGap      = 400
)

// followSystemTheme lets this app's menus follow Windows' light or dark app mode. Windows has no
// documented switch for Win32 menus; uxtheme's SetPreferredAppMode (ordinal 135) is the one File
// Explorer and Notepad use. Without it, menus stay light.
func followSystemTheme() {
	if uxtheme.Load() != nil {
		return
	}
	if setPreferredAppMode, _, _ := procGetProcAddress.Call(uxtheme.Handle(), 135); setPreferredAppMode != 0 {
		const allowDark = 1
		syscall.SyscallN(setPreferredAppMode, allowDark)
	}
	flushMenuTheme()
}

// flushMenuTheme makes menus pick up a light/dark change (uxtheme's FlushMenuThemes, ordinal 136).
func flushMenuTheme() {
	if uxtheme.Load() != nil {
		return
	}
	if flush, _, _ := procGetProcAddress.Call(uxtheme.Handle(), 136); flush != 0 {
		syscall.SyscallN(flush)
	}
}

/* ---------- the icon ---------- */

// trayStyle is the mark as the tray shows it: a monochrome glyph for the taskbar's light or dark
// theme, like Windows' own tray icons, with the glass filled in while the lens is open, and dimmed and struck through while circle-watching is off.
func (a *application) trayStyle() icon.Style {
	ink := color.NRGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}
	if darkTaskbar() {
		ink = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	style := icon.Style{Ink: ink}
	switch {
	case a.state.IsOpen():
		// Open: the glass filled, light on a dark taskbar and dark on a light one, the arrow cut out of it.
		style.Glass, style.GlassInk = colourNRGBA(brand.switchOn, 0xff), colourNRGBA(brand.onPrimary, 0xff)
		if !darkTaskbar() {
			style.Glass, style.GlassInk = colourNRGBA(brand.track, 0xff), colourNRGBA(brand.primary, 0xff)
		}
	case !a.settings.Enabled:
		style.Paused = true
	}
	return style
}

// iconBitmap draws the mark into a 32-bit bitmap: premultiplied for menus, straight alpha for icons.
func iconBitmap(side int, style icon.Style, premultiplied bool) uintptr {
	picture := icon.Draw(side, style)
	canvas, ok := newDIB(side, side)
	if !ok {
		return 0
	}
	for i := 0; i < side*side; i++ {
		r, g, b, a := picture.Pix[i*4], picture.Pix[i*4+1], picture.Pix[i*4+2], picture.Pix[i*4+3]
		if premultiplied {
			r, g, b = byte(int(r)*int(a)/255), byte(int(g)*int(a)/255), byte(int(b)*int(a)/255)
		}
		canvas.Pixels[i*4], canvas.Pixels[i*4+1], canvas.Pixels[i*4+2], canvas.Pixels[i*4+3] = b, g, r, a
	}
	return canvas.Detach()
}

func makeIcon(side int, style icon.Style) uintptr {
	colour := iconBitmap(side, style, false)
	if colour == 0 {
		return 0
	}
	defer procDeleteObject.Call(colour)
	mask, _, _ := procCreateBitmap.Call(uintptr(side), uintptr(side), 1, 1, 0)
	defer procDeleteObject.Call(mask)
	info := iconInfo{IsIcon: 1, Mask: mask, Color: colour}
	handle, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return handle
}

func smallIconSide() int { return int(max(16, systemMetric(smCxSmIcon))) }

func (a *application) trayData() notifyIconData {
	data := notifyIconData{Wnd: a.window, ID: 1, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: messageTray, Icon: a.trayIcon}
	data.Size = uint32(unsafe.Sizeof(data))
	tip := fmt.Sprintf("Circle Lens: draw %s to magnify", loopsLabel(a.settings.Loops))
	if !a.settings.Enabled {
		tip = "Circle Lens: paused (" + a.shortcut.String() + " still opens it)"
	}
	copy(data.Tip[:len(data.Tip)-1], wideSlice(tip))
	return data
}

func (a *application) addTrayIcon() {
	if a.trayIcon == 0 {
		a.trayIcon = makeIcon(smallIconSide(), a.trayStyle())
	}
	data := a.trayData()
	procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
}

func (a *application) updateTrayIcon() {
	previous := a.trayIcon
	a.trayIcon = makeIcon(smallIconSide(), a.trayStyle())
	data := a.trayData()
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&data)))
	if previous != 0 {
		procDestroyIcon.Call(previous)
	}
}

func (a *application) removeTrayIcon() {
	data := a.trayData()
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	if a.trayIcon != 0 {
		procDestroyIcon.Call(a.trayIcon)
	}
}

// welcome shows a one-time notification saying how to use the lens.
func (a *application) welcome() {
	a.notify("Circle Lens is ready", fmt.Sprintf("Draw %s anywhere with the mouse to magnify, or press %s. Do it again to close. Settings are in this icon's menu.", loopsLabel(a.settings.Loops), a.shortcut.String()), "")
	a.settings.Welcomed = true
	a.save()
}

/* ---------- the menu ---------- */

// menu builds a native popup menu; bitmaps it was given are freed with it.
type menu struct {
	handle  uintptr
	bitmaps []uintptr
	subs    []*menu
}

func newMenu() *menu {
	handle, _, _ := procCreatePopupMenu.Call()
	return &menu{handle: handle}
}

func (m *menu) insert(info menuItemInfo) {
	info.Size = uint32(unsafe.Sizeof(info))
	count, _, _ := procGetMenuItemCount.Call(m.handle)
	procInsertMenuItem.Call(m.handle, count, 1, uintptr(unsafe.Pointer(&info)))
}

// item adds a command. state: mfsChecked, mfsDisabled, mfsDefault (bold).
func (m *menu) item(id int, text string, state uint32) {
	m.insert(menuItemInfo{Mask: miimFType | miimState | miimID | miimString, Type: mftString, State: state, ID: uint32(id), TypeData: wide(text)})
}

// choice adds one of a set of options, marked with a radio bullet when chosen.
func (m *menu) choice(id int, text string, chosen bool) {
	var state uint32
	if chosen {
		state = mfsChecked
	}
	m.insert(menuItemInfo{Mask: miimFType | miimState | miimID | miimString, Type: mftString | mftRadioCheck, State: state, ID: uint32(id), TypeData: wide(text)})
}

// withIcon adds a command with an icon beside it.
func (m *menu) withIcon(id int, text string, state uint32, bitmap uintptr) {
	if bitmap != 0 {
		m.bitmaps = append(m.bitmaps, bitmap)
	}
	m.insert(menuItemInfo{Mask: miimFType | miimState | miimID | miimString | miimBitmap, Type: mftString, State: state, ID: uint32(id), TypeData: wide(text), Bitmap: bitmap})
}

func (m *menu) separator() { m.insert(menuItemInfo{Mask: miimFType, Type: mftSeparator}) }

func (m *menu) submenu(text string, fill func(*menu)) {
	sub := newMenu()
	fill(sub)
	m.subs = append(m.subs, sub)
	m.insert(menuItemInfo{Mask: miimFType | miimSubMenu | miimString, Type: mftString, SubMenu: sub.handle, TypeData: wide(text)})
}

// destroy frees the menu (and its submenus, which Windows destroys with it) and the bitmaps.
func (m *menu) destroy() {
	procDestroyMenu.Call(m.handle)
	m.freeBitmaps()
}

func (m *menu) freeBitmaps() {
	for _, bitmap := range m.bitmaps {
		procDeleteObject.Call(bitmap)
	}
	for _, sub := range m.subs {
		sub.freeBitmaps()
	}
}

func (a *application) showMenu() {
	m := newMenu()
	defer m.destroy()

	menuInk := color.NRGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}
	if darkMode() {
		menuInk = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	m.withIcon(commandOpen, "Open the lens\t"+a.shortcut.String(), mfsDefault, iconBitmap(smallIconSide(), icon.Style{Ink: menuInk}, true))
	status := fmt.Sprintf("Draw %s anywhere to open it", loopsLabel(a.settings.Loops))
	if !a.settings.Enabled {
		status = "Not watching for circles"
	}
	m.item(0, status, mfsDisabled)
	m.separator()
	m.item(commandEnabled, "Watch for circles", checkedIf(a.settings.Enabled))
	m.submenu("Circles to draw", func(sub *menu) {
		for i, choice := range loopChoices {
			label := choice.label
			switch choice.loops {
			case 1:
				label += "\tquickest, may open by accident"
			case 2:
				label += "\tdefault"
			case 3:
				label += "\tfewest accidents"
			}
			sub.choice(commandLoops+i, label, choice.loops == a.settings.Loops)
		}
	})
	m.submenu("Starting zoom", func(sub *menu) {
		for i, zoom := range zoomChoices {
			label := fmt.Sprintf("%.1f×", zoom)
			if zoom == 1.8 {
				label += "\tdefault"
			}
			sub.choice(commandZoom+i, label, zoom == a.settings.Zoom)
		}
	})
	m.submenu("Wait between open and close", func(sub *menu) {
		for i, gap := range gapChoices {
			label := trimZoom(gap) + " s"
			if gap == 1.5 {
				label += "\tdefault"
			}
			sub.choice(commandGap+i, label, gap == a.settings.Gap)
		}
	})
	m.submenu("Lens size", func(sub *menu) {
		for i, choice := range sizeChoices {
			label := fmt.Sprintf("%s\t%.0f px", choice.label, choice.radius*2)
			sub.choice(commandSize+i, label, choice.radius == a.settings.Radius)
		}
	})
	m.separator()
	m.submenu("Shortcut", func(sub *menu) {
		current := a.shortcut.String()
		listed := false
		for i, preset := range shortcut.Presets {
			sub.choice(commandShortcut+i, preset, preset == current)
			listed = listed || preset == current
		}
		if !listed {
			sub.choice(commandShortcut+len(shortcut.Presets), current, true)
		}
		sub.separator()
		sub.item(commandRecord, "Record your own…", 0)
	})
	m.submenu("Keys while the lens is open", func(sub *menu) {
		sub.item(commandRecordZoomIn, "Zoom in\t"+a.settings.ZoomInKeys, 0)
		sub.item(commandRecordZoomOut, "Zoom out\t"+a.settings.ZoomOutKeys, 0)
		sub.item(commandResetZoomKeys, "Reset to Ctrl + = and Ctrl + −", 0)
		sub.separator()
		sub.choice(commandCloseOff, "No close key", a.settings.CloseKey == "")
		sub.choice(commandCloseEsc, "Esc closes it", a.settings.CloseKey == shortcut.Esc)
		if a.settings.CloseKey != "" && a.settings.CloseKey != shortcut.Esc {
			sub.choice(commandRecordClose, a.settings.CloseKey+" closes it", true)
		}
		sub.item(commandRecordClose, "Record a close key…", 0)
	})
	m.item(commandFast, "Zoom out when moving fast", checkedIf(a.settings.ZoomOutWhenFast))
	m.item(commandRim, "Convex rim", checkedIf(a.settings.ConvexRim))
	m.item(commandStartup, "Start with Windows", checkedIf(startsWithWindows()))
	if !runningInstalled() {
		m.item(commandInstall, "Install Circle Lens…", 0)
	}
	if updatesSupported() {
		m.separator()
		m.item(commandAutoUpdate, "Update automatically", checkedIf(a.settings.AutoUpdate))
		label, clickable := a.updateLabel()
		state := uint32(mfsDisabled)
		if clickable {
			state = 0
		}
		m.item(commandUpdate, label+"\tv"+version, state)
	}
	m.separator()
	m.item(commandGuide, "How to use Circle Lens", 0)
	m.item(commandNews, "What's new in "+version, 0)
	m.item(commandWebsite, "Circle Lens website", 0)
	m.separator()
	m.item(commandQuit, "Quit Circle Lens", 0)

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWindow.Call(a.window) // or the menu would not close when clicking elsewhere
	command, _, _ := procTrackPopupMenu.Call(m.handle, tpmRightButton|tpmReturnCmd, uintptr(cursor.X), uintptr(cursor.Y), 0, a.window, 0)
	procPostMessage.Call(a.window, 0, 0, 0)
	a.runCommand(int(command))
}

// websiteAddress is the project's page.
const websiteAddress = "https://fauzul.com/circle-lens"

// openAddress opens a web address in the default browser.
func openAddress(address string) {
	procShellExecute.Call(0, uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(address))), 0, 0, swShowNormal)
}

func checkedIf(on bool) uint32 {
	if on {
		return mfsChecked
	}
	return 0
}

func (a *application) runCommand(command int) {
	switch {
	case command == commandOpen:
		a.open()
	case command == commandEnabled:
		a.settings.Enabled = !a.settings.Enabled
		a.save()
	case command == commandInstall:
		if err := install(true, nil); err != nil {
			tell("Could not install "+displayName()+": "+err.Error(), mbIconError)
			break
		}
		startInstalled() // takes over from this copy
	case command == commandAutoUpdate:
		a.settings.AutoUpdate = !a.settings.AutoUpdate
		a.save()
		if a.settings.AutoUpdate {
			a.updateTimer() // checks now if a check is due, or installs an update that is waiting
		}
	case command == commandUpdate:
		a.updateCommand()
	case command == commandRecord:
		a.flyout.open()
		a.startRecording()
	case command == commandRecordZoomIn, command == commandRecordZoomOut, command == commandRecordClose:
		a.flyout.open()
		a.startRecordingFor(map[int]string{commandRecordZoomIn: "zoomIn", commandRecordZoomOut: "zoomOut", commandRecordClose: "close"}[command])
	case command == commandCloseOff:
		a.setKeyWhileOpen("close", "")
	case command == commandCloseEsc:
		a.setKeyWhileOpen("close", shortcut.Esc)
	case command == commandResetZoomKeys:
		a.resetZoomKeys()
	case command == commandGuide:
		runGuideWindow()
	case command == commandNews:
		runNewsWindow()
	case command == commandWebsite:
		openAddress(websiteAddress)
	case command == commandFast:
		a.settings.ZoomOutWhenFast = !a.settings.ZoomOutWhenFast
		a.save()
	case command == commandRim:
		a.settings.ConvexRim = !a.settings.ConvexRim
		a.save()
	case command >= commandShortcut && command < commandShortcut+len(shortcut.Presets):
		a.chooseShortcut(shortcut.Presets[command-commandShortcut])
	case command == commandStartup:
		setStartsWithWindows(!startsWithWindows())
	case command == commandQuit:
		a.registerKeys(false)
		procPostQuitMessage.Call(0)
	case command >= commandLoops && command < commandLoops+len(loopChoices):
		a.settings.Loops = loopChoices[command-commandLoops].loops
		a.save()
	case command >= commandZoom && command < commandZoom+len(zoomChoices):
		a.settings.Zoom = zoomChoices[command-commandZoom]
		a.state.SetZoom(a.now(), a.settings.Zoom)
		a.save()
	case command >= commandGap && command < commandGap+len(gapChoices):
		a.settings.Gap = gapChoices[command-commandGap]
		a.save()
	case command >= commandSize && command < commandSize+len(sizeChoices):
		a.settings.Radius = sizeChoices[command-commandSize].radius
		a.save()
	}
}

// systemScale is the primary monitor's DPI scale (1 = 96 DPI).
func systemScale() float64 {
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}
