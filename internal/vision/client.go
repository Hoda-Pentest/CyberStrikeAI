package vision

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/openai"
)

// Client calls a standalone Vision ChatModel (a single Generate).
type Client struct {
	cfg    config.VisionConfig
	mainOA config.OpenAIConfig
}

// NewClient constructs the vision client.
func NewClient(visionCfg config.VisionConfig, mainOpenAI config.OpenAIConfig) *Client {
	return &Client{cfg: visionCfg, mainOA: mainOpenAI}
}

// Analyze sends the image bytes into the VL model and returns a text description.
func (c *Client) Analyze(ctx context.Context, img ImagePayload, question string) (string, error) {
	if len(img.Bytes) == 0 {
		return "", fmt.Errorf("empty image payload")
	}
	mime := strings.TrimSpace(img.MIMEType)
	if mime == "" {
		mime = "image/jpeg"
	}
	oa := c.cfg.OpenAICfgEffective(c.mainOA)
	if strings.TrimSpace(oa.APIKey) == "" {
		return "", fmt.Errorf("vision API key is empty (set vision.api_key or openai.api_key)")
	}
	if strings.TrimSpace(oa.Model) == "" {
		return "", fmt.Errorf("vision model is empty")
	}

	timeout := time.Duration(c.cfg.TimeoutSecondsEffective()) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpClient := &http.Client{
		Timeout: timeout + 15*time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   60 * time.Second,
				KeepAlive: 60 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: timeout + 10*time.Second,
		},
	}

	b64 := base64.StdEncoding.EncodeToString(img.Bytes)
	payload := map[string]any{"model": oa.Model, "max_completion_tokens": oa.MaxCompletionTokensEffective(), "messages": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": buildVisionPrompt(question)}, {"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + b64, "detail": c.cfg.DetailEffective()}}}}}}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	client := openai.NewClient(&oa, httpClient, nil)
	if err := client.ChatCompletion(ctx, payload, &reply); err != nil {
		return "", err
	}
	if len(reply.Choices) == 0 || strings.TrimSpace(reply.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("vision model returned empty content")
	}
	return strings.TrimSpace(reply.Choices[0].Message.Content), nil
}

func buildVisionPrompt(question string) string {
	q := strings.TrimSpace(question)
	if q == "" {
		q = "Give a general description of the image, focusing on an authorized security-testing scenario (visible text, forms, buttons, captchas, error messages, technology-stack clues)."
	}
	extra := ""
	if looksLikeCaptchaQuestion(q) {
		extra = "\nIf this is a captcha: output only the character sequence you recognize, with no spaces, punctuation, or explanation; if it is illegible, say clearly that you cannot recognize it."
	}
	return `You are an authorized security-testing assistant. Answer the user's question based on the image, describing only what you can confirm from the image; do not make anything up.
User question: ` + q + extra
}

func looksLikeCaptchaQuestion(q string) bool {
	s := strings.ToLower(q)
	for _, kw := range []string{"captcha", "verification code", "verify code", "vcode", "graphic code"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return strings.Contains(s, "only output") && strings.Contains(s, "character")
}
