package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{}

func postJSON(ctx context.Context, url, bearer string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("không kết nối được tới AI: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		err := fmt.Errorf("AI trả về HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, fmt.Errorf("%w: %v", ErrAuth, err)
		}
		return nil, err
	}
	return resp, nil
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func messages(r Request) []chatMsg {
	var m []chatMsg
	if r.System != "" {
		m = append(m, chatMsg{"system", r.System})
	}
	return append(m, chatMsg{"user", r.Prompt})
}

// ---- Ollama ----------------------------------------------------------------

type ollama struct{ cfg Config }

func ollamaBase(cfg Config) string {
	if cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/")
	}
	return "http://127.0.0.1:11434"
}

// OllamaRunning reports whether a local Ollama server answers.
func OllamaRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:11434/api/tags", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func (o *ollama) Name() string { return "ollama" }

// model returns the configured model, or the first one installed.
func (o *ollama) model(ctx context.Context) (string, error) {
	if o.cfg.Model != "" {
		return o.cfg.Model, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ollamaBase(o.cfg)+"/api/tags", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("không kết nối được tới Ollama: %w", err)
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&tags) != nil || len(tags.Models) == 0 {
		return "", errors.New("Ollama chưa có model nào; hãy chạy lệnh ollama pull để tải một model")
	}
	return tags.Models[0].Name, nil
}

func (o *ollama) Complete(ctx context.Context, r Request) (string, error) {
	return o.Stream(ctx, r, func(string) {})
}

func (o *ollama) Stream(ctx context.Context, r Request, onDelta func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, o.cfg.Timeout)
	defer cancel()
	model, err := o.model(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]any{"model": model, "messages": messages(r), "stream": true}
	if r.JSON {
		body["format"] = "json"
	}
	resp, err := postJSON(ctx, ollamaBase(o.cfg)+"/api/chat", "", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var ev struct {
			Message chatMsg `json:"message"`
			Error   string  `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Error != "" {
			return "", errors.New("Ollama báo lỗi: " + ev.Error)
		}
		if ev.Message.Content != "" {
			full.WriteString(ev.Message.Content)
			onDelta(ev.Message.Content)
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return full.String(), nil
}

// ---- OpenAI-compatible endpoint --------------------------------------------

// openAI talks to any endpoint that implements /chat/completions: hosted
// services of other vendors and local model servers.
type openAI struct{ cfg Config }

func openAIBase(cfg Config) string {
	if cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/")
	}
	return "https://api.openai.com/v1"
}

func (o *openAI) Name() string { return "http-openai" }

func (o *openAI) Complete(ctx context.Context, r Request) (string, error) {
	return o.Stream(ctx, r, func(string) {})
}

func (o *openAI) Stream(ctx context.Context, r Request, onDelta func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, o.cfg.Timeout)
	defer cancel()
	body := map[string]any{"model": o.cfg.Model, "messages": messages(r), "stream": true}
	if r.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	resp, err := postJSON(ctx, openAIBase(o.cfg)+"/chat/completions", o.cfg.APIKey, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var ev struct {
			Choices []struct {
				Delta chatMsg `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil || len(ev.Choices) == 0 {
			continue
		}
		if t := ev.Choices[0].Delta.Content; t != "" {
			full.WriteString(t)
			onDelta(t)
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return full.String(), nil
}
