package pipeline

import (
	"fmt"
	"log"
	"strings"
	"time"

	"q3vigilai/internal/i18n"
	"q3vigilai/internal/store"
	"q3vigilai/internal/textutil"
)

// Toast is one desktop notification.
type Toast struct {
	Title   string
	Body    string
	Level   string // info | notice | warning | system
	AlertID int64  // 0 for digests and system notices
}

// SeverityLabel and StatusLabel hold the names shown to the user, in the source
// language; read them through SeverityName and StatusName to get the current one.
var (
	SeverityLabel = map[string]string{"info": i18n.N("Thông tin"), "notice": i18n.N("Cần chú ý"), "warning": i18n.N("Cảnh báo")}
	StatusLabel   = map[string]string{"proposal": i18n.N("Đề xuất"), "draft": i18n.N("Dự thảo"), "issued": i18n.N("Đã ban hành"),
		"effective": i18n.N("Đã có hiệu lực"), "other": i18n.N("Tin liên quan"), "unknown": ""}
)

// SeverityName returns the display name of an alert level.
func SeverityName(sev string) string { return i18n.TC("mức", SeverityLabel[sev]) }

// StatusName returns the display name of a legal status ("" for unknown).
func StatusName(status string) string {
	if StatusLabel[status] == "" {
		return ""
	}
	return i18n.T(StatusLabel[status])
}

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
	tags := []string{SeverityName(a.Severity)}
	if s := StatusName(a.LegalStatus); s != "" {
		tags = append(tags, s)
	}
	if a.Verified == "unconfirmed" {
		tags = append(tags, i18n.T("chưa xác nhận"))
	}
	body := a.Title
	if a.SourceName != "" {
		body += "\n— " + i18n.T(a.SourceName)
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
			parts = append(parts, fmt.Sprintf("%d %s", count[sev], strings.ToLower(SeverityName(sev))))
		}
	}
	body := strings.Join(parts, ", ") + ".\n"
	for i, a := range list {
		if i == 2 {
			body += i18n.T("… và {0} tin khác", len(list)-2)
			break
		}
		body += "• " + textutil.Truncate(a.Title, 90) + "\n"
	}
	return Toast{Title: i18n.T("Q3VigilAI: {0} cảnh báo pháp luật mới", len(list)), Body: textutil.Truncate(body, 250), Level: level}
}

// Test shows a sample toast whatever the quiet hours and level switches say,
// so the user can tell whether toasts reach the screen at all.
func (n *Notifier) Test() {
	n.Show(Toast{Title: i18n.T("Q3VigilAI: thông báo thử"), Level: "notice",
		Body: i18n.T("Nếu bạn thấy dòng này, thông báo của Q3VigilAI hoạt động. Bấm vào đây để mở danh sách cảnh báo.")})
}

// ScanSummary answers a scan the user asked for. Such a scan must always say
// something: "no toast" after pressing the button reads as "broken". It is
// shown even in quiet hours, since the user is at the machine.
func (n *Notifier) ScanSummary(alertsNew, sourcesFailed int, shown int) {
	if shown > 0 && sourcesFailed == 0 {
		return // real alert toasts already told the story
	}
	title, body := i18n.T("Q3VigilAI: quét xong"), ""
	switch {
	case alertsNew > 0 && shown == 0:
		body = i18n.T("{0} cảnh báo mới, nhưng thông báo cho mức này đang tắt. Bấm để xem trong danh sách.", alertsNew)
	case alertsNew == 0:
		body = i18n.T("Không có tin mới khớp các chủ đề đang theo dõi.")
	}
	if sourcesFailed > 0 {
		body = strings.TrimSpace(body + " " + i18n.T("{0} nguồn không quét được, xem mục Nguồn.", sourcesFailed))
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
	for i := range bad {
		bad[i] = i18n.T(bad[i])
	}
	body := strings.Join(bad, "; ")
	n.Show(Toast{Title: i18n.T("Q3VigilAI: {0} nguồn không quét được", len(bad)), Level: "system",
		Body: textutil.Truncate(body+".\n"+i18n.T("Tin từ các nguồn này đang KHÔNG được theo dõi. Mở mục Nguồn để xem lỗi."), 250)})
	n.St.SetSettings(map[string]string{"last_system_notice_at": store.FormatTime(now)})
}
