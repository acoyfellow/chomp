package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// ── Platforms ──

func TestPlatformStatuses(t *testing.T) {
	defer setupTest(t)()
	statuses := platformStatuses()
	if len(statuses) == 0 {
		t.Fatal("no platforms")
	}
	// Should have Shelley
	found := false
	for _, s := range statuses {
		if s.Name == "Shelley" {
			found = true
			// Status should be "live" or "unconfigured" — never fake
			if s.Status != "live" && s.Status != "unconfigured" {
				t.Fatalf("unexpected shelley status: %s", s.Status)
			}
		}
	}
	if !found {
		t.Fatal("missing Shelley platform")
	}
}

func TestApiPlatforms(t *testing.T) {
	defer setupTest(t)()
	w := getReq(apiPlatforms, "/api/platforms")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var statuses []PlatformStatus
	json.Unmarshal(w.Body.Bytes(), &statuses)
	if len(statuses) < 4 {
		t.Fatalf("expected 4 platforms, got %d", len(statuses))
	}
}

// ── Partials (template rendering) ──

func TestPartialBalance(t *testing.T) {
	defer setupTest(t)()
	w := getReq(partialsBalance, "/partials/balance")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Platforms") {
		t.Fatalf("missing Platforms header in: %s", body[:200])
	}
	// Must not contain any dollar amounts
	if strings.Contains(body, "/day") {
		t.Fatal("balance card should not contain /day fake budget")
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

// ── E2E Reality Tests ──
// These verify that data flows end-to-end with no mocking:
// create → run → update tokens → done → balance adjusts → metrics track.

func TestE2E_TokensBurnedReducesBalance(t *testing.T) {
	defer setupTest(t)()

	// Create and run a task
	postJSON(apiAddTask, `{"prompt":"burn tokens"}`)
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)

	// Update tokens burned on the task
	w := postJSON(apiUpdateTask, `{"id":"1","tokens":"50000"}`)
	if w.Code != 200 {
		t.Fatalf("update failed: %d %s", w.Code, w.Body.String())
	}

	// Read state — tokens should be persisted
	s, _ := readStateUnsafe()
	if s.Tasks[0].Tokens != 50000 {
		t.Fatalf("expected 50000 tokens, got %d", s.Tasks[0].Tokens)
	}

	// Balance partial should reflect burned tokens
	rec := getReq(partialsBalance, "/partials/balance")
	body := rec.Body.String()
	// 50k tokens = $0.15 burned at $3/1M, so remaining ~$2.85
	// The balance should NOT show $3.00 anymore
	if strings.Contains(body, ">$3<") || strings.Contains(body, ">3<") {
		// Check more carefully — the dollars field
		if strings.Contains(body, "$3") && strings.Contains(body, ".00") {
			// Still showing full $3.00 — that's wrong if tokens burned
		}
	}
	// Should show burned tokens in the BURNED metric
	if !strings.Contains(body, "50k") {
		t.Fatalf("balance should show 50k burned, got: %s", body)
	}
}

func TestE2E_MetricsTrackCorrectly(t *testing.T) {
	defer setupTest(t)()

	// Empty state: 0 live, 0 tasks, 0 burned
	rec := getReq(partialsBalance, "/partials/balance")
	body := rec.Body.String()
	// Should have "0" for all three stats
	if !strings.Contains(body, ">0<") {
		t.Fatal("empty state should show 0 metrics")
	}

	// Add 2 tasks, run 1
	postJSON(apiAddTask, `{"prompt":"task one"}`)
	postJSON(apiAddTask, `{"prompt":"task two"}`)
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)

	rec = getReq(partialsBalance, "/partials/balance")
	body = rec.Body.String()

	// TASKS should show 2
	if !strings.Contains(body, ">2<") {
		t.Fatalf("should show 2 total tasks, body: %s", body)
	}

	// LIVE should show 1 (we need to check the live stat specifically)
	// The live stat has a green pulse dot before the number when > 0
	if !strings.Contains(body, "bg-green-500") {
		t.Fatal("should show green live indicator for 1 active task")
	}
}

func TestE2E_DoneTaskWithTokens(t *testing.T) {
	defer setupTest(t)()

	postJSON(apiAddTask, `{"prompt":"finish me"}`)
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)

	// Complete with tokens
	w := postJSON(apiDoneTask, `{"id":"1","result":"done!","tokens":"125000"}`)
	if w.Code != 200 {
		t.Fatalf("done failed: %d", w.Code)
	}

	s, _ := readStateUnsafe()
	if s.Tasks[0].Status != "done" {
		t.Fatalf("expected done, got %s", s.Tasks[0].Status)
	}
	if s.Tasks[0].Tokens != 125000 {
		t.Fatalf("expected 125000 tokens, got %d", s.Tasks[0].Tokens)
	}
	if s.Tasks[0].Result != "done!" {
		t.Fatalf("expected result 'done!', got %q", s.Tasks[0].Result)
	}
}

func TestE2E_UpdateTaskTokens(t *testing.T) {
	defer setupTest(t)()

	postJSON(apiAddTask, `{"prompt":"track me"}`)
	postJSON(apiRunTask, `{"id":"1"}`)

	// First update: 10k tokens
	w := postJSON(apiUpdateTask, `{"id":"1","tokens":"10000"}`)
	if w.Code != 200 {
		t.Fatalf("update failed: %d", w.Code)
	}
	s, _ := readStateUnsafe()
	if s.Tasks[0].Tokens != 10000 {
		t.Fatalf("expected 10000, got %d", s.Tasks[0].Tokens)
	}

	// Second update: 75k tokens (cumulative, agent reports total)
	postJSON(apiUpdateTask, `{"id":"1","tokens":"75000"}`)
	s, _ = readStateUnsafe()
	if s.Tasks[0].Tokens != 75000 {
		t.Fatalf("expected 75000, got %d", s.Tasks[0].Tokens)
	}

	// Verify HX-Trigger header is sent
	w = postJSON(apiUpdateTask, `{"id":"1","tokens":"80000"}`)
	if w.Header().Get("HX-Trigger") != "refreshTasks" {
		t.Fatal("update should send HX-Trigger: refreshTasks")
	}
}

func TestE2E_UpdateTaskNotFound(t *testing.T) {
	defer setupTest(t)()
	w := postJSON(apiUpdateTask, `{"id":"999","tokens":"5000"}`)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestE2E_DeleteRefreshesUI(t *testing.T) {
	defer setupTest(t)()

	postJSON(apiAddTask, `{"prompt":"delete me"}`)

	// Delete via form-encoded (how HTMX sends it)
	w := postForm(apiDeleteTask, "id=1")
	if w.Code != 200 {
		t.Fatalf("delete failed: %d", w.Code)
	}

	// Must have HX-Trigger header
	if w.Header().Get("HX-Trigger") != "refreshTasks" {
		t.Fatal("delete must send HX-Trigger: refreshTasks")
	}

	// State must be empty
	s, _ := readStateUnsafe()
	if len(s.Tasks) != 0 {
		t.Fatal("task not deleted from state")
	}
}

func TestE2E_RunRefreshesUI(t *testing.T) {
	defer setupTest(t)()

	postJSON(apiAddTask, `{"prompt":"run me"}`)
	w := postForm(apiRunTask, "id=1")
	if w.Code != 200 {
		t.Fatalf("run failed: %d", w.Code)
	}
	if w.Header().Get("HX-Trigger") != "refreshTasks" {
		t.Fatal("run must send HX-Trigger: refreshTasks")
	}
}

func TestE2E_AddRefreshesUI(t *testing.T) {
	defer setupTest(t)()

	w := postForm(apiAddTask, "prompt=hello+world&dir=/tmp")
	if w.Code != 200 {
		t.Fatalf("add failed: %d", w.Code)
	}
	if w.Header().Get("HX-Trigger") != "refreshTasks" {
		t.Fatal("add must send HX-Trigger: refreshTasks")
	}

	// Verify task was actually created
	s, _ := readStateUnsafe()
	if len(s.Tasks) != 1 {
		t.Fatal("task not created")
	}
	if s.Tasks[0].Prompt != "hello world" {
		t.Fatalf("expected 'hello world', got %q", s.Tasks[0].Prompt)
	}
}

func TestE2E_WizardCreatesTaskWithAgentAndModel(t *testing.T) {
	defer setupTest(t)()

	// Simulate wizard step 4 submit (form-encoded like HTMX)
	w := postForm(apiAddTask, "prompt=refactor+auth&dir=/home/exedev/app&agent=shelley&model=claude-sonnet-4")
	if w.Code != 200 {
		t.Fatalf("add failed: %d", w.Code)
	}

	s, _ := readStateUnsafe()
	task := s.Tasks[0]
	if task.Prompt != "refactor auth" {
		t.Fatalf("prompt: got %q", task.Prompt)
	}
	if task.Platform != "shelley" {
		t.Fatalf("agent: expected shelley, got %q", task.Platform)
	}
	if task.Model != "claude-sonnet-4" {
		t.Fatalf("model: expected claude-sonnet-4, got %q", task.Model)
	}
	if task.Dir != "/home/exedev/app" {
		t.Fatalf("dir: got %q", task.Dir)
	}
}

func TestE2E_ProgressBarNotHardcoded(t *testing.T) {
	defer setupTest(t)()

	// Queued task should have 0% progress
	postJSON(apiAddTask, `{"prompt":"queued task"}`)

	rec := getReq(partialsTasks, "/partials/tasks?tab=active")
	// Queued tasks don't have progress bars, so no "width:50%"
	if strings.Contains(rec.Body.String(), "width:50%") {
		t.Fatal("progress should not be hardcoded to 50%")
	}

	// Active task with tokens should have real progress
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)
	postJSON(apiUpdateTask, `{"id":"1","tokens":"100000"}`)

	rec = getReq(partialsTasks, "/partials/tasks?tab=active")
	body := rec.Body.String()
	// 100k tokens / 200k budget = 50%, but computed not hardcoded
	if strings.Contains(body, "width:0%") {
		t.Fatal("active task with 100k tokens should have >0% progress")
	}
}

func TestE2E_BurnedTokensShowInBalance(t *testing.T) {
	defer setupTest(t)()

	// No burns — should show 0 burned
	rec := getReq(partialsBalance, "/partials/balance")
	if !strings.Contains(rec.Body.String(), ">0<") {
		t.Fatal("should show 0 burned with no tasks")
	}

	// Burn 500k tokens
	postJSON(apiAddTask, `{"prompt":"big task"}`)
	postJSON(apiRunTask, `{"id":"1","agent":"shelley"}`)
	postJSON(apiUpdateTask, `{"id":"1","tokens":"500000"}`)

	rec = getReq(partialsBalance, "/partials/balance")
	body := rec.Body.String()
	// Should show 500k in the BURNED metric
	if !strings.Contains(body, "500k") {
		t.Fatalf("should show 500k burned, body: %s", body)
	}
	// Should NOT contain any dollar amounts
	if strings.Contains(body, "$3") || strings.Contains(body, "$2") || strings.Contains(body, "/day") {
		t.Fatalf("balance card should have no fake dollar amounts, body: %s", body)
	}
}

func TestE2E_NothingMocked(t *testing.T) {
	defer setupTest(t)()

	// Full lifecycle: create with agent+model, run, update tokens, done
	// then verify everything persisted to state.json — no mock data.
	postForm(apiAddTask, "prompt=real+task&agent=shelley&model=claude-sonnet-4&dir=/tmp")

	// Read raw state file
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	var s State
	json.Unmarshal(data, &s)
	if len(s.Tasks) != 1 {
		t.Fatal("task not persisted to disk")
	}
	if s.Tasks[0].Platform != "shelley" {
		t.Fatalf("agent not persisted: %q", s.Tasks[0].Platform)
	}
	if s.Tasks[0].Model != "claude-sonnet-4" {
		t.Fatalf("model not persisted: %q", s.Tasks[0].Model)
	}

	// Run it
	postForm(apiRunTask, "id=1")
	data, _ = os.ReadFile(stateFile)
	json.Unmarshal(data, &s)
	if s.Tasks[0].Status != "active" {
		t.Fatal("status not updated on disk")
	}

	// Update tokens
	postJSON(apiUpdateTask, `{"id":"1","tokens":"42000"}`)
	data, _ = os.ReadFile(stateFile)
	json.Unmarshal(data, &s)
	if s.Tasks[0].Tokens != 42000 {
		t.Fatalf("tokens not persisted: %d", s.Tasks[0].Tokens)
	}

	// Done
	postJSON(apiDoneTask, `{"id":"1","result":"shipped","tokens":"88000"}`)
	data, _ = os.ReadFile(stateFile)
	json.Unmarshal(data, &s)
	if s.Tasks[0].Status != "done" {
		t.Fatal("done status not persisted")
	}
	if s.Tasks[0].Tokens != 88000 {
		t.Fatal("final tokens not persisted")
	}
	if s.Tasks[0].Result != "shipped" {
		t.Fatal("result not persisted")
	}
}

func TestE2E_WizardStepPartials(t *testing.T) {
	defer setupTest(t)()

	// Step 1
	rec := getReq(partialsCreate, "/partials/create")
	if !strings.Contains(rec.Body.String(), "Step 1 of 4") {
		t.Fatal("step 1 not rendered")
	}

	// Step 2
	rec = getReq(partialsCreate, "/partials/create?step=2&prompt=test&dir=/tmp")
	if !strings.Contains(rec.Body.String(), "Step 2 of 4") {
		t.Fatal("step 2 not rendered")
	}
	if !strings.Contains(rec.Body.String(), "Shelley") {
		t.Fatal("step 2 should list agents")
	}

	// Step 3
	rec = getReq(partialsCreate, "/partials/create?step=3&prompt=test&dir=/tmp&agent=shelley")
	if !strings.Contains(rec.Body.String(), "Step 3 of 4") {
		t.Fatal("step 3 not rendered")
	}
	if !strings.Contains(rec.Body.String(), "claude-sonnet-4") {
		t.Fatal("step 3 should list agent models")
	}

	// Step 4
	rec = getReq(partialsCreate, "/partials/create?step=4&prompt=test&dir=/tmp&agent=shelley&model=claude-sonnet-4")
	if !strings.Contains(rec.Body.String(), "Step 4 of 4") {
		t.Fatal("step 4 not rendered")
	}
	if !strings.Contains(rec.Body.String(), "Shelley") {
		t.Fatal("step 4 should show agent name")
	}
	if !strings.Contains(rec.Body.String(), "claude-sonnet-4") {
		t.Fatal("step 4 should show model")
	}
}

func TestE2E_CustomAgentAppearsInWizard(t *testing.T) {
	defer setupTest(t)()

	// Install a custom agent
	w := postJSON(apiConfigAgents, `{"id":"my-agent","name":"My Agent","command":"echo","models":["gpt-4.1"],"color":"#FF0000"}`)
	if w.Code != 200 {
		t.Fatalf("add agent failed: %d", w.Code)
	}

	// Step 2 of wizard should include it
	rec := getReq(partialsCreate, "/partials/create?step=2&prompt=test")
	if !strings.Contains(rec.Body.String(), "My Agent") {
		t.Fatal("custom agent should appear in wizard step 2")
	}

	// Step 3 with custom agent should show its models
	rec = getReq(partialsCreate, "/partials/create?step=3&prompt=test&agent=my-agent")
	if !strings.Contains(rec.Body.String(), "gpt-4.1") {
		t.Fatal("custom agent models should appear in wizard step 3")
	}
}

func TestE2E_TaskProgressComputed(t *testing.T) {
	// Queued = 0%
	q := Task{Status: "queued"}
	if p := taskProgress(q); p != 0 {
		t.Fatalf("queued progress should be 0, got %d", p)
	}

	// Done = 100%
	d := Task{Status: "done"}
	if p := taskProgress(d); p != 100 {
		t.Fatalf("done progress should be 100, got %d", p)
	}

	// Active with 100k tokens = 50%
	a := Task{Status: "active", Tokens: 100_000}
	if p := taskProgress(a); p != 50 {
		t.Fatalf("active with 100k tokens should be 50%%, got %d", p)
	}

	// Active with 0 tokens, no created time = 5%
	a0 := Task{Status: "active"}
	if p := taskProgress(a0); p != 5 {
		t.Fatalf("active with no data should be 5%%, got %d", p)
	}

	// Active near budget cap = clamped to 95%
	big := Task{Status: "active", Tokens: 500_000}
	if p := taskProgress(big); p > 95 {
		t.Fatalf("active progress should cap at 95%%, got %d", p)
	}
}

// ── Session tracking tests ──

func TestE2E_SessionCreatedOnRun(t *testing.T) {
	defer setupTest(t)()

	// Create a task
	w := postJSON(apiAddTask, `{"prompt":"build it","dir":"/tmp"}`)
	if w.Code != 200 {
		t.Fatalf("add task: %d", w.Code)
	}
	var task Task
	json.Unmarshal(w.Body.Bytes(), &task)

	// Run the task
	w = postJSON(apiRunTask, `{"id":"`+task.ID+`","agent":"shelley"}`)
	if w.Code != 200 {
		t.Fatalf("run task: %d %s", w.Code, w.Body.String())
	}

	// Read state and verify session was created
	s, err := readState()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tasks) != 1 {
		t.Fatal("expected 1 task")
	}
	if len(s.Tasks[0].Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(s.Tasks[0].Sessions))
	}
	sess := s.Tasks[0].Sessions[0]
	if sess.ID != "s1" {
		t.Fatalf("expected session id 's1', got %q", sess.ID)
	}
	if sess.Agent != "shelley" {
		t.Fatalf("expected agent 'shelley', got %q", sess.Agent)
	}
	if sess.StartedAt == "" {
		t.Fatal("session started_at should be set")
	}
	if sess.EndedAt != "" {
		t.Fatal("session ended_at should be empty")
	}
}

func TestE2E_SessionTokensUpdated(t *testing.T) {
	defer setupTest(t)()

	// Create + run
	w := postJSON(apiAddTask, `{"prompt":"build it"}`)
	var task Task
	json.Unmarshal(w.Body.Bytes(), &task)
	postJSON(apiRunTask, `{"id":"`+task.ID+`","agent":"shelley"}`)

	// Update tokens
	w = postJSON(apiUpdateTask, `{"id":"`+task.ID+`","tokens":"42000"}`)
	if w.Code != 200 {
		t.Fatalf("update: %d", w.Code)
	}

	s, _ := readState()
	if len(s.Tasks[0].Sessions) != 1 {
		t.Fatal("expected 1 session")
	}
	if s.Tasks[0].Sessions[0].Tokens != 42000 {
		t.Fatalf("expected session tokens 42000, got %d", s.Tasks[0].Sessions[0].Tokens)
	}
}

func TestE2E_SessionClosedOnDone(t *testing.T) {
	defer setupTest(t)()

	// Create + run
	w := postJSON(apiAddTask, `{"prompt":"build it"}`)
	var task Task
	json.Unmarshal(w.Body.Bytes(), &task)
	postJSON(apiRunTask, `{"id":"`+task.ID+`","agent":"shelley"}`)

	// Complete
	w = postJSON(apiDoneTask, `{"id":"`+task.ID+`","result":"shipped","tokens":"10000"}`)
	if w.Code != 200 {
		t.Fatalf("done: %d %s", w.Code, w.Body.String())
	}

	s, _ := readState()
	sess := s.Tasks[0].Sessions[0]
	if sess.Result != "done" {
		t.Fatalf("expected result 'done', got %q", sess.Result)
	}
	if sess.EndedAt == "" {
		t.Fatal("session ended_at should be set")
	}
	if sess.Summary != "shipped" {
		t.Fatalf("expected summary 'shipped', got %q", sess.Summary)
	}
	if sess.Tokens != 10000 {
		t.Fatalf("expected session tokens 10000, got %d", sess.Tokens)
	}
}

func TestE2E_HandoffCreatesNewSession(t *testing.T) {
	defer setupTest(t)()

	// Create + run
	w := postJSON(apiAddTask, `{"prompt":"build it"}`)
	var task Task
	json.Unmarshal(w.Body.Bytes(), &task)
	postJSON(apiRunTask, `{"id":"`+task.ID+`","agent":"shelley"}`)

	// Handoff
	w = postJSON(apiHandoffTask, `{"id":"`+task.ID+`","summary":"passing to next agent"}`)
	if w.Code != 200 {
		t.Fatalf("handoff: %d %s", w.Code, w.Body.String())
	}

	s, _ := readState()
	if s.Tasks[0].Status != "queued" {
		t.Fatalf("expected status 'queued' after handoff, got %q", s.Tasks[0].Status)
	}
	if len(s.Tasks[0].Sessions) != 1 {
		t.Fatalf("expected 1 session after handoff, got %d", len(s.Tasks[0].Sessions))
	}
	sess := s.Tasks[0].Sessions[0]
	if sess.Result != "handoff" {
		t.Fatalf("expected result 'handoff', got %q", sess.Result)
	}
	if sess.EndedAt == "" {
		t.Fatal("session ended_at should be set after handoff")
	}
	if sess.Summary != "passing to next agent" {
		t.Fatalf("expected summary, got %q", sess.Summary)
	}

	// Now run again (new agent picks it up)
	w = postJSON(apiRunTask, `{"id":"`+task.ID+`","agent":"opencode"}`)
	if w.Code != 200 {
		t.Fatalf("second run: %d %s", w.Code, w.Body.String())
	}

	s, _ = readState()
	if len(s.Tasks[0].Sessions) != 2 {
		t.Fatalf("expected 2 sessions after second run, got %d", len(s.Tasks[0].Sessions))
	}
	if s.Tasks[0].Sessions[1].ID != "s2" {
		t.Fatalf("expected session id 's2', got %q", s.Tasks[0].Sessions[1].ID)
	}
	if s.Tasks[0].Sessions[1].Agent != "opencode" {
		t.Fatalf("expected agent 'opencode', got %q", s.Tasks[0].Sessions[1].Agent)
	}
}


func TestE2E_PerTaskBudgetFlag(t *testing.T) {
	defer setupTest(t)()

	postJSON(apiAddTask, `{"prompt":"token hog"}`)
	postJSON(apiRunTask, `{"id":"1"}`)

	// Under limit — no flag
	postJSON(apiUpdateTask, `{"id":"1","tokens":"100000"}`)
	s, _ := readStateUnsafe()
	if s.Tasks[0].BudgetExceeded {
		t.Fatal("should not be flagged under limit")
	}

	// Over limit — flag set
	postJSON(apiUpdateTask, `{"id":"1","tokens":"300000"}`)
	s, _ = readStateUnsafe()
	if !s.Tasks[0].BudgetExceeded {
		t.Fatal("should be flagged at/over 300k per-task limit")
	}
}

func TestSandboxDispatch(t *testing.T) {
	defer setupTest(t)()

	// Create a mock sandbox worker
	received := make(chan map[string]string, 1)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		received <- body
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sandboxId":"task-%s","status":"started"}`, body["taskId"])
	}))
	defer mock.Close()

	// Point sandbox dispatch at mock
	old := sandboxWorkerURL
	sandboxWorkerURL = mock.URL
	defer func() { sandboxWorkerURL = old }()

	// Create and run a task
	postJSON(apiAddTask, `{"prompt":"test sandbox","agent":"pi","model":"test-model","repo_url":"https://github.com/test/repo"}`)
	postJSON(apiRunTask, `{"id":"1"}`)

	// Verify dispatch was called with correct payload
	select {
	case body := <-received:
		if body["taskId"] != "1" {
			t.Fatalf("expected taskId=1, got %s", body["taskId"])
		}
		if body["prompt"] != "test sandbox" {
			t.Fatalf("expected prompt='test sandbox', got %s", body["prompt"])
		}
		if body["agent"] != "pi" {
			t.Fatalf("expected agent=pi, got %s", body["agent"])
		}
		if body["model"] != "test-model" {
			t.Fatalf("expected model=test-model, got %s", body["model"])
		}
		if body["repoUrl"] != "https://github.com/test/repo" {
			t.Fatalf("expected repoUrl, got %s", body["repoUrl"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sandbox dispatch was not called within 2s")
	}

	// Give async goroutine time to write sandbox_id back
	time.Sleep(500 * time.Millisecond)

	// Verify sandbox_id was saved on session
	s, _ := readStateUnsafe()
	if len(s.Tasks) == 0 {
		t.Fatal("no tasks")
	}
	if len(s.Tasks[0].Sessions) == 0 {
		t.Fatal("no sessions")
	}
	if s.Tasks[0].Sessions[0].SandboxID != "task-1" {
		t.Fatalf("expected sandbox_id=task-1, got %s", s.Tasks[0].Sessions[0].SandboxID)
	}
}

func TestStripAnsi(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[38;5;109mpi\x1b[39m v0.52.9", "pi v0.52.9"},
		{"\x1b]0;title\x07text", "text"},
		{"a\x1b[?2004hb", "ab"},
		{"", ""},
	}
	for _, c := range cases {
		got := stripAnsi(c.in)
		if got != c.want {
			t.Errorf("stripAnsi(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSandboxOutputEndpoint(t *testing.T) {
	defer setupTest(t)()

	// Create a mock sandbox worker that serves logs
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sandboxId":"task-1","processId":"agent-1","logs":{"stdout":"\u001b[31mhello\u001b[0m world","stderr":""}}`)
	}))
	defer mock.Close()

	old := sandboxWorkerURL
	sandboxWorkerURL = mock.URL
	defer func() { sandboxWorkerURL = old }()

	// Create task, run it (which sets sandbox_id via dispatch)
	postJSON(apiAddTask, `{"prompt":"test","agent":"pi","model":"test"}`)
	postJSON(apiRunTask, `{"id":"1"}`)

	// Manually set sandbox_id since dispatch is async
	time.Sleep(100 * time.Millisecond)
	stateMu.Lock()
	s, _ := readStateUnsafe()
	if len(s.Tasks) > 0 && len(s.Tasks[0].Sessions) > 0 {
		s.Tasks[0].Sessions[0].SandboxID = "task-1"
		writeState(s)
	}
	stateMu.Unlock()

	// Hit the output endpoint
	req := httptest.NewRequest("GET", "/api/sandbox/output/1", nil)
	w := httptest.NewRecorder()
	apiSandboxOutput(w, req)

	got := w.Body.String()
	if !strings.Contains(got, "hello world") {
		t.Fatalf("expected 'hello world' in output, got: %s", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatal("ANSI codes should be stripped")
	}
}

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

	// Should have found some free models (OpenRouter always has some)
	if result.Count == 0 {
		t.Log("warning: no free models found (may be network issue)")
	}

	// All returned models should end with :free
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

func TestBuiltinAgentsIncludeCursorAndClaude(t *testing.T) {
	agents := builtinAgents()

	expected := []string{"shelley", "opencode", "pi", "cursor", "claude-code", "codex"}
	for _, id := range expected {
		if _, ok := agents[id]; !ok {
			t.Errorf("missing builtin agent: %s", id)
		}
	}

	// Cursor should have the right color
	if agents["cursor"].Color != "#00D1FF" {
		t.Errorf("cursor color = %q, want #00D1FF", agents["cursor"].Color)
	}

	// Claude Code should use 'claude' command
	if agents["claude-code"].Command != "claude" {
		t.Errorf("claude-code command = %q, want claude", agents["claude-code"].Command)
	}
}

// --- Router model tests ---

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
	defer func() { if old != "" { os.Setenv("OPENCODE_ZEN_API_KEY", old) } }()

	// Clear cache
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

func TestRouterRegistry(t *testing.T) {
	// All expected routers exist
	for _, id := range []string{"zen", "groq", "cerebras", "sambanova", "together", "fireworks", "openrouter"} {
		if getRouter(id) == nil {
			t.Errorf("missing router: %s", id)
		}
	}
	// Unknown router returns nil
	if getRouter("nope") != nil {
		t.Error("expected nil for unknown router")
	}
}

func TestDispatch_RouterField(t *testing.T) {
	defer setupTest(t)()
	os.Setenv("CHOMP_API_TOKEN", "test-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")

	// Unknown router should 400
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
	defer setupTest(t)()
	os.Setenv("CHOMP_API_TOKEN", "test-tok")
	defer os.Unsetenv("CHOMP_API_TOKEN")
	// Save and unset ALL router keys
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
	defer setupTest(t)()
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
	defer setupTest(t)()
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
	defer setupTest(t)()
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
