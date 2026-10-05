package tray

import "math"

// State is what the tray icon shows.
type State struct {
	Unread   bool
	Scanning bool
	Problem  bool // a source keeps failing, or the AI is unusable
	Paused   bool
}

type rgb struct{ r, g, b float64 }

var (
	navy  = rgb{31, 58, 95}
	grey  = rgb{120, 125, 135}
	white = rgb{255, 255, 255}
	red   = rgb{229, 57, 53}
	amber = rgb{255, 160, 0}
	blue  = rgb{66, 165, 245}
)

// Render draws the icon at size×size and returns BGRA pixels, top row first.
// The picture is a rounded square with a "Q", plus a badge in the top-right
// corner (red: unread alerts, amber: a problem) and a dot in the bottom-right
// corner while a scan runs. It is drawn in code so the app ships as one file.
func Render(size int, st State) []byte {
	const ss = 4 // supersampling per axis
	out := make([]byte, size*size*4)
	bg := navy
	if st.Paused {
		bg = grey
	}
	badge, hasBadge := red, st.Unread
	if st.Problem && !st.Unread {
		badge, hasBadge = amber, true
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := (float64(x) + (float64(sx)+0.5)/ss) / float64(size)
					v := (float64(y) + (float64(sy)+0.5)/ss) / float64(size)
					if c, ok := pixel(u, v, bg, badge, hasBadge, st.Scanning); ok {
						r, g, b, a = r+c.r, g+c.g, b+c.b, a+1
					}
				}
			}
			i := (y*size + x) * 4
			if a > 0 {
				out[i], out[i+1], out[i+2] = byte(b/a), byte(g/a), byte(r/a)
				out[i+3] = byte(255 * a / (ss * ss))
			}
		}
	}
	return out
}

func dist(u, v, cx, cy float64) float64 { return math.Hypot(u-cx, v-cy) }

// pixel returns the colour of the icon at (u, v) in the unit square.
func pixel(u, v float64, bg, badge rgb, hasBadge, scanning bool) (rgb, bool) {
	if hasBadge {
		if d := dist(u, v, 0.78, 0.22); d < 0.17 {
			return badge, true
		} else if d < 0.22 {
			return white, true
		}
	}
	if scanning {
		if d := dist(u, v, 0.78, 0.78); d < 0.15 {
			return blue, true
		} else if d < 0.20 {
			return white, true
		}
	}
	// Rounded square.
	const inset, rad = 0.04, 0.22
	cx := math.Min(math.Max(u, inset+rad), 1-inset-rad)
	cy := math.Min(math.Max(v, inset+rad), 1-inset-rad)
	if dist(u, v, cx, cy) > rad {
		return rgb{}, false
	}
	// The ring of the Q.
	if d := dist(u, v, 0.5, 0.47); d < 0.30 && d > 0.185 {
		return white, true
	}
	// The tail of the Q: a thick segment.
	ax, ay, bx, by := 0.58, 0.58, 0.80, 0.81
	t := ((u-ax)*(bx-ax) + (v-ay)*(by-ay)) / ((bx-ax)*(bx-ax) + (by-ay)*(by-ay))
	t = math.Min(math.Max(t, 0), 1)
	if dist(u, v, ax+t*(bx-ax), ay+t*(by-ay)) < 0.062 {
		return white, true
	}
	return bg, true
}
