package shortcut

// DoubleTap spots a modifier tapped twice: pressed and let go on its own (within tapMs, with no other key
// in between), then again within betweenMs. Capitals, Ctrl+C and the like never count, because another
// key comes between the press and the release.
type DoubleTap struct {
	// Only is the modifier to watch ("Ctrl", "Shift", "Alt"); empty watches all three (recording).
	Only string

	down     string  // the modifier held now, alone
	downAt   float64 // when it went down
	tapped   string  // the modifier tapped once already
	tappedAt float64 // when that tap ended
}

const (
	tapMs     = 300
	betweenMs = 400
)

// Down reports a key going down at t (ms). Modifier is "Ctrl", "Shift" or "Alt" for those keys and
// empty for any other key.
func (d *DoubleTap) Down(modifier string, t float64) {
	switch {
	case modifier == "":
		d.reset() // another key: whatever was held is a combination, not a tap
	case d.down == modifier:
		// held down: the keyboard repeating it
	case d.down != "":
		d.reset() // two modifiers together: a combination
	default:
		d.down, d.downAt = modifier, t
	}
}

// Up reports a key being let go at t; it returns the modifier when that completed a double tap.
func (d *DoubleTap) Up(modifier string, t float64) string {
	if modifier == "" || modifier != d.down {
		return ""
	}
	d.down = ""
	if t-d.downAt > tapMs || (d.Only != "" && modifier != d.Only) {
		d.reset() // held too long to be a tap, or not the one watched
		return ""
	}
	if d.tapped == modifier && t-d.tappedAt <= betweenMs+tapMs {
		d.reset()
		return modifier
	}
	d.tapped, d.tappedAt = modifier, t
	return ""
}

func (d *DoubleTap) reset() { *d = DoubleTap{Only: d.Only} }
