package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"q3vigilai/internal/i18n"
)

// hidden keeps a console child process from flashing a window: the app
// itself is a GUI process with no console to share.
func hidden(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// FindClaude locates the Claude Code CLI: on PATH, in the usual install
// locations, or the copy bundled with the Claude desktop app.
func FindClaude() string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")
	if p := firstExisting(
		filepath.Join(home, ".local", "bin", "claude.exe"),
		filepath.Join(home, ".claude", "local", "claude.exe"),
		filepath.Join(appData, "npm", "claude.cmd"),
	); p != "" {
		return p
	}
	// The desktop app keeps one folder per version; take the newest. It is a
	// packaged (MSIX) app: only processes running inside it see its files under
	// %APPDATA%. Anything started from Explorer, the Run key or a terminal
	// must look in the package's own folder instead.
	var matches []string
	for _, root := range []string{
		filepath.Join(appData, "Claude", "claude-code"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Packages", "Claude_*", "LocalCache", "Roaming", "Claude", "claude-code"),
	} {
		found, _ := filepath.Glob(filepath.Join(root, "*", "*", "claude.exe"))
		matches = append(matches, found...)
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		a, _ := os.Stat(matches[i])
		b, _ := os.Stat(matches[j])
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	return matches[0]
}

// FindCodex locates the Codex CLI.
func FindCodex() string {
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	return firstExisting(
		filepath.Join(os.Getenv("APPDATA"), "npm", "codex.cmd"),
		filepath.Join(home, ".cargo", "bin", "codex.exe"),
		filepath.Join(home, ".local", "bin", "codex.exe"),
	)
}

func isScript(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".cmd" || ext == ".bat"
}

// run executes a CLI with the prompt on stdin and returns stdout.
func run(ctx context.Context, cfg Config, path string, args []string, stdin string, onLine func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	hidden(cmd)
	if cfg.WorkDir != "" {
		os.MkdirAll(cfg.WorkDir, 0o755)
		cmd.Dir = cfg.WorkDir
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	var out bytes.Buffer
	if onLine == nil {
		cmd.Stdout = &out
		err := cmd.Run()
		return out.String(), cliErr(ctx, err, stderr.String(), out.String())
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", cliErr(ctx, err, "", "")
	}
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		out.WriteString(line)
		out.WriteByte('\n')
		onLine(line)
	}
	err = cmd.Wait()
	return out.String(), cliErr(ctx, err, stderr.String(), out.String())
}

func cliErr(ctx context.Context, err error, stderr, stdout string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New(i18n.T("AI không phản hồi trong thời gian cho phép"))
	}
	// Claude reports failures in its JSON result with a non-zero exit code;
	// let the caller read that instead of the bare exit status.
	if strings.Contains(stdout, `"type":"result"`) {
		return nil
	}
	msg := strings.TrimSpace(stderr)
	if len(msg) > 400 {
		msg = msg[:400]
	}
	if msg == "" {
		msg = err.Error()
	}
	return errors.New(i18n.T("CLI lỗi: {0}", msg))
}

// ---- Claude CLI ------------------------------------------------------------

type claudeCLI struct {
	path string
	cfg  Config
}

func (c *claudeCLI) Name() string { return "claude-cli" }

// args builds the non-interactive invocation, verified against Claude Code
// 2.1.286: no tools, no MCP servers, no skills, no saved session.
func (c *claudeCLI) args(r Request, format string) (args []string, stdin string) {
	args = []string{"-p", "--output-format", format, "--tools", "", "--strict-mcp-config",
		"--disable-slash-commands", "--no-session-persistence"}
	if format == "stream-json" {
		args = append(args, "--verbose", "--include-partial-messages")
	}
	if c.cfg.Model != "" {
		args = append(args, "--model", c.cfg.Model)
	}
	stdin = r.Prompt
	// A .cmd launcher goes through cmd.exe, whose quoting rules make long
	// free text unsafe as an argument; there the system text travels on stdin.
	if r.System != "" {
		if isScript(c.path) {
			stdin = r.System + "\n\n" + r.Prompt
		} else {
			args = append(args, "--system-prompt", r.System)
		}
	}
	return args, stdin
}

type claudeResult struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

func claudeOutcome(res claudeResult) (string, error) {
	if res.IsError {
		if strings.Contains(strings.ToLower(res.Result), "not logged in") || strings.Contains(res.Result, "/login") {
			return "", fmt.Errorf("%w: %s", ErrAuth, i18n.T("Claude CLI chưa đăng nhập. Mở terminal, chạy lệnh claude rồi gõ /login"))
		}
		if strings.Contains(strings.ToLower(res.Result), "invalid api key") {
			return "", fmt.Errorf("%w: %s", ErrAuth, res.Result)
		}
		return "", errors.New(i18n.T("Claude CLI báo lỗi: {0}", res.Result))
	}
	return res.Result, nil
}

func (c *claudeCLI) Complete(ctx context.Context, r Request) (string, error) {
	args, stdin := c.args(r, "json")
	out, err := run(ctx, c.cfg, c.path, args, stdin, nil)
	if err != nil {
		return "", err
	}
	raw, ok := ExtractJSON(out)
	var res claudeResult
	if !ok || json.Unmarshal([]byte(raw), &res) != nil || res.Type != "result" {
		return "", errors.New(i18n.T("không đọc được kết quả từ Claude CLI"))
	}
	return claudeOutcome(res)
}

func (c *claudeCLI) Stream(ctx context.Context, r Request, onDelta func(string)) (string, error) {
	args, stdin := c.args(r, "stream-json")
	var final claudeResult
	var streamed strings.Builder
	_, err := run(ctx, c.cfg, c.path, args, stdin, func(line string) {
		var ev struct {
			Type  string `json:"type"`
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			return
		}
		switch ev.Type {
		case "stream_event":
			if ev.Event.Type == "content_block_delta" && ev.Event.Delta.Type == "text_delta" && ev.Event.Delta.Text != "" {
				streamed.WriteString(ev.Event.Delta.Text)
				onDelta(ev.Event.Delta.Text)
			}
		case "result":
			final = claudeResult{Type: "result", IsError: ev.IsError, Result: ev.Result}
		}
	})
	if err != nil {
		return "", err
	}
	if final.Type != "result" {
		return "", errors.New(i18n.T("Claude CLI kết thúc mà không trả kết quả"))
	}
	text, err := claudeOutcome(final)
	if err != nil {
		return "", err
	}
	if streamed.Len() == 0 && text != "" {
		onDelta(text)
	}
	if text == "" {
		text = streamed.String()
	}
	return text, nil
}

// ---- Codex CLI -------------------------------------------------------------

// codexCLI drives "codex exec". It could not be exercised against a real
// Codex install while this was written; the invocation follows the documented
// non-interactive mode, and a failure is surfaced to the user as a CLI error.
type codexCLI struct {
	path string
	cfg  Config
}

func (c *codexCLI) Name() string { return "codex-cli" }

func (c *codexCLI) Complete(ctx context.Context, r Request) (string, error) {
	tmp, err := os.CreateTemp("", "q3vigilai-codex-*.txt")
	if err != nil {
		return "", err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	args := []string{"exec", "--skip-git-repo-check", "--sandbox", "read-only", "--color", "never", "-o", tmp.Name()}
	if c.cfg.Model != "" {
		args = append(args, "-m", c.cfg.Model)
	}
	args = append(args, "-")
	stdin := r.Prompt
	if r.System != "" {
		stdin = r.System + "\n\n" + r.Prompt
	}
	out, err := run(ctx, c.cfg, c.path, args, stdin, nil)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "login") || strings.Contains(strings.ToLower(err.Error()), "auth") {
			return "", fmt.Errorf("%w: %v", ErrAuth, err)
		}
		return "", err
	}
	if b, err := os.ReadFile(tmp.Name()); err == nil && len(bytes.TrimSpace(b)) > 0 {
		return strings.TrimSpace(string(b)), nil
	}
	if strings.TrimSpace(out) == "" {
		return "", errors.New(i18n.T("Codex CLI không trả về nội dung"))
	}
	return strings.TrimSpace(out), nil
}

func (c *codexCLI) Stream(ctx context.Context, r Request, onDelta func(string)) (string, error) {
	text, err := c.Complete(ctx, r)
	if err == nil {
		onDelta(text)
	}
	return text, err
}
