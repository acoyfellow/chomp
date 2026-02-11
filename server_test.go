package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTest(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	stateFile = filepath.Join(dir, "state.json")
	keysFile = filepath.Join(dir, "keys.json")
	agentsFile = filepath.Join(dir, "agents.json")

	// Reset cache
	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()

	// Parse templates
	var err error
	tmpl, err = template.New("").ParseFS(templateFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		t.Fatal(err)
	}

	return func() { os.RemoveAll(dir) }
}

func postJSON(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func postForm(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func getReq(handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// ── Task CRUD ──

func TestAddTask_JSON(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiAddTask, `{"prompt":"do stuff","dir":"/tmp"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var task Task
	json.Unmarshal(w.Body.Bytes(), &task)
	if task.Prompt != "do stuff" {
		t.Fatalf("expected prompt 'do stuff', got %q", task.Prompt)
	}
	if task.Dir != "/tmp" {
		t.Fatalf("expected dir '/tmp', got %q", task.Dir)
	}
	if task.Status != "queued" {
		t.Fatalf("expected status 'queued', got %q", task.Status)
	}
	if task.ID != "1" {
		t.Fatalf("expected id '1', got %q", task.ID)
	}
}

func TestAddTask_EmptyPrompt(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiAddTask, `{"prompt":""}`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddTask_MethodNotAllowed(t *testing.T) {
	defer setupTest(t)()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	apiAddTask(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestRunTask_JSON(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"test"}`)
	w := postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s, _ := readStateUnsafe()
	if s.Tasks[0].Status != "active" {
		t.Fatalf("expected status 'active', got %q", s.Tasks[0].Status)
	}
	if s.Tasks[0].Platform != "shelley" {
		t.Fatalf("expected platform 'shelley', got %q", s.Tasks[0].Platform)
	}
}

func TestRunTask_Form(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"test"}`)
	w := postForm(apiRunTask, "id=1&agent=opencode")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s, _ := readStateUnsafe()
	if s.Tasks[0].Status != "active" {
		t.Fatalf("expected active, got %q", s.Tasks[0].Status)
	}
}

func TestRunTask_NotFound(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiRunTask, `{"id":"999"}`)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDoneTask(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"test"}`)
	postJSON(apiRunTask, `{"id":"1"}`)
	w := postJSON(apiDoneTask, `{"id":"1","result":"it worked"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s, _ := readStateUnsafe()
	if s.Tasks[0].Status != "done" {
		t.Fatalf("expected done, got %q", s.Tasks[0].Status)
	}
	if s.Tasks[0].Result != "it worked" {
		t.Fatalf("expected result, got %q", s.Tasks[0].Result)
	}
}

func TestDeleteTask_JSON(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"delete me"}`)
	w := postJSON(apiDeleteTask, `{"id":"1"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s, _ := readStateUnsafe()
	if len(s.Tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(s.Tasks))
	}
}

func TestDeleteTask_Form(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"delete me"}`)
	w := postForm(apiDeleteTask, "id=1")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	s, _ := readStateUnsafe()
	if len(s.Tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(s.Tasks))
	}
}

func TestDeleteTask_MissingID(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiDeleteTask, `{}`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── State ──

func TestGetState_Empty(t *testing.T) {
	defer setupTest(t)()
	w := getReq(apiState, "/api/state")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var s State
	json.Unmarshal(w.Body.Bytes(), &s)
	if len(s.Tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(s.Tasks))
	}
}

func TestGetState_WithTasks(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"one"}`)
	postJSON(apiAddTask, `{"prompt":"two"}`)
	w := getReq(apiState, "/api/state")
	var s State
	json.Unmarshal(w.Body.Bytes(), &s)
	if len(s.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(s.Tasks))
	}
	if s.NextID != 3 {
		t.Fatalf("expected next_id 3, got %d", s.NextID)
	}
}

// ── Task lifecycle ──

func TestFullLifecycle(t *testing.T) {
	defer setupTest(t)()
	// Add
	postJSON(apiAddTask, `{"prompt":"lifecycle test"}`)
	s, _ := readStateUnsafe()
	if s.Tasks[0].Status != "queued" {
		t.Fatal("not queued")
	}
	// Run
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)
	s, _ = readStateUnsafe()
	if s.Tasks[0].Status != "active" {
		t.Fatal("not active")
	}
	// Done
	postJSON(apiDoneTask, `{"id":"1","result":"done!"}`)
	s, _ = readStateUnsafe()
	if s.Tasks[0].Status != "done" {
		t.Fatal("not done")
	}
	// Delete
	postJSON(apiDeleteTask, `{"id":"1"}`)
	s, _ = readStateUnsafe()
	if len(s.Tasks) != 0 {
		t.Fatal("not deleted")
	}
}

// ── Config keys ──

func TestConfigKeys_Set(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiConfigKeys, `{"key":"OPENROUTER_API_KEY","value":"sk-test"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if os.Getenv("OPENROUTER_API_KEY") != "sk-test" {
		t.Fatal("env not set")
	}
	// Verify persisted
	keys, _ := readKeys()
	if keys["OPENROUTER_API_KEY"] != "sk-test" {
		t.Fatal("not persisted")
	}
	// Clean up
	os.Unsetenv("OPENROUTER_API_KEY")
}

func TestConfigKeys_Delete(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiConfigKeys, `{"key":"OPENROUTER_API_KEY","value":"sk-test"}`)
	w := postJSON(apiConfigKeys, `{"key":"OPENROUTER_API_KEY","value":""}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		t.Fatal("env not cleared")
	}
}

func TestConfigKeys_BadKey(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiConfigKeys, `{"key":"EVIL_KEY","value":"hack"}`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── Config endpoint ──

func TestGetConfig(t *testing.T) {
	defer setupTest(t)()
	w := getReq(apiConfig, "/api/config")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var cfg ConfigResponse
	json.Unmarshal(w.Body.Bytes(), &cfg)
	if len(cfg.Agents) == 0 {
		t.Fatal("no agents")
	}
	if len(cfg.Routers) == 0 {
		t.Fatal("no routers")
	}
	// Shelley should always be available
	if a, ok := cfg.Agents["shelley"]; !ok || !a.Available {
		t.Fatal("shelley should be available")
	}
}

// ── Custom agents ──

func TestCustomAgents_Add(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiConfigAgents, `{"id":"my-agent","name":"My Agent","command":"echo","models":["gpt-4"],"color":"#FF0000"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	agents, _ := readCustomAgents()
	if _, ok := agents["my-agent"]; !ok {
		t.Fatal("agent not saved")
	}
}

func TestCustomAgents_CantOverwriteBuiltin(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiConfigAgents, `{"id":"shelley","name":"Fake","command":"echo"}`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCustomAgents_Delete(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiConfigAgents, `{"id":"my-agent","name":"My Agent","command":"echo"}`)
	req := httptest.NewRequest("DELETE", "/", strings.NewReader(`{"id":"my-agent"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	apiConfigAgents(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	agents, _ := readCustomAgents()
	if _, ok := agents["my-agent"]; ok {
		t.Fatal("agent not deleted")
	}
}

func TestCustomAgents_CantDeleteBuiltin(t *testing.T) {
	defer setupTest(t)()
	req := httptest.NewRequest("DELETE", "/", strings.NewReader(`{"id":"shelley"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	apiConfigAgents(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCustomAgents_BadID(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiConfigAgents, `{"id":"BAD ID!","name":"Test","command":"echo"}`)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCustomAgents_MergedList(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiConfigAgents, `{"id":"custom","name":"Custom","command":"echo","models":["m1"]}`)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	apiConfigAgents(w, req)
	var agents map[string]AgentConfig
	json.Unmarshal(w.Body.Bytes(), &agents)
	if _, ok := agents["shelley"]; !ok {
		t.Fatal("missing shelley")
	}
	if _, ok := agents["custom"]; !ok {
		t.Fatal("missing custom agent")
	}
}

// ── Balance ──

func TestBalance(t *testing.T) {
	defer setupTest(t)()
	balanceMu.Lock()
	balanceCached = nil
	balanceMu.Unlock()

	w := getReq(apiBalance, "/api/balance")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var bal BalanceResponse
	json.Unmarshal(w.Body.Bytes(), &bal)
	if len(bal.Providers) == 0 {
		t.Fatal("no providers")
	}
	// Shelley should always be present and configured
	found := false
	for _, p := range bal.Providers {
		if p.ID == "shelley" {
			found = true
			if !p.Configured {
				t.Fatal("shelley should be configured")
			}
			if p.DailyTokens != 1_000_000 {
				t.Fatalf("expected 1M daily tokens, got %d", p.DailyTokens)
			}
			if p.DailyUSD < 2.0 {
				t.Fatalf("expected ~$3/day, got $%.2f", p.DailyUSD)
			}
		}
	}
	if !found {
		t.Fatal("missing shelley provider")
	}
	// Total should be at least shelley's contribution
	if bal.TotalDailyTokens < 1_000_000 {
		t.Fatalf("expected at least 1M total daily, got %d", bal.TotalDailyTokens)
	}
	if bal.TotalDailyUSD < 2.0 {
		t.Fatalf("expected at least $2/day, got $%.2f", bal.TotalDailyUSD)
	}
}

// ── Partials (template rendering) ──

func TestPartialBalance(t *testing.T) {
	defer setupTest(t)()
	balanceMu.Lock()
	balanceCached = nil
	balanceMu.Unlock()

	w := getReq(partialsBalance, "/partials/balance")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Daily Token Budget") && !strings.Contains(body, "Available Balance") {
		t.Fatalf("missing balance header in: %s", body[:200])
	}
}

func TestPartialTasks_Active(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"active test"}`)
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)
	postJSON(apiAddTask, `{"prompt":"queued test"}`)

	req := httptest.NewRequest("GET", "/partials/tasks?tab=active", nil)
	w := httptest.NewRecorder()
	partialsTasks(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "active test") {
		t.Fatal("missing active task")
	}
	if !strings.Contains(body, "queued test") {
		t.Fatal("missing queued task")
	}
	if !strings.Contains(body, "Waiting") {
		t.Fatal("missing Waiting divider")
	}
}

func TestPartialTasks_Completed(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"done test"}`)
	postJSON(apiRunTask, `{"id":"1"}`)
	postJSON(apiDoneTask, `{"id":"1","result":"ok"}`)

	req := httptest.NewRequest("GET", "/partials/tasks?tab=completed", nil)
	w := httptest.NewRecorder()
	partialsTasks(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "done test") {
		t.Fatal("missing done task")
	}
}

func TestPartialTasks_Empty(t *testing.T) {
	defer setupTest(t)()
	req := httptest.NewRequest("GET", "/partials/tasks?tab=active", nil)
	w := httptest.NewRecorder()
	partialsTasks(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "Nothing here yet") {
		t.Fatal("missing empty state")
	}
}

func TestPartialDetail(t *testing.T) {
	defer setupTest(t)()
	postJSON(apiAddTask, `{"prompt":"detail test"}`)
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)

	req := httptest.NewRequest("GET", "/partials/detail/1", nil)
	w := httptest.NewRecorder()
	partialsDetail(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "detail test") {
		t.Fatal("missing task prompt")
	}
	if !strings.Contains(body, "Shelley") {
		t.Fatal("missing agent name")
	}
}

func TestPartialDetail_NotFound(t *testing.T) {
	defer setupTest(t)()
	req := httptest.NewRequest("GET", "/partials/detail/999", nil)
	w := httptest.NewRecorder()
	partialsDetail(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPartialSettings(t *testing.T) {
	defer setupTest(t)()
	w := getReq(partialsSettings, "/partials/settings")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Settings") {
		t.Fatal("missing title")
	}
	if !strings.Contains(body, "Shelley") {
		t.Fatal("missing shelley agent")
	}
	if !strings.Contains(body, "API Keys") || !strings.Contains(body, "OPENROUTER_API_KEY") {
		t.Fatal("missing key info")
	}
}

func TestPartialCreate(t *testing.T) {
	defer setupTest(t)()
	w := getReq(partialsCreate, "/partials/create")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "New Task") {
		t.Fatal("missing form")
	}
}

// ── Page ──

func TestPageIndex(t *testing.T) {
	defer setupTest(t)()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	pageIndex(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatal("missing doctype")
	}
	if !strings.Contains(body, "htmx") {
		t.Fatal("missing htmx")
	}
	if !strings.Contains(body, "chomp") {
		t.Fatal("missing brand")
	}
}

func TestPageIndex_404(t *testing.T) {
	defer setupTest(t)()
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	pageIndex(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestServeCSS(t *testing.T) {
	defer setupTest(t)()
	w := getReq(serveCSS, "/static/style.css")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/css" {
		t.Fatalf("expected text/css, got %s", w.Header().Get("Content-Type"))
	}
	if w.Body.Len() == 0 {
		t.Fatal("empty css")
	}
}

// ── Helpers ──

func TestFmtTokens(t *testing.T) {
	cases := []struct{ in int; want string }{
		{0, "0"}, {500, "500"}, {1500, "1k"}, {1234567, "1.2M"},
	}
	for _, c := range cases {
		got := fmtTokens(c.in)
		if got != c.want {
			t.Errorf("fmtTokens(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTimeAgo(t *testing.T) {
	if timeAgo("") != "" {
		t.Error("empty should return empty")
	}
	if timeAgo("garbage") != "" {
		t.Error("bad input should return empty")
	}
}

func TestIsStale(t *testing.T) {
	if isStale("", 5) {
		t.Error("empty should not be stale")
	}
	old := "2020-01-01T00:00:00Z"
	if !isStale(old, 5) {
		t.Error("old timestamp should be stale")
	}
}
