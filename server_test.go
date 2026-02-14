package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func setupTest(t *testing.T) func() {
	t.Helper()
	return func() {}
}

// ── Free models ──

func TestFreeModelsEndpoint(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/models/free", nil)
	w := httptest.NewRecorder()
	apiFreeModels(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Count  int         `json:"count"`
		Models []FreeModel `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result.Count == 0 {
		t.Log("warning: no free models found (may be network issue)")
	}

	for _, m := range result.Models {
		if !strings.HasSuffix(m.ID, ":free") {
			t.Errorf("model %s doesn't end with :free", m.ID)
		}
	}
}

func TestFreeModelsEndpoint_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/models/free", nil)
	w := httptest.NewRecorder()
	apiFreeModels(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ── Router models ──

func TestRouterModelsEndpoint(t *testing.T) {
	if os.Getenv("OPENCODE_ZEN_API_KEY") == "" {
		t.Skip("OPENCODE_ZEN_API_KEY not set")
	}
	req := httptest.NewRequest("GET", "/api/models/zen", nil)
	w := httptest.NewRecorder()
	apiRouterModels(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Router string        `json:"router"`
		Count  int           `json:"count"`
		Models []RouterModel `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Count == 0 {
		t.Fatal("expected zen models, got 0")
	}
	if result.Router != "zen" {
		t.Errorf("expected router=zen, got %s", result.Router)
	}
}

func TestRouterModelsEndpoint_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/models/zen", nil)
	w := httptest.NewRecorder()
	apiRouterModels(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestRouterModelsEndpoint_NoKey(t *testing.T) {
	old := os.Getenv("OPENCODE_ZEN_API_KEY")
	os.Unsetenv("OPENCODE_ZEN_API_KEY")
	defer func() {
		if old != "" {
			os.Setenv("OPENCODE_ZEN_API_KEY", old)
		}
	}()

	c := getModelCache("zen")
	c.mu.Lock()
	c.models = nil
	c.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/models/zen", nil)
	w := httptest.NewRecorder()
	apiRouterModels(w, req)
	if w.Code != 502 {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestRouterModelsEndpoint_UnknownRouter(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/models/bogus", nil)
	w := httptest.NewRecorder()
	apiRouterModels(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── Router registry ──

func TestRouterRegistry(t *testing.T) {
	for _, id := range []string{"zen", "groq", "cerebras", "sambanova", "fireworks", "openrouter"} {
		if getRouter(id) == nil {
			t.Errorf("missing router: %s", id)
		}
	}
	if getRouter("nope") != nil {
		t.Error("expected nil for unknown router")
	}
}

// ── Dispatch ──

func TestDispatch_RouterField(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("POST", "/api/dispatch",
		strings.NewReader(`{"prompt":"hello","router":"bogus"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-tok")
	w := httptest.NewRecorder()
	apiDispatch(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for unknown router, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDispatch_NoRouterConfigured(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")
	saved := make(map[string]string)
	for _, rd := range routerDefs {
		if v := os.Getenv(rd.EnvKey); v != "" {
			saved[rd.EnvKey] = v
			os.Unsetenv(rd.EnvKey)
		}
	}
	defer func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	}()

	req := httptest.NewRequest("POST", "/api/dispatch",
		strings.NewReader(`{"prompt":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-tok")
	w := httptest.NewRecorder()
	apiDispatch(w, req)
	if w.Code != 502 {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDispatch_EmptyPrompt(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("POST", "/api/dispatch",
		strings.NewReader(`{"prompt":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-tok")
	w := httptest.NewRecorder()
	apiDispatch(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDispatch_Unauthorized(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("POST", "/api/dispatch",
		strings.NewReader(`{"prompt":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	apiDispatch(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestDispatch_MethodNotAllowed(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("GET", "/api/dispatch", nil)
	req.Header.Set("Authorization", "Bearer test-tok")
	w := httptest.NewRecorder()
	apiDispatch(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestJobHasRouterField(t *testing.T) {
	j := Job{ID: "1", Router: "zen", Model: "gpt-5-nano", Status: "done"}
	data, _ := json.Marshal(j)
	if !strings.Contains(string(data), `"router":"zen"`) {
		t.Fatalf("expected router field in JSON: %s", data)
	}
}

// ── OpenAI-compatible proxy ──

func v1Request(method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer test-v1-tok")
	w := httptest.NewRecorder()
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		v1ChatCompletions(w, req)
	case strings.HasSuffix(path, "/models"):
		v1Models(w, req)
	}
	return w
}

func TestV1Auth_Unauthorized(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	v1ChatCompletions(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestV1Auth_WrongToken(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	v1ChatCompletions(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestV1Auth_NoAuthMode(t *testing.T) {
	os.Setenv("CHOMP_V1_NO_AUTH", "1")
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_V1_NO_AUTH")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	v1ChatCompletions(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 (past auth), got %d: %s", w.Code, w.Body.String())
	}
}

func TestV1ChatCompletions_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	v1ChatCompletions(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestV1ChatCompletions_EmptyMessages(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")
	w := v1Request("POST", "/v1/chat/completions", `{"model":"auto","messages":[]}`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestV1ChatCompletions_InvalidJSON(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")
	w := v1Request("POST", "/v1/chat/completions", `not json`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestV1ChatCompletions_UnknownRouter(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")
	w := v1Request("POST", "/v1/chat/completions", `{"model":"x","router":"nope","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestV1ChatCompletions_NoRouterConfigured(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")
	saved := make(map[string]string)
	for _, rd := range routerDefs {
		if v := os.Getenv(rd.EnvKey); v != "" {
			saved[rd.EnvKey] = v
			os.Unsetenv(rd.EnvKey)
		}
	}
	defer func() { for k, v := range saved { os.Setenv(k, v) } }()

	w := v1Request("POST", "/v1/chat/completions", `{"messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 502 {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestV1Models_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/models", nil)
	w := httptest.NewRecorder()
	v1Models(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestV1Models_Unauthorized(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	v1Models(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestV1Models_ReturnsOpenAIFormat(t *testing.T) {
	os.Setenv("CHOMP_API_TOKEN", "test-v1-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")
	w := v1Request("GET", "/v1/models", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Object != "list" {
		t.Errorf("expected object=list, got %s", result.Object)
	}
	for _, m := range result.Data {
		if m.Object != "model" {
			t.Errorf("model %s has object=%s, want model", m.ID, m.Object)
		}
		if getRouter(m.OwnedBy) == nil {
			t.Errorf("model %s owned_by unknown router %s", m.ID, m.OwnedBy)
		}
	}
}

// ── Platforms ──

func TestApiPlatforms(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/platforms", nil)
	w := httptest.NewRecorder()
	apiPlatforms(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var statuses []PlatformStatus
	json.Unmarshal(w.Body.Bytes(), &statuses)
	if len(statuses) < 6 {
		t.Fatalf("expected 6+ platforms, got %d", len(statuses))
	}
}

// ── Config ──

func TestApiConfig(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	apiConfig(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var cfg struct {
		Routers map[string]RouterConfig `json:"routers"`
	}
	json.Unmarshal(w.Body.Bytes(), &cfg)
	if len(cfg.Routers) < 6 {
		t.Fatalf("expected 6+ routers, got %d", len(cfg.Routers))
	}
}

// ── Root ──

func TestApiRoot(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	apiRoot(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["name"] != "chomp" {
		t.Fatalf("expected name=chomp, got %v", result["name"])
	}
}

func TestApiRoot_404(t *testing.T) {
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	apiRoot(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
