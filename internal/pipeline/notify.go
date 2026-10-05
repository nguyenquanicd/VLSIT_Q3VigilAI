package pipeline

import (
	"fmt"
	"log"
	"strings"
	"time"

	"q3vnlaw/internal/store"
	"q3vnlaw/internal/textutil"
)

// Toast is one desktop notification.
type Toast struct {
	Title   string
	Body    string
	Level   string // info | notice | warning | system
	AlertID int64  // 0 for digests and system notices
}

// SeverityLabel and StatusLabel are the Vietnamese names shown to the user.
var (
	SeverityLabel = map[string]string{"info": "Thông tin", "notice": "Cần chú ý", "warning": "Cảnh báo"}
	StatusLabel   = map[string]string{"proposal": "Đề xuất", "draft": "Dự thảo", "issued": "Đã ban hành",
		"effective": "Đã có hiệu lực", "other": "Tin liên quan", "unknown": ""}
)

// Notifier turns pending alerts into toasts, honouring the user's quiet
// hours, pause and per-level switches.
type Notifier struct {
	St   *store.Store
	Show func(Toast)
	Now  func() time.Time
}

func (n *Notifier) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// Muted reports whether toasts are held back right now.
func (n *Notifier) Muted() bool {
	now := n.now()
	if until := store.ParseTime(n.St.Setting("pause_until")); now.Before(until) {
		return true
	}
	qs, qe := n.St.Setting("quiet_start"), n.St.Setting("quiet_end")
	return qs != qe && InWindow(now, qs, qe)
}

// Flush shows the pending alerts and returns how many toasts were shown.
// While muted nothing is shown and nothing is lost: alerts stay pending.
func (n *Notifier) Flush() int {
	if n.Muted() {
		return 0
	}
	pending, err := n.St.PendingNotifications()
	if err != nil || len(pending) == 0 {
		return 0
	}
	var show []store.Alert
	var ids []int64
	held := map[string]int{}
	for _, a := range pending {
		ids = append(ids, a.ID)
		if n.St.Setting("notify_"+a.Severity) == "1" {
			show = append(show, a)
		} else {
			held[a.Severity]++
		}
	}
	// Levels the user switched off are settled without a toast; they remain in
	// the list. The log says so, because "no toast" is otherwise
	// indistinguishable from "the app is broken".
	for sev, c := range held {
		log.Printf("không hiện thông báo cho %d cảnh báo mức %q vì mức này đang tắt trong Cài đặt → Thông báo", c, SeverityLabel[sev])
	}
	defer n.St.MarkNotified(ids)
	if len(show) == 0 {
		return 0
	}
	if over := n.St.SettingInt("notify_group_over"); over > 0 && len(show) > over {
		n.Show(digest(show))
		return 1
	}
	for _, a := range show {
		n.Show(AlertToast(a))
	}
	return len(show)
}

// AlertToast formats one alert: level and legal status, headline, source.
func AlertToast(a store.Alert) Toast {
	tags := []string{SeverityLabel[a.Severity]}
	if s := StatusLabel[a.LegalStatus]; s != "" {
		tags = append(tags, s)
	}
	if a.Verified == "unconfirmed" {
		tags = append(tags, "chưa xác nhận")
	}
	body := a.Title
	if a.SourceName != "" {
		body += "\n— " + a.SourceName
	}
	return Toast{Title: textutil.Truncate(strings.Join(tags, " · ")+" | "+a.TopicName, 62), Body: textutil.Truncate(body, 250),
		Level: a.Severity, AlertID: a.ID}
}

func digest(list []store.Alert) Toast {
	count := map[string]int{}
	level := "info"
	for _, a := range list {
		count[a.Severity]++
		if a.Severity == "warning" || (a.Severity == "notice" && level == "info") {
			level = a.Severity
		}
	}
	var parts []string
	for _, sev := range []string{"warning", "notice", "info"} {
		if count[sev] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count[sev], strings.ToLower(SeverityLabel[sev])))
		}
	}
	body := strings.Join(parts, ", ") + ".\n"
	for i, a := range list {
		if i == 2 {
			body += fmt.Sprintf("… và %d tin khác", len(list)-2)
			break
		}
		body += "• " + textutil.Truncate(a.Title, 90) + "\n"
	}
	return Toast{Title: fmt.Sprintf("Q3VNLaw: %d cảnh báo pháp luật mới", len(list)), Body: textutil.Truncate(body, 250), Level: level}
}

// Test shows a sample toast whatever the quiet hours and level switches say,
// so the user can tell whether toasts reach the screen at all.
func (n *Notifier) Test() {
	n.Show(Toast{Title: "Q3VNLaw: thông báo thử", Level: "notice",
		Body: "Nếu bạn thấy dòng này, thông báo của Q3VNLaw hoạt động. Bấm vào đây để mở danh sách cảnh báo."})
}

// ScanSummary answers a scan the user asked for. Such a scan must always say
// something: "no toast" after pressing the button reads as "broken". It is
// shown even in quiet hours, since the user is at the machine.
func (n *Notifier) ScanSummary(alertsNew, sourcesFailed int, shown int) {
	if shown > 0 && sourcesFailed == 0 {
		return // real alert toasts already told the story
	}
	title, body := "Q3VNLaw: quét xong", ""
	switch {
	case alertsNew > 0 && shown == 0:
		body = fmt.Sprintf("%d cảnh báo mới, nhưng thông báo cho mức này đang tắt. Bấm để xem trong danh sách.", alertsNew)
	case alertsNew == 0:
		body = "Không có tin mới khớp các chủ đề đang theo dõi."
	}
	if sourcesFailed > 0 {
		body = strings.TrimSpace(body + fmt.Sprintf(" %d nguồn không quét được, xem mục Nguồn.", sourcesFailed))
	}
	n.Show(Toast{Title: title, Body: body, Level: "info"})
}

// SystemCheck raises at most one system toast a day about sources that keep
// failing or keep coming back empty. Silence here would read as "no news".
func (n *Notifier) SystemCheck() {
	if n.St.Setting("notify_system") != "1" || n.Muted() {
		return
	}
	now := n.now()
	if now.Sub(store.ParseTime(n.St.Setting("last_system_notice_at"))) < 24*time.Hour {
		return
	}
	list, err := n.St.Sources()
	if err != nil {
		return
	}
	var bad []string
	for _, s := range list {
		if s.Enabled && (s.FailCount >= 3 || s.EmptyCount >= 3) {
			bad = append(bad, s.Name)
		}
	}
	if len(bad) == 0 {
		return
	}
	body := strings.Join(bad, "; ")
	n.Show(Toast{Title: fmt.Sprintf("Q3VNLaw: %d nguồn không quét được", len(bad)), Level: "system",
		Body: textutil.Truncate(body+".\nTin từ các nguồn này đang KHÔNG được theo dõi. Mở mục Nguồn để xem lỗi.", 250)})
	n.St.SetSettings(map[string]string{"last_system_notice_at": store.FormatTime(now)})
}
