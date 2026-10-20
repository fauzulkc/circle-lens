package shortcut

// usual is what common shortcuts already do in most apps or in the system. Picking one of these is
// allowed, but the app asks first: a key combination is taken from every app while Circle Lens runs,
// and a double tap fires both.
var usual = map[string]string{
	"Ctrl twice": "shows the pointer in PowerToys (Find My Mouse)",

	"Ctrl+A": "selects all in most apps", "Ctrl+B": "makes text bold in most editors", "Ctrl+C": "copies in most apps",
	"Ctrl+D": "bookmarks the page in browsers", "Ctrl+E": "searches in browsers and File Explorer", "Ctrl+F": "finds in most apps",
	"Ctrl+G": "finds the next match in many apps", "Ctrl+H": "replaces text or shows history in many apps", "Ctrl+I": "makes text italic in most editors",
	"Ctrl+J": "shows downloads in browsers", "Ctrl+K": "adds a link in most editors", "Ctrl+L": "selects the address bar in browsers",
	"Ctrl+N": "opens a new window in most apps", "Ctrl+O": "opens a file in most apps", "Ctrl+P": "prints in most apps",
	"Ctrl+R": "reloads the page in browsers", "Ctrl+S": "saves in most apps", "Ctrl+T": "opens a new tab in browsers",
	"Ctrl+U": "underlines text in most editors", "Ctrl+V": "pastes in most apps", "Ctrl+W": "closes the tab in most apps",
	"Ctrl+X": "cuts in most apps", "Ctrl+Y": "redoes in most apps", "Ctrl+Z": "undoes in most apps",
	"Ctrl+0": "resets the zoom in browsers", "Ctrl+=": "zooms in in browsers", "Ctrl+-": "zooms out in browsers",
	"Ctrl+Tab": "switches tabs in most apps", "Ctrl+Space": "switches the input language or completes code in many apps",
	"Ctrl+Enter": "sends or runs in many apps", "Ctrl+Backspace": "deletes a word in most apps", "Ctrl+Delete": "deletes a word in most apps",
	"Ctrl+Left": "moves back a word in most apps", "Ctrl+Right": "moves on a word in most apps",
	"Ctrl+Home": "goes to the start in most apps", "Ctrl+End": "goes to the end in most apps",
	"Ctrl+F4": "closes the tab or document in most apps", "Ctrl+F5": "reloads the page without its cache in browsers",
	"Ctrl+Esc": "opens the Start menu",

	"Ctrl+Shift+Esc": "opens Task Manager", "Ctrl+Shift+N": "opens a private window in browsers", "Ctrl+Shift+T": "reopens a closed tab in browsers",
	"Ctrl+Shift+Z": "redoes in most apps", "Ctrl+Shift+S": "saves as in most apps", "Ctrl+Shift+V": "pastes plain text in many apps",
	"Ctrl+Shift+I": "opens developer tools in browsers", "Ctrl+Shift+C": "copies in terminals and inspects in browsers",
	"Ctrl+Shift+P": "opens the command palette in code editors", "Ctrl+Shift+Tab": "switches tabs backwards in most apps",
	"Ctrl+Alt+Delete": "opens the Windows security screen", "Ctrl+Alt+T": "opens a terminal on Linux",

	"Alt+F4": "closes the window", "Alt+Tab": "switches windows", "Alt+Esc": "switches windows", "Alt+Space": "opens the window menu",
	"Alt+Enter": "shows properties or goes full screen in many apps",
	"Alt+Left":  "goes back in browsers and File Explorer", "Alt+Right": "goes forward in browsers and File Explorer",

	"Win+A": "opens quick settings", "Win+D": "shows the desktop", "Win+E": "opens File Explorer", "Win+H": "starts dictation",
	"Win+I": "opens Settings", "Win+K": "casts to a display", "Win+L": "locks the computer", "Win+M": "minimises every window",
	"Win+N": "opens notifications", "Win+P": "chooses a display mode", "Win+R": "opens Run", "Win+S": "opens search",
	"Win+V": "opens the clipboard history", "Win+X": "opens the quick link menu", "Win+Tab": "opens Task view", "Win+.": "opens the emoji panel",
	"Win+Up": "maximises the window", "Win+Down": "minimises the window", "Win+Left": "snaps the window left", "Win+Right": "snaps the window right",
	"Win+Shift+S": "takes a screenshot", "Win+Ctrl+D": "adds a virtual desktop",

	"F1": "opens help in most apps", "F2": "renames in File Explorer", "F3": "finds the next match in many apps",
	"F5": "reloads in browsers and File Explorer", "F6": "moves between panes or to the address bar", "F7": "turns on caret browsing in browsers",
	"F10": "selects the menu bar", "F11": "goes full screen in browsers", "F12": "opens developer tools in browsers",
}

// Conflict says what a shortcut already does in most apps or in the system ("copies in most apps"), or
// "" if it is not a common one.
func Conflict(s Shortcut) string {
	if text, ok := usual[s.String()]; ok {
		return text
	}
	if !s.IsDoubleTap() && s.Win && !s.Ctrl && !s.Alt && len(s.Key) == 1 && s.Key >= "0" && s.Key <= "9" {
		return "opens the app pinned at that place on the taskbar"
	}
	return ""
}
