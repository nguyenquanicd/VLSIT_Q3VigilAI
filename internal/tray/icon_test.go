package tray

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func at(px []byte, size, x, y int) (r, g, b, a byte) {
	i := (y*size + x) * 4
	return px[i+2], px[i+1], px[i], px[i+3]
}

func TestRender(t *testing.T) {
	const n = 32
	px := Render(n, State{})
	if len(px) != n*n*4 {
		t.Fatalf("buffer: %d bytes", len(px))
	}
	if _, _, _, a := at(px, n, 0, 0); a != 0 {
		t.Errorf("corner should be transparent, alpha=%d", a)
	}
	if r, g, b, a := at(px, n, 16, 3); a != 255 || r != 31 || g != 58 || b != 95 {
		t.Errorf("background: %d %d %d %d", r, g, b, a)
	}
	if r, g, b, _ := at(px, n, 16, 7); r != 255 || g != 255 || b != 255 {
		t.Errorf("ring of the Q should be white: %d %d %d", r, g, b)
	}
	// Unread: a red badge top-right. A problem alone: amber. Unread wins.
	if r, g, _, _ := at(Render(n, State{Unread: true}), n, 24, 7); r < 200 || g > 90 {
		t.Errorf("unread badge: r=%d g=%d", r, g)
	}
	if r, g, _, _ := at(Render(n, State{Problem: true}), n, 24, 7); r < 200 || g < 130 {
		t.Errorf("problem badge: r=%d g=%d", r, g)
	}
	if r, g, _, _ := at(Render(n, State{Problem: true, Unread: true}), n, 24, 7); r < 200 || g > 90 {
		t.Errorf("unread must win over problem: r=%d g=%d", r, g)
	}
	if _, _, b, _ := at(Render(n, State{Scanning: true}), n, 24, 24); b < 200 {
		t.Errorf("scanning dot: b=%d", b)
	}
	if r, g, b, _ := at(Render(n, State{Paused: true}), n, 16, 3); r != 120 || g != 125 || b != 135 {
		t.Errorf("paused background: %d %d %d", r, g, b)
	}

	// Set Q3_ICON_DIR to write the icons out for a visual check.
	if dir := os.Getenv("Q3_ICON_DIR"); dir != "" {
		for name, st := range map[string]State{"normal": {}, "unread": {Unread: true}, "scanning": {Scanning: true, Unread: true},
			"problem": {Problem: true}, "paused": {Paused: true}} {
			const big = 96
			img := image.NewNRGBA(image.Rect(0, 0, big, big))
			p := Render(big, st)
			for y := 0; y < big; y++ {
				for x := 0; x < big; x++ {
					r, g, b, a := at(p, big, x, y)
					img.SetNRGBA(x, y, color.NRGBA{r, g, b, a})
				}
			}
			f, err := os.Create(filepath.Join(dir, "icon-"+name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, img)
			f.Close()
		}
	}
}

// The shell rejects the structure unless its size is exactly what the
// Windows headers define for 64-bit builds.
func TestNotifyIconDataLayout(t *testing.T) {
	var d notifyIconData
	if got := unsafe.Sizeof(d); got != 976 {
		t.Errorf("NOTIFYICONDATAW is %d bytes, want 976", got)
	}
	if got := unsafe.Offsetof(d.szInfo); got != 304 {
		t.Errorf("szInfo at %d, want 304", got)
	}
	if got := unsafe.Offsetof(d.hBalloonIcon); got != 968 {
		t.Errorf("hBalloonIcon at %d, want 968", got)
	}
	if got := len(utf16("một chuỗi dài hơn giới hạn cho phép", 10)); got != 10 {
		t.Errorf("utf16 truncation: %d", got)
	}
}
