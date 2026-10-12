//go:build linux

package main

// The tray icon and its menu: a StatusNotifierItem with a com.canonical.dbusmenu menu, over the session
// bus. KDE, Xfce, Cinnamon, MATE, Budgie and others show it, and so does GNOME with the AppIndicator
// extension (which Ubuntu ships). The desktop draws the menu, so it looks native there. Without a tray,
// the app still runs: `circle-lens -quit` stops it.

import (
	"image/color"
	"os"
	"strconv"
	"strings"
	"sync"

	"circlelens/internal/icon"
)

const (
	trayItemPath = dbusPath("/StatusNotifierItem")
	trayMenuPath = dbusPath("/MenuBar")
	watcherName  = "org.kde.StatusNotifierWatcher"
)

type tray struct {
	sys  *x11System
	bus  *dbusConn
	name string

	mutex    sync.Mutex
	items    map[int32]*trayEntry
	root     *trayEntry
	revision uint32
	open     bool
	paused   bool
}

type trayEntry struct {
	id       int32
	item     menuItem
	children []*trayEntry
}

func startTray(s *x11System) *tray {
	bus, err := connectSessionBus()
	if err != nil {
		debugLog("tray: no session bus (%v)", err)
		return nil
	}
	t := &tray{sys: s, bus: bus, name: "org.kde.StatusNotifierItem-" + strconv.Itoa(os.Getpid()) + "-1", items: map[int32]*trayEntry{}}
	t.root = &trayEntry{}
	t.items[0] = t.root
	go t.serve()
	if _, err := bus.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", "su", t.name, uint32(4)); err != nil {
		debugLog("tray: %v", err)
	}
	bus.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "AddMatch", "s",
		"type='signal',interface='org.freedesktop.DBus',member='NameOwnerChanged',arg0='"+watcherName+"'")
	go t.register()
	return t
}

// register tells the desktop's tray about the icon (again, whenever the tray restarts).
func (t *tray) register() {
	if _, err := t.bus.call(watcherName, "/StatusNotifierWatcher", watcherName, "RegisterStatusNotifierItem", "s", t.name); err != nil {
		debugLog("tray: no tray to show the icon in (%v)", err)
	}
}

func (t *tray) close() { t.bus.close() }

// setMenu replaces the menu (on the main loop) and tells the desktop.
func (s *x11System) setMenu(items []menuItem, open bool) {
	t := s.tray
	if t == nil {
		return
	}
	t.mutex.Lock()
	t.items = map[int32]*trayEntry{}
	t.root = &trayEntry{}
	t.items[0] = t.root
	next := int32(1)
	var add func(parent *trayEntry, items []menuItem)
	add = func(parent *trayEntry, items []menuItem) {
		for _, item := range items {
			entry := &trayEntry{id: next, item: item}
			next++
			t.items[entry.id] = entry
			parent.children = append(parent.children, entry)
			add(entry, item.children)
		}
	}
	add(t.root, items)
	t.revision++
	revision := t.revision
	paused := !app.settings.Enabled
	iconChanged := t.open != open || t.paused != paused
	t.open, t.paused = open, paused
	t.mutex.Unlock()
	t.bus.signal(trayMenuPath, "com.canonical.dbusmenu", "LayoutUpdated", "ui", revision, int32(0))
	if iconChanged {
		t.bus.signal(trayItemPath, "org.kde.StatusNotifierItem", "NewIcon", "")
	}
}

/* ---------- answering the desktop ---------- */

func (t *tray) serve() {
	for {
		select {
		case <-t.bus.closed:
			return
		case m := <-t.bus.calls:
			if m.kind == 4 {
				if m.member == "NameOwnerChanged" && len(m.body) == 3 && m.body[0] == watcherName && m.body[2] != "" {
					go t.register()
				}
				continue
			}
			t.answer(m)
		}
	}
}

// answer replies to one method call. Arguments are checked before use: the bus is shared with every
// app in the session.
func (t *tray) answer(m *dbusMessage) {
	argument := func(i int) any {
		if i < len(m.body) {
			return m.body[i]
		}
		return nil
	}
	switch m.iface + "." + m.member {
	case "org.freedesktop.DBus.Introspectable.Introspect":
		t.bus.reply(m, "s", t.introspection(m.path))
	case "org.freedesktop.DBus.Properties.Get":
		name, _ := argument(1).(string)
		if value, ok := t.properties(m.path)[name]; ok {
			t.bus.reply(m, "v", value)
			return
		}
		t.bus.replyError(m, "org.freedesktop.DBus.Error.UnknownProperty", "no such property")
	case "org.freedesktop.DBus.Properties.GetAll":
		var all []any
		for name, value := range t.properties(m.path) {
			all = append(all, []any{name, value})
		}
		t.bus.reply(m, "a{sv}", all)
	case "org.kde.StatusNotifierItem.Activate", "org.kde.StatusNotifierItem.SecondaryActivate":
		t.sys.post(func() { app.toggle() })
		t.bus.reply(m, "")
	case "org.kde.StatusNotifierItem.ContextMenu", "org.kde.StatusNotifierItem.Scroll":
		t.bus.reply(m, "")
	case "com.canonical.dbusmenu.GetLayout":
		parent, _ := argument(0).(int32)
		depth, ok := argument(1).(int32)
		if !ok {
			depth = -1
		}
		t.mutex.Lock()
		entry := t.items[parent]
		if entry == nil {
			entry = t.root
		}
		layout := t.layout(entry, depth)
		revision := t.revision
		t.mutex.Unlock()
		t.bus.reply(m, "u(ia{sv}av)", revision, layout)
	case "com.canonical.dbusmenu.GetGroupProperties":
		ids, _ := argument(0).([]any)
		out := []any{}
		t.mutex.Lock()
		for _, id := range ids {
			number, _ := id.(int32)
			if entry := t.items[number]; entry != nil {
				out = append(out, []any{entry.id, entryProperties(entry)})
			}
		}
		t.mutex.Unlock()
		t.bus.reply(m, "a(ia{sv})", out)
	case "com.canonical.dbusmenu.GetProperty":
		id, _ := argument(0).(int32)
		name, _ := argument(1).(string)
		t.mutex.Lock()
		entry := t.items[id]
		t.mutex.Unlock()
		if entry != nil {
			for _, pair := range entryProperties(entry) {
				if pair.([]any)[0] == name {
					t.bus.reply(m, "v", pair.([]any)[1])
					return
				}
			}
		}
		t.bus.replyError(m, "org.freedesktop.DBus.Error.InvalidArgs", "no such property")
	case "com.canonical.dbusmenu.Event":
		id, _ := argument(0).(int32)
		kind, _ := argument(1).(string)
		t.event(id, kind)
		t.bus.reply(m, "")
	case "com.canonical.dbusmenu.EventGroup":
		events, _ := argument(0).([]any)
		for _, event := range events {
			if fields, ok := event.([]any); ok && len(fields) == 4 {
				id, _ := fields[0].(int32)
				kind, _ := fields[1].(string)
				t.event(id, kind)
			}
		}
		t.bus.reply(m, "ai", []any{})
	case "com.canonical.dbusmenu.AboutToShow":
		t.bus.reply(m, "b", false)
	case "com.canonical.dbusmenu.AboutToShowGroup":
		t.bus.reply(m, "aiai", []any{}, []any{})
	default:
		if m.kind == 1 {
			t.bus.replyError(m, "org.freedesktop.DBus.Error.UnknownMethod", "unknown method "+m.member)
		}
	}
}

func (t *tray) event(id int32, kind string) {
	if kind != "clicked" {
		return
	}
	t.mutex.Lock()
	entry := t.items[id]
	t.mutex.Unlock()
	if entry != nil && entry.item.action != nil && !entry.item.disabled {
		t.sys.post(entry.item.action)
	}
}

// layout is one entry and its children, depth levels down (-1 for all).
func (t *tray) layout(entry *trayEntry, depth int32) []any {
	children := []any{}
	if depth != 0 {
		for _, child := range entry.children {
			children = append(children, dbusVariant{"(ia{sv}av)", t.layout(child, depth-1)})
		}
	}
	return []any{entry.id, entryProperties(entry), children}
}

func entryProperties(entry *trayEntry) []any {
	if entry.id == 0 {
		return []any{[]any{"children-display", dbusVariant{"s", "submenu"}}}
	}
	item := entry.item
	if item.separator {
		return []any{[]any{"type", dbusVariant{"s", "separator"}}}
	}
	label := strings.ReplaceAll(item.label, "_", "__")
	if item.detail != "" {
		label += " (" + strings.ReplaceAll(item.detail, "_", "__") + ")"
	}
	properties := []any{
		[]any{"label", dbusVariant{"s", label}},
		[]any{"enabled", dbusVariant{"b", !item.disabled}},
	}
	if item.radio || item.checkable {
		kind := "checkmark"
		if item.radio {
			kind = "radio"
		}
		state := int32(0)
		if item.checked {
			state = 1
		}
		properties = append(properties, []any{"toggle-type", dbusVariant{"s", kind}}, []any{"toggle-state", dbusVariant{"i", state}})
	}
	if len(entry.children) > 0 {
		properties = append(properties, []any{"children-display", dbusVariant{"s", "submenu"}})
	}
	return properties
}

func (t *tray) properties(path dbusPath) map[string]dbusVariant {
	if path == trayMenuPath {
		return map[string]dbusVariant{
			"Version":       {"u", uint32(3)},
			"TextDirection": {"s", "ltr"},
			"Status":        {"s", "normal"},
			"IconThemePath": {"as", []any{}},
		}
	}
	t.mutex.Lock()
	open, paused := t.open, t.paused
	t.mutex.Unlock()
	return map[string]dbusVariant{
		"Category":      {"s", "ApplicationStatus"},
		"Id":            {"s", appID()},
		"Title":         {"s", displayName()},
		"Status":        {"s", "Active"},
		"WindowId":      {"i", int32(0)},
		"IconName":      {"s", ""},
		"IconPixmap":    {"a(iiay)", trayPixmaps(open, paused)},
		"IconThemePath": {"s", ""},
		"ItemIsMenu":    {"b", true},
		"Menu":          {"o", trayMenuPath},
		"ToolTip":       {"(sa(iiay)ss)", []any{"", []any{}, displayName(), "Draw a circle twice to magnify"}},
	}
}

// trayPixmaps draws the mark at the sizes trays ask for, as ARGB32 in network byte order: the brand
// blue ring, with the glass filled while the lens is open, and struck through while circles are off.
func trayPixmaps(open, paused bool) []any {
	style := icon.Style{Ink: icon.Accent, Paused: paused}
	if open {
		style.Glass, style.GlassInk = icon.Accent, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	var out []any
	for _, size := range []int{16, 22, 24, 32, 48, 64} {
		picture := icon.Draw(size, style)
		data := make([]any, size*size*4)
		for i := 0; i < size*size; i++ {
			r, g, b, a := picture.Pix[i*4], picture.Pix[i*4+1], picture.Pix[i*4+2], picture.Pix[i*4+3]
			data[i*4], data[i*4+1], data[i*4+2], data[i*4+3] = a, r, g, b
		}
		out = append(out, []any{int32(size), int32(size), data})
	}
	return out
}

func (t *tray) introspection(path dbusPath) string {
	const header = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd"><node>`
	switch path {
	case trayItemPath:
		return header + `<interface name="org.kde.StatusNotifierItem">
<method name="Activate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="SecondaryActivate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="ContextMenu"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="Scroll"><arg type="i" direction="in"/><arg type="s" direction="in"/></method>
<signal name="NewIcon"/><signal name="NewToolTip"/><signal name="NewStatus"><arg type="s"/></signal>
<property name="Category" type="s" access="read"/><property name="Id" type="s" access="read"/>
<property name="Title" type="s" access="read"/><property name="Status" type="s" access="read"/>
<property name="WindowId" type="i" access="read"/><property name="IconName" type="s" access="read"/>
<property name="IconPixmap" type="a(iiay)" access="read"/><property name="IconThemePath" type="s" access="read"/>
<property name="ItemIsMenu" type="b" access="read"/><property name="Menu" type="o" access="read"/>
<property name="ToolTip" type="(sa(iiay)ss)" access="read"/>
</interface></node>`
	case trayMenuPath:
		return header + `<interface name="com.canonical.dbusmenu">
<method name="GetLayout"><arg type="i" direction="in"/><arg type="i" direction="in"/><arg type="as" direction="in"/><arg type="u" direction="out"/><arg type="(ia{sv}av)" direction="out"/></method>
<method name="GetGroupProperties"><arg type="ai" direction="in"/><arg type="as" direction="in"/><arg type="a(ia{sv})" direction="out"/></method>
<method name="GetProperty"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="out"/></method>
<method name="Event"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="in"/><arg type="u" direction="in"/></method>
<method name="EventGroup"><arg type="a(isvu)" direction="in"/><arg type="ai" direction="out"/></method>
<method name="AboutToShow"><arg type="i" direction="in"/><arg type="b" direction="out"/></method>
<method name="AboutToShowGroup"><arg type="ai" direction="in"/><arg type="ai" direction="out"/><arg type="ai" direction="out"/></method>
<signal name="LayoutUpdated"><arg type="u"/><arg type="i"/></signal>
<signal name="ItemsPropertiesUpdated"><arg type="a(ia{sv})"/><arg type="a(ias)"/></signal>
<property name="Version" type="u" access="read"/><property name="TextDirection" type="s" access="read"/>
<property name="Status" type="s" access="read"/><property name="IconThemePath" type="as" access="read"/>
</interface></node>`
	}
	return header + `<node name="StatusNotifierItem"/><node name="MenuBar"/></node>`
}
