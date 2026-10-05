package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultAnthropicModel is used when the user sets no model.
const DefaultAnthropicModel = "claude-opus-5-5"

// fallbackModels are the models that accept the server-side refusal
// fallback: a request the safety classifiers decline is re-served by another
// model inside the same call instead of failing.
var fallbackModels = map[string]bool{"claude-opus-5-5": true, "claude-opus-5": true, "claude-fable-5-1": true, "claude-sonnet-5-5": true}

type anthropicAPI struct {
	cfg    Config
	client anthropic.Client
}

func newAnthropic(cfg Config) *anthropicAPI {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &anthropicAPI{cfg: cfg, client: anthropic.NewClient(opts...)}
}

func (a *anthropicAPI) Name() string { return "http-anthropic" }

func (a *anthropicAPI) Complete(ctx context.Context, r Request) (string, error) {
	return a.Stream(ctx, r, func(string) {})
}

// Stream always uses the streaming endpoint, which keeps long generations
// clear of HTTP timeouts. Thinking is left at the model's default (adaptive).
func (a *anthropicAPI) Stream(ctx context.Context, r Request, onDelta func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()
	model := a.cfg.Model
	if model == "" {
		model = DefaultAnthropicModel
	}
	// Thinking tokens count against max_tokens, so the cap stays generous
	// even for short answers.
	maxTokens := int64(16000)
	if r.MaxTokens > 16000 {
		maxTokens = int64(r.MaxTokens)
	}
	params := anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(r.Prompt))},
	}
	if r.System != "" {
		params.System = []anthropic.BetaTextBlockParam{{Text: r.System}}
	}
	if fallbackModels[model] && a.cfg.BaseURL == "" {
		// "default" routes by refusal category, so no model list is kept here.
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}

	stream := a.client.Beta.Messages.NewStreaming(ctx, params)
	message := anthropic.BetaMessage{}
	var full strings.Builder
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return "", err
		}
		if ev, ok := event.AsAny().(anthropic.BetaRawContentBlockDeltaEvent); ok {
			if d, ok := ev.Delta.AsAny().(anthropic.BetaTextDelta); ok && d.Text != "" {
				full.WriteString(d.Text)
				onDelta(d.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", anthropicErr(err)
	}
	if message.StopReason == anthropic.BetaStopReasonRefusal {
		return "", fmt.Errorf("%w (%s)", ErrRefused, message.StopDetails.Category)
	}
	if message.StopReason == anthropic.BetaStopReasonMaxTokens && full.Len() == 0 {
		return "", errors.New("câu trả lời bị cắt do vượt giới hạn độ dài")
	}
	return full.String(), nil
}

func anthropicErr(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401, 403:
			return fmt.Errorf("%w: khóa API Anthropic không hợp lệ hoặc không có quyền (HTTP %d)", ErrAuth, apierr.StatusCode)
		case 404:
			return fmt.Errorf("Anthropic API không nhận ra model đã chọn (HTTP 404)")
		case 429:
			return errors.New("Anthropic API đang giới hạn tốc độ, hãy thử lại sau (HTTP 429)")
		default:
			return fmt.Errorf("Anthropic API lỗi HTTP %d", apierr.StatusCode)
		}
	}
	return fmt.Errorf("không kết nối được tới Anthropic API: %w", err)
}
