// Package tray puts the app in the Windows notification area: an icon with
// a state badge, a right-click menu, and balloon notifications (which
// Windows 10 and 11 show as toasts). It talks to Win32 directly, so the app
// needs no installer, no registration and no runtime.
package tray

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	kernel  = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassEx       = user32.NewProc("RegisterClassExW")
	pCreateWindowEx        = user32.NewProc("CreateWindowExW")
	pDefWindowProc         = user32.NewProc("DefWindowProcW")
	pGetMessage            = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessage       = user32.NewProc("DispatchMessageW")
	pPostMessage           = user32.NewProc("PostMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pAppendMenu            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pCreateIconIndirect    = user32.NewProc("CreateIconIndirect")
	pDestroyIcon           = user32.NewProc("DestroyIcon")
	pSetDpiAwareness       = user32.NewProc("SetProcessDpiAwarenessContext")
	pShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	pCreateBitmap          = gdi32.NewProc("CreateBitmap")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pGetModuleHandle       = kernel.NewProc("GetModuleHandleW")
)

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	wmApp         = 0x8000
	wmTray        = wmApp + 1 // from the shell: mouse and balloon events
	wmApply       = wmApp + 2 // from other goroutines: state changed
	wmQuit        = wmApp + 3

	ninBalloonUserClick = 0x0405

	nimAdd, nimModify, nimDelete        = 0, 1, 2
	nifMessage, nifIcon, nifTip, nifInf = 0x1, 0x2, 0x4, 0x10

	mfString, mfGrayed, mfChecked, mfPopup, mfSeparator = 0x0, 0x1, 0x8, 0x10, 0x800
	tpmRightButton, tpmReturnCmd, tpmNoNotify           = 0x2, 0x100, 0x80

	smCxSmIcon = 49
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     uintptr
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

// Item is one entry of the right-click menu.
type Item struct {
	ID        int // returned to OnCommand; 0 for labels and separators
	Label     string
	Checked   bool
	Disabled  bool
	Separator bool
	Sub       []Item
}

// Balloon is a notification.
type Balloon struct {
	Title   string
	Body    string
	Level   string // info | notice | warning | system
	OnClick func()
}

// Tray is the notification-area icon.
type Tray struct {
	// OnClick is called on a left click of the icon.
	OnClick func()
	// Menu builds the right-click menu each time it opens.
	Menu func() []Item
	// OnCommand receives the ID of the chosen menu entry.
	OnCommand func(id int)

	hwnd           uintptr
	icon           uintptr
	taskbarCreated uint32

	mu       sync.Mutex
	state    State
	tooltip  string
	dirty    bool
	balloons []Balloon
	onClick  func() // click handler of the balloon on screen
}

var current *Tray // the window procedure has no user pointer; there is one tray

// New prepares a tray icon with its first tooltip.
func New(tooltip string) *Tray { return &Tray{tooltip: tooltip} }

func utf16(s string, max int) []uint16 {
	u, _ := windows.UTF16FromString(s)
	if len(u) > max {
		u = append(u[:max-1], 0)
	}
	return u
}

func (t *Tray) data(flags uint32) *notifyIconData {
	d := &notifyIconData{hWnd: t.hwnd, uID: 1, uFlags: flags, uCallbackMessage: wmTray, hIcon: t.icon}
	d.cbSize = uint32(unsafe.Sizeof(*d))
	copy(d.szTip[:], utf16(t.tooltip, len(d.szTip)))
	return d
}

func makeIcon(st State) uintptr {
	size, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	if size < 16 {
		size = 16
	}
	// Draw at twice the size the shell asks for; it scales down cleanly and
	// stays sharp where the shell shows the icon larger.
	n := int(size) * 2
	px := Render(n, st)
	color, _, _ := pCreateBitmap.Call(uintptr(n), uintptr(n), 1, 32, uintptr(unsafe.Pointer(&px[0])))
	mask, _, _ := pCreateBitmap.Call(uintptr(n), uintptr(n), 1, 1, 0)
	defer pDeleteObject.Call(color)
	defer pDeleteObject.Call(mask)
	info := iconInfo{fIcon: 1, hbmMask: mask, hbmColor: color}
	h, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(px)
	return h
}

// Run creates the icon and processes its messages until Quit. It must be
// called from the main goroutine; it pins itself to its OS thread because a
// window belongs to the thread that created it.
func (t *Tray) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	current = t
	// Per-monitor DPI awareness, so the icon is drawn at its real size.
	if pSetDpiAwareness.Find() == nil {
		pSetDpiAwareness.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	}
	inst, _, _ := pGetModuleHandle.Call(0)
	class, _ := windows.UTF16PtrFromString("Q3VNLawTray")
	wc := wndClassEx{lpfnWndProc: windows.NewCallback(wndProc), hInstance: inst, lpszClassName: class}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return errors.New("RegisterClassEx: " + err.Error())
	}
	// A hidden top-level window: a message-only window would not receive the
	// "TaskbarCreated" broadcast sent when Explorer restarts.
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		return errors.New("CreateWindowEx: " + err.Error())
	}
	t.hwnd = hwnd
	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	tc, _, _ := pRegisterWindowMessage.Call(uintptr(unsafe.Pointer(name)))
	t.taskbarCreated = uint32(tc)

	t.mu.Lock()
	t.icon = makeIcon(t.state)
	t.dirty = false
	d := t.data(nifMessage | nifIcon | nifTip)
	t.mu.Unlock()
	if r, _, _ := pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(d))); r == 0 {
		return errors.New("không tạo được biểu tượng ở khay hệ thống")
	}
	// State set before the window existed is applied now.
	pPostMessage.Call(t.hwnd, wmApply, 0, 0)

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	t := current
	switch {
	case t == nil:
	case message == wmTray:
		switch uint32(lParam) & 0xFFFF {
		case wmLButtonUp:
			if t.OnClick != nil {
				go t.OnClick()
			}
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		case ninBalloonUserClick:
			t.mu.Lock()
			f := t.onClick
			t.mu.Unlock()
			if f != nil {
				go f()
			}
		}
		return 0
	case message == wmApply:
		t.apply()
		return 0
	case message == wmQuit || message == wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case message == wmDestroy:
		pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(t.data(0))))
		if t.icon != 0 {
			pDestroyIcon.Call(t.icon)
		}
		pPostQuitMessage.Call(0)
		return 0
	case message == t.taskbarCreated && t.taskbarCreated != 0:
		// Explorer restarted and forgot every icon.
		t.mu.Lock()
		d := t.data(nifMessage | nifIcon | nifTip)
		t.mu.Unlock()
		pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(d)))
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

// apply runs on the window thread: it pushes the pending icon, tooltip and
// balloons to the shell.
func (t *Tray) apply() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dirty {
		old := t.icon
		t.icon = makeIcon(t.state)
		t.dirty = false
		pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(t.data(nifIcon|nifTip))))
		if old != 0 {
			pDestroyIcon.Call(old)
		}
	}
	for _, b := range t.balloons {
		d := t.data(nifInf)
		copy(d.szInfoTitle[:], utf16(b.Title, len(d.szInfoTitle)))
		copy(d.szInfo[:], utf16(b.Body, len(d.szInfo)))
		switch b.Level {
		case "warning", "system":
			d.dwInfoFlags = 2 // NIIF_WARNING
		default:
			d.dwInfoFlags = 1 // NIIF_INFO
		}
		t.onClick = b.OnClick
		pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(d)))
	}
	t.balloons = nil
}

func (t *Tray) post(message uint32) {
	if t.hwnd != 0 {
		pPostMessage.Call(t.hwnd, uintptr(message), 0, 0)
	}
}

// SetState changes the icon and tooltip. Safe to call from any goroutine.
func (t *Tray) SetState(st State, tooltip string) {
	t.mu.Lock()
	if t.state != st || t.tooltip != tooltip {
		t.state, t.tooltip, t.dirty = st, tooltip, true
	}
	t.mu.Unlock()
	t.post(wmApply)
}

// Notify shows a balloon. Safe to call from any goroutine.
func (t *Tray) Notify(b Balloon) {
	t.mu.Lock()
	t.balloons = append(t.balloons, b)
	t.mu.Unlock()
	t.post(wmApply)
}

// Quit removes the icon and makes Run return.
func (t *Tray) Quit() { t.post(wmQuit) }

func buildMenu(items []Item) uintptr {
	menu, _, _ := pCreatePopupMenu.Call()
	for _, it := range items {
		if it.Separator {
			pAppendMenu.Call(menu, mfSeparator, 0, 0)
			continue
		}
		label, _ := windows.UTF16PtrFromString(it.Label)
		flags, id := uintptr(mfString), uintptr(it.ID)
		if it.Checked {
			flags |= mfChecked
		}
		if it.Disabled {
			flags |= mfGrayed
		}
		if len(it.Sub) > 0 {
			flags |= mfPopup
			id = buildMenu(it.Sub) // owned and destroyed by the parent menu
		}
		pAppendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(label)))
	}
	return menu
}

func (t *Tray) showMenu() {
	if t.Menu == nil {
		return
	}
	menu := buildMenu(t.Menu())
	defer pDestroyMenu.Call(menu)
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	// Without this the menu does not close when the user clicks elsewhere.
	pSetForegroundWindow.Call(t.hwnd)
	id, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify, uintptr(p.x), uintptr(p.y), 0, t.hwnd, 0)
	if id != 0 && t.OnCommand != nil {
		go t.OnCommand(int(id))
	}
}
