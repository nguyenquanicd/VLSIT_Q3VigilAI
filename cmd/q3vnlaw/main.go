// Command q3vnlaw is a portable tray application that watches official
// Vietnamese legal sources and the licensed press for changes relevant to
// the user's topics, and alerts the user.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"q3vnlaw/internal/ai"
	"q3vnlaw/internal/chat"
	"q3vnlaw/internal/fetch"
	"q3vnlaw/internal/pipeline"
	"q3vnlaw/internal/server"
	"q3vnlaw/internal/sources"
	"q3vnlaw/internal/store"
	"q3vnlaw/internal/tray"
	"q3vnlaw/web"
)

// Version is the application version.
const Version = "0.1.0"

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Menu command ids.
const (
	cmdOpen = iota + 1
	cmdAlerts
	cmdScan
	cmdPause1h
	cmdPauseMorning
	cmdResume
	cmdAutostart
	cmdQuit
)

type app struct {
	st      *store.Store
	eng     *pipeline.Engine
	sched   *pipeline.Scheduler
	srv     *server.Server
	tray    *tray.Tray
	dataDir string
	exe     string

	mu       sync.Mutex
	provider ai.Provider
	aiNote   string
}

func main() {
	dataFlag := flag.String("data", "", "thư mục dữ liệu (mặc định: thư mục data cạnh file chạy)")
	noTray := flag.Bool("no-tray", false, "chạy không có biểu tượng khay (dùng để kiểm thử)")
	showVersion := flag.Bool("version", false, "in phiên bản rồi thoát")
	flag.Parse()
	if *showVersion {
		fmt.Println("Q3VNLaw " + Version)
		return
	}
	exe, _ := os.Executable()
	dataDir, fallback := resolveDataDir(*dataFlag, exe)
	logFile := setupLog(dataDir)
	defer logFile.Close()
	log.Printf("Q3VNLaw %s khởi động, dữ liệu tại %s", Version, dataDir)

	// One instance per data folder. A second launch asks the first one to
	// show its window and exits.
	if !acquireInstance(dataDir) {
		log.Print("đã có một phiên bản đang chạy; yêu cầu nó mở cửa sổ")
		showExisting(dataDir)
		return
	}
	if err := run(exe, dataDir, fallback, *noTray); err != nil {
		log.Printf("lỗi nghiêm trọng: %v", err)
		fatalBox("Q3VNLaw không khởi động được:\n" + err.Error())
		os.Exit(1)
	}
}

func run(exe, dataDir string, fallback, noTray bool) error {
	backupBeforeUpgrade(dataDir)
	st, err := store.Open(filepath.Join(dataDir, "q3vnlaw.db"))
	if err != nil {
		return fmt.Errorf("không mở được cơ sở dữ liệu: %w", err)
	}
	defer st.Close()
	os.WriteFile(filepath.Join(dataDir, "version.txt"), []byte(Version), 0o644)
	if err := sources.Sync(st); err != nil {
		return err
	}

	a := &app{st: st, dataDir: dataDir, exe: exe}
	fc := fetch.New()
	a.eng = &pipeline.Engine{St: st, Fetch: fc, DataDir: dataDir, Provider: a.currentProvider}
	notifier := &pipeline.Notifier{St: st, Show: a.toast}
	a.sched = &pipeline.Scheduler{Engine: a.eng, Notifier: notifier, St: st}
	a.srv = &server.Server{St: st, Engine: a.eng, Sched: a.sched, Fetch: fc, Web: web.FS, Version: Version, DataDir: dataDir,
		Chat:  &chat.Service{St: st, Provider: a.currentProvider},
		Token: server.NewToken(), AI: a.aiStatus, ReloadAI: a.reloadAI, SetAutostart: a.setAutostart,
		OpenPath: openPath, Changed: a.refreshTray, ShowWindow: func() { a.openWindow("alerts") }}
	a.eng.Emit = a.srv.Broadcast
	a.srv.TopicSaved = func(id int64) { go a.sched.Backfill(context.Background(), id) }
	a.reloadAI()
	if st.Setting("autostart") == "1" {
		a.setAutostart(true) // the folder may have moved since last run
	}
	if err := a.srv.Listen(); err != nil {
		return fmt.Errorf("không mở được cổng cục bộ: %w", err)
	}
	runtimeFile := filepath.Join(dataDir, "runtime.json")
	info, _ := json.Marshal(map[string]any{"port": a.srv.Port(), "token": a.srv.Token, "pid": os.Getpid(), "url": a.srv.URL("")})
	os.WriteFile(runtimeFile, info, 0o600)
	defer os.Remove(runtimeFile)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := a.srv.Serve(ctx); err != nil {
			log.Printf("máy chủ cục bộ dừng: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		a.sched.Run(ctx)
	}()
	// One small call at start-up tells the user right away when the AI found
	// on the machine cannot be used (typically: the CLI is not signed in).
	go func() {
		if p := a.currentProvider(); p != nil {
			err := ai.Check(ctx, p)
			if ctx.Err() != nil {
				return
			}
			a.eng.ReportAI(err)
			if err != nil {
				log.Printf("kiểm tra AI khi khởi động: %v", err)
			}
			a.refreshTray()
		}
	}()

	if noTray {
		log.Printf("chạy không khay tại %s", a.srv.URL(""))
		waitForStopFile(dataDir)
	} else {
		a.tray = tray.New("Q3VNLaw")
		a.tray.OnClick = func() { a.openWindow("alerts") }
		a.tray.Menu = a.menu
		a.tray.OnCommand = a.command
		a.refreshTray()
		if fallback {
			a.tray.Notify(tray.Balloon{Title: "Q3VNLaw", Level: "system",
				Body: "Không ghi được vào thư mục cạnh file chạy, dữ liệu được lưu tại " + dataDir})
		}
		if topics, _ := st.Topics(false); len(topics) == 0 {
			a.tray.Notify(tray.Balloon{Title: "Q3VNLaw đang chạy ở khay hệ thống", Level: "info",
				Body:    "Bấm vào đây để tạo chủ đề pháp luật đầu tiên cần theo dõi.",
				OnClick: func() { a.openWindow("topics") }})
		}
		if err := a.tray.Run(); err != nil {
			cancel()
			wg.Wait()
			return err
		}
	}
	log.Print("đang thoát")
	cancel()
	// A running scan gets a moment to finish its current step.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
	return nil
}

// ---- data folder, log, single instance -------------------------------------

// resolveDataDir prefers "data" next to the executable, which is what makes
// the app portable; if that is not writable it falls back to the user's
// local application data.
func resolveDataDir(flagValue, exe string) (dir string, fallback bool) {
	if flagValue != "" {
		abs, _ := filepath.Abs(flagValue)
		os.MkdirAll(abs, 0o755)
		return abs, false
	}
	dir = filepath.Join(filepath.Dir(exe), "data")
	if writable(dir) {
		return dir, false
	}
	dir = filepath.Join(os.Getenv("LOCALAPPDATA"), "Q3VNLaw")
	os.MkdirAll(dir, 0o755)
	return dir, true
}

func writable(dir string) bool {
	if os.MkdirAll(dir, 0o755) != nil {
		return false
	}
	probe := filepath.Join(dir, ".write-test")
	if os.WriteFile(probe, []byte("x"), 0o644) != nil {
		return false
	}
	os.Remove(probe)
	return true
}

func setupLog(dataDir string) *os.File {
	dir := filepath.Join(dataDir, "logs")
	os.MkdirAll(dir, 0o755)
	// Logs older than two weeks are removed.
	if old, err := filepath.Glob(filepath.Join(dir, "q3vnlaw-*.log")); err == nil {
		for _, p := range old {
			if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > 14*24*time.Hour {
				os.Remove(p)
			}
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, "q3vnlaw-"+time.Now().Format("20060102")+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return os.Stderr
	}
	log.SetOutput(f)
	return f
}

// backupBeforeUpgrade copies the database aside when a different version of
// the app last used this data folder, before any schema migration runs.
func backupBeforeUpgrade(dataDir string) {
	db := filepath.Join(dataDir, "q3vnlaw.db")
	prev, _ := os.ReadFile(filepath.Join(dataDir, "version.txt"))
	if _, err := os.Stat(db); err != nil || string(prev) == Version || len(prev) == 0 {
		return
	}
	dir := filepath.Join(dataDir, "backups")
	os.MkdirAll(dir, 0o755)
	src, err := os.Open(db)
	if err != nil {
		return
	}
	defer src.Close()
	dst, err := os.Create(filepath.Join(dir, fmt.Sprintf("truoc-nang-cap-%s-%s.db", strings.TrimSpace(string(prev)), time.Now().Format("20060102-150405"))))
	if err != nil {
		return
	}
	defer dst.Close()
	io.Copy(dst, src)
	log.Printf("đã sao lưu cơ sở dữ liệu của phiên bản %s trước khi nâng cấp", prev)
}

var instanceMutex windows.Handle

func acquireInstance(dataDir string) bool {
	name, _ := windows.UTF16PtrFromString(`Local\Q3VNLaw-` + strings.NewReplacer(`\`, "_", ":", "_", "/", "_").Replace(strings.ToLower(dataDir)))
	h, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		return false
	}
	instanceMutex = h
	return true
}

func showExisting(dataDir string) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "runtime.json"))
	if err != nil {
		return
	}
	var info struct {
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &info) != nil {
		return
	}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/show", info.Port), nil)
	req.Header.Set("X-Q3-Token", info.Token)
	client := http.Client{Timeout: 5 * time.Second}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// waitForStopFile blocks until a file named "stop" appears in the data
// folder. It is how the headless mode used by tests is shut down cleanly.
func waitForStopFile(dataDir string) {
	stop := filepath.Join(dataDir, "stop")
	os.Remove(stop)
	for {
		if _, err := os.Stat(stop); err == nil {
			os.Remove(stop)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func fatalBox(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("Q3VNLaw")
	windows.MessageBox(0, t, c, windows.MB_ICONERROR)
}

// ---- AI --------------------------------------------------------------------

func (a *app) reloadAI() {
	cfg := ai.Config{Provider: a.st.Setting("ai_provider"), CLIPath: a.st.Setting("ai_cli_path"), Model: a.st.Setting("ai_model"),
		BaseURL: a.st.Setting("ai_base_url"), Timeout: time.Duration(a.st.SettingInt("ai_timeout_seconds")) * time.Second,
		WorkDir: filepath.Join(a.dataDir, "ai-workdir")}
	keyErr := ""
	if enc := a.st.Setting("ai_api_key"); enc != "" {
		key, err := ai.Unprotect(enc)
		if err != nil {
			keyErr = " (" + err.Error() + ")"
		}
		cfg.APIKey = key
	}
	p, note := ai.Build(cfg)
	a.mu.Lock()
	a.provider, a.aiNote = p, note+keyErr
	a.mu.Unlock()
	log.Printf("AI: %s", note+keyErr)
}

func (a *app) currentProvider() ai.Provider {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider
}

func (a *app) aiStatus() (ai.Provider, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider, a.aiNote
}

// ---- Windows integration ---------------------------------------------------

func (a *app) setAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("Q3VNLaw"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	return k.SetStringValue("Q3VNLaw", `"`+a.exe+`"`)
}

func findEdge() string {
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// openWindow shows the UI: in an Edge "app" window when Edge is installed
// (no address bar, no tabs), otherwise in the default browser.
func (a *app) openWindow(view string) {
	log.Printf("mở cửa sổ: %q", view)
	// Tests set this to exercise the tray without putting windows on screen.
	if os.Getenv("Q3VNLAW_NO_WINDOW") != "" {
		return
	}
	u := a.srv.URL(view)
	if edge := findEdge(); edge != "" {
		// The two "no-…" switches keep Edge from covering the app with its
		// own first-run and default-browser prompts.
		cmd := exec.Command(edge, "--app="+u, "--window-size=1160,760", "--no-first-run", "--no-default-browser-check")
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
			return
		}
	}
	openPath(u)
}

// openPath opens a file, folder or URL with its default application.
func openPath(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	t, _ := windows.UTF16PtrFromString(target)
	return windows.ShellExecute(0, verb, t, nil, nil, windows.SW_SHOWNORMAL)
}

func (a *app) toast(t pipeline.Toast) {
	log.Printf("thông báo [%s] %s | %s", t.Level, t.Title, strings.ReplaceAll(t.Body, "\n", " / "))
	if a.tray == nil {
		return
	}
	view := "alerts"
	if t.AlertID != 0 {
		view = fmt.Sprintf("alerts/%d", t.AlertID)
	} else if t.Level == "system" {
		view = "sources"
	}
	a.tray.Notify(tray.Balloon{Title: t.Title, Body: t.Body, Level: t.Level, OnClick: func() { a.openWindow(view) }})
	a.refreshTray()
}

// refreshTray recomputes the icon badge and tooltip from the current state.
func (a *app) refreshTray() {
	if a.tray == nil {
		return
	}
	unread := a.st.UnreadCount()
	failing := 0
	if list, err := a.st.Sources(); err == nil {
		for _, s := range list {
			if s.Enabled && (s.FailCount >= 3 || s.EmptyCount >= 3) {
				failing++
			}
		}
	}
	paused := time.Now().Before(store.ParseTime(a.st.Setting("pause_until")))
	st := tray.State{Unread: unread > 0, Scanning: a.eng.Running(), Problem: failing > 0, Paused: paused}
	tip := "Q3VNLaw"
	switch {
	case st.Scanning:
		tip += " — đang quét nguồn"
	case unread > 0:
		tip += fmt.Sprintf(" — %d cảnh báo chưa đọc", unread)
	default:
		if scans, _ := a.st.Scans(1); len(scans) > 0 && scans[0].FinishedAt != "" {
			tip += " — quét gần nhất " + store.ParseTime(scans[0].FinishedAt).Local().Format("15:04 02/01") + ", không có tin chưa đọc"
		} else {
			tip += " — chưa quét lần nào"
		}
	}
	if failing > 0 {
		tip += fmt.Sprintf(" · %d nguồn lỗi", failing)
	}
	if paused {
		tip += " · đang tạm dừng thông báo"
	}
	a.tray.SetState(st, tip)
}

func (a *app) menu() []tray.Item {
	unread := a.st.UnreadCount()
	_, note := a.aiStatus()
	if e := a.eng.AIError(); e != "" {
		note = "lỗi – " + e
	}
	if len([]rune(note)) > 70 {
		note = string([]rune(note)[:70]) + "…"
	}
	pause := []tray.Item{{ID: cmdPause1h, Label: "1 giờ"}, {ID: cmdPauseMorning, Label: "Đến 7 giờ sáng mai"}}
	if time.Now().Before(store.ParseTime(a.st.Setting("pause_until"))) {
		pause = append(pause, tray.Item{Separator: true}, tray.Item{ID: cmdResume, Label: "Bật lại ngay"})
	}
	scan := tray.Item{ID: cmdScan, Label: "Quét ngay"}
	if a.eng.Running() {
		scan = tray.Item{Label: "Đang quét…", Disabled: true}
	}
	return []tray.Item{
		{ID: cmdOpen, Label: "Mở Q3VNLaw"},
		{ID: cmdAlerts, Label: fmt.Sprintf("Cảnh báo chưa đọc (%d)", unread)},
		{Separator: true},
		scan,
		{Label: "Tạm dừng thông báo", Sub: pause},
		{Separator: true},
		{Label: "AI: " + note, Disabled: true},
		{ID: cmdAutostart, Label: "Khởi động cùng Windows", Checked: a.st.Setting("autostart") == "1"},
		{Separator: true},
		{ID: cmdQuit, Label: "Thoát"},
	}
}

func (a *app) command(id int) {
	switch id {
	case cmdOpen:
		a.openWindow("")
	case cmdAlerts:
		a.openWindow("alerts")
	case cmdScan:
		go func() {
			if _, err := a.sched.ScanNow(context.Background(), nil); err != nil {
				log.Printf("quét tay: %v", err)
			}
			a.refreshTray()
		}()
	case cmdPause1h:
		a.st.SetSettings(map[string]string{"pause_until": store.FormatTime(time.Now().Add(time.Hour))})
	case cmdPauseMorning:
		now := time.Now()
		morning := time.Date(now.Year(), now.Month(), now.Day()+1, 7, 0, 0, 0, now.Location())
		a.st.SetSettings(map[string]string{"pause_until": store.FormatTime(morning)})
	case cmdResume:
		a.st.SetSettings(map[string]string{"pause_until": ""})
		a.sched.Notifier.Flush()
	case cmdAutostart:
		on := a.st.Setting("autostart") != "1"
		if err := a.setAutostart(on); err != nil {
			log.Printf("khởi động cùng Windows: %v", err)
			return
		}
		v := "0"
		if on {
			v = "1"
		}
		a.st.SetSettings(map[string]string{"autostart": v})
	case cmdQuit:
		a.tray.Quit()
		return
	}
	a.refreshTray()
}
