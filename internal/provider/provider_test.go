package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type modelTestTransport func(*http.Request) (*http.Response, error)

func (f modelTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestChatRequestModelOptions(t *testing.T) {
	for _, tc := range []struct {
		name, provider, baseURL, model, option, value string
	}{
		{name: "DeepSeek Flash", provider: "deepseek", model: "deepseek-flash", option: "thinking", value: `{"type":"disabled"}`},
		{name: "OpenAI Luna", provider: "openai", model: "gpt-6-luna", option: "reasoning_effort", value: `"none"`},
		{name: "Compatible Luna", provider: "openai", baseURL: "https://example.com/v1", model: "gpt-6-luna", option: "reasoning_effort"},
		{name: "Qwen Flash", provider: "qwen", model: "qwen3.8-flash", option: "enable_thinking", value: `false`},
		{name: "OpenRouter Luna", provider: "openrouter", model: "openai/gpt-6-luna", option: "reasoning", value: `{"effort":"none"}`},
		{name: "Grok 4.3", provider: "grok", model: "grok-4.3", option: "reasoning_effort", value: `"none"`},
		{name: "Grok 4.7", provider: "grok", model: "grok-4.7", option: "reasoning_effort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := Get(tc.provider)
			s := p.Default
			s.Model = tc.model
			if tc.baseURL != "" {
				s.BaseURL = tc.baseURL
			}
			c := Client{http: &http.Client{Transport: modelTestTransport(func(req *http.Request) (*http.Response, error) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					return nil, err
				}
				if got := string(body["model"]); got != `"`+tc.model+`"` {
					t.Errorf("request model = %s, want %q", got, tc.model)
				}
				if got := string(body[tc.option]); got != tc.value {
					t.Errorf("request %s = %q, want %q", tc.option, got, tc.value)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"Hello"}}]}`))}, nil
			})}}
			if _, err := c.Translate(context.Background(), p, s, "test-key", Input{Text: "你好", Target: "en"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
