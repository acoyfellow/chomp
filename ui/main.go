// chomp-ui: web dashboard for chomp (https://github.com/acoyfellow/chomp)
//
// One binary. SQLite. No config files required.
// Set API keys via environment variables or the web UI.
//
// Usage:
//   go build -o chomp-ui && ./chomp-ui
//   GROQ_API_KEY=gsk_... OPENROUTER_API_KEY=sk-or-... ./chomp-ui

package main

import (
	"bufio"
	"database/sql"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed dashboard.html
var dashboardHTML string

// ---------------------------------------------------------------------------
// Schema — the whole database in one string
// ---------------------------------------------------------------------------

const schema = `
CREATE TABLE IF NOT EXISTS platforms (
    slug        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    icon        TEXT NOT NULL DEFAULT '🤖',
    api_key     TEXT,
    base_url    TEXT,
    tokens_total INTEGER NOT NULL DEFAULT 0,
    tokens_used  INTEGER NOT NULL DEFAULT 0,
    reset_interval TEXT,
    available   BOOLEAN NOT NULL DEFAULT 0,
    last_error  TEXT,
    last_checked TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tasks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    prompt      TEXT NOT NULL,
    dir         TEXT,
    status      TEXT NOT NULL DEFAULT 'queued',
    platform    TEXT,
    result      TEXT,
    tokens_used INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at  TIMESTAMP,
    completed_at TIMESTAMP
);

-- Seed platforms (ignore if they already exist)
INSERT OR IGNORE INTO platforms (slug, name, icon, reset_interval)
VALUES
    ('exedev',     'exe.dev (Shelley)', '🐚', 'monthly'),
    ('openrouter', 'OpenRouter',        '🌐', 'daily'),
    ('google',     'Google AI Studio',  '🔷', 'daily'),
    ('groq',       'Groq',              '⚡', 'daily');
`

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

var (
	flagListen = flag.String("listen", ":8001", "HTTP listen address")
	flagChomp  = flag.String("chomp", "chomp", "path to chomp CLI")
	flagDB     = flag.String("db", "chomp-ui.db", "SQLite database path")
)

func main() {
	flag.Parse()
	loadEnvFile(".env")

	db := mustOpenDB(*flagDB)
	defer db.Close()

	// Apply API keys from env vars into the database
	applyEnvKeys(db)

	// Start background health checker (every 60s)
	go healthLoop(db, 60*time.Second)

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { serveDashboard(db, w) })
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { serveState(db, w) })
	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) { createTask(db, w, r) })
	mux.HandleFunc("POST /api/tasks/{id}/run", func(w http.ResponseWriter, r *http.Request) { runTask(db, w, r) })
	mux.HandleFunc("POST /api/tasks/{id}/done", func(w http.ResponseWriter, r *http.Request) { completeTask(db, w, r) })
	mux.HandleFunc("DELETE /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) { deleteTask(db, w, r) })
	mux.HandleFunc("POST /api/platforms/{slug}/key", func(w http.ResponseWriter, r *http.Request) { setKey(db, w, r) })

	log.Printf("chomp-ui listening on %s", *flagListen)
	log.Fatal(http.ListenAndServe(*flagListen, mux))
}

// ---------------------------------------------------------------------------
// Database
// ---------------------------------------------------------------------------

func mustOpenDB(path string) *sql.DB {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		log.Fatal(err)
	}
	db.Exec("PRAGMA journal_mode=wal")
	db.Exec("PRAGMA busy_timeout=1000")
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("schema: %v", err)
	}
	return db
}

// ---------------------------------------------------------------------------
// Types — what the template and API see
// ---------------------------------------------------------------------------

type Platform struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	TokensUsed  int64  `json:"tokens_used"`
	TokensTotal int64  `json:"tokens_total"`
	TokensLeft  int64  `json:"tokens_left"`
	PctUsed     int    `json:"pct_used"`
	ResetsAt    string `json:"resets_at"`
	Available   bool   `json:"available"`
	HasKey      bool   `json:"has_key"`
	LastError   string `json:"last_error"`
}

type Task struct {
	ID       int64  `json:"id"`
	Prompt   string `json:"prompt"`
	Dir      string `json:"dir"`
	Status   string `json:"status"`
	Platform string `json:"platform"`
	Result   string `json:"result"`
	Tokens   int64  `json:"tokens"`
	Created  string `json:"created"`
	Elapsed  string `json:"elapsed"`
}

type PageData struct {
	Platforms   []Platform `json:"platforms"`
	ActiveTasks []Task     `json:"active_tasks"`
	QueuedTasks []Task     `json:"queued_tasks"`
	DoneTasks   []Task     `json:"done_tasks"`
	Stats       Stats      `json:"stats"`
}

type Stats struct {
	Queued  int `json:"queued"`
	Active  int `json:"active"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	Tokens  int64 `json:"tokens"`
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

func loadPageData(db *sql.DB) PageData {
	data := PageData{
		Platforms:   []Platform{},
		ActiveTasks: []Task{},
		QueuedTasks: []Task{},
		DoneTasks:   []Task{},
	}

	// Platforms
	rows, _ := db.Query(`SELECT slug, name, icon, COALESCE(api_key,''), tokens_total, tokens_used,
		COALESCE(reset_interval,''), available, COALESCE(last_error,'') FROM platforms ORDER BY available DESC, name`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var p Platform
			var apiKey string
			rows.Scan(&p.Slug, &p.Name, &p.Icon, &apiKey, &p.TokensTotal, &p.TokensUsed,
				&p.ResetsAt, &p.Available, &p.LastError)
			p.HasKey = apiKey != "" || p.Slug == "exedev"
			p.TokensLeft = p.TokensTotal - p.TokensUsed
			if p.TokensTotal > 0 {
				p.PctUsed = int(p.TokensUsed * 100 / p.TokensTotal)
			}
			data.Platforms = append(data.Platforms, p)
		}
	}

	// Tasks
	taskRows, _ := db.Query(`SELECT id, prompt, COALESCE(dir,''), status, COALESCE(platform,''),
		COALESCE(result,''), tokens_used, created_at, started_at FROM tasks ORDER BY created_at DESC`)
	if taskRows != nil {
		defer taskRows.Close()
		for taskRows.Next() {
			var t Task
			var createdAt time.Time
			var startedAt sql.NullTime
			taskRows.Scan(&t.ID, &t.Prompt, &t.Dir, &t.Status, &t.Platform,
				&t.Result, &t.Tokens, &createdAt, &startedAt)
			t.Created = ago(time.Since(createdAt))
			if startedAt.Valid {
				t.Elapsed = ago(time.Since(startedAt.Time))
			}
			switch t.Status {
			case "active":
				data.ActiveTasks = append(data.ActiveTasks, t)
				data.Stats.Active++
			case "queued":
				data.QueuedTasks = append(data.QueuedTasks, t)
				data.Stats.Queued++
			case "done":
				data.DoneTasks = append(data.DoneTasks, t)
				data.Stats.Done++
				data.Stats.Tokens += t.Tokens
			case "failed":
				data.Stats.Failed++
			}
		}
	}

	return data
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func serveDashboard(db *sql.DB, w http.ResponseWriter) {
	data := loadPageData(db)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := template.New("dashboard").Funcs(template.FuncMap{
		"formatTokens": formatTokens,
	}).Parse(dashboardHTML)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tmpl.Execute(w, data)
}

func serveState(db *sql.DB, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loadPageData(db))
}

func createTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt   string `json:"prompt"`
		Dir      string `json:"dir"`
		Platform string `json:"platform"`
	}
	if strings.Contains(r.Header.Get("Content-Type"), "json") {
		json.NewDecoder(r.Body).Decode(&req)
	} else {
		req.Prompt = r.FormValue("prompt")
		req.Dir = r.FormValue("dir")
		req.Platform = r.FormValue("platform")
	}
	if req.Prompt == "" {
		http.Error(w, `{"error":"prompt required"}`, 400)
		return
	}

	result, err := db.Exec(`INSERT INTO tasks (prompt, dir, platform) VALUES (?, NULLIF(?,''), NULLIF(?,''))`,
		req.Prompt, req.Dir, req.Platform)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 500)
		return
	}
	id, _ := result.LastInsertId()

	// Sync to chomp CLI (best effort)
	go syncToChomp(req.Prompt, req.Dir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"id": id})
}

func runTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Get the task's platform, default to exedev
	var platform string
	db.QueryRow(`SELECT COALESCE(platform,'exedev') FROM tasks WHERE id=?`, id).Scan(&platform)

	db.Exec(`UPDATE tasks SET status='active', platform=?, started_at=CURRENT_TIMESTAMP WHERE id=?`, platform, id)

	// Dispatch via chomp (best effort, background)
	go func() {
		cmd := exec.Command(*flagChomp, "run", "--platform", platform)
		cmd.Run()
	}()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"dispatched","platform":"%s"}`, platform)
}

func completeTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Result string `json:"result"`
		Tokens int64  `json:"tokens"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	db.Exec(`UPDATE tasks SET status='done', result=?, tokens_used=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
		req.Result, req.Tokens, id)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"done"}`)
}

func deleteTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"deleted"}`)
}

func setKey(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	db.Exec(`UPDATE platforms SET api_key=? WHERE slug=?`, req.APIKey, slug)

	// Immediately check the key
	var p platformRow
	db.QueryRow(`SELECT slug, COALESCE(api_key,''), COALESCE(base_url,'') FROM platforms WHERE slug=?`, slug).Scan(&p.slug, &p.apiKey, &p.baseURL)
	result := checkPlatform(p)

	var errPtr *string
	if result.err != "" {
		errPtr = &result.err
	}
	db.Exec(`UPDATE platforms SET available=?, last_error=?, tokens_total=CASE WHEN ?>0 THEN ? ELSE tokens_total END, last_checked=CURRENT_TIMESTAMP WHERE slug=?`,
		result.ok, errPtr, result.total, result.total, slug)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"available": result.ok, "error": result.err, "details": result.details})
}

// ---------------------------------------------------------------------------
// Platform health checks — validate API keys, fetch usage
// ---------------------------------------------------------------------------

type platformRow struct {
	slug, apiKey, baseURL string
}

type checkResult struct {
	ok      bool
	total   int64
	used    int64
	err     string
	details string
}

func checkPlatform(p platformRow) checkResult {
	switch p.slug {
	case "exedev":
		return checkExeDev()
	case "openrouter":
		return checkOpenRouter(p.apiKey)
	case "groq":
		return checkGroq(p.apiKey)
	case "google":
		return checkGoogle(p.apiKey)
	default:
		if p.apiKey == "" {
			return checkResult{err: "no API key"}
		}
		return checkResult{ok: true, details: "unknown platform, key stored"}
	}
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// exe.dev: just check if the worker binary exists
func checkExeDev() checkResult {
	path, err := exec.LookPath("worker")
	if err != nil {
		return checkResult{err: "worker CLI not in PATH"}
	}
	return checkResult{ok: true, details: "worker at " + path}
}

// OpenRouter: GET /api/v1/auth/key → { data: { usage, limit, is_free_tier, rate_limit } }
func checkOpenRouter(key string) checkResult {
	if key == "" {
		return checkResult{err: "no API key"}
	}
	req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/auth/key", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := httpClient.Do(req)
	if err != nil {
		return checkResult{err: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return checkResult{err: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trunc(string(body), 120))}
	}
	var out struct {
		Data struct {
			Usage     float64  `json:"usage"`
			Limit     *float64 `json:"limit"`
			FreeTier  bool     `json:"is_free_tier"`
			RateLimit struct {
				Requests int    `json:"requests"`
				Interval string `json:"interval"`
			} `json:"rate_limit"`
		} `json:"data"`
	}
	json.Unmarshal(body, &out)
	d := out.Data
	return checkResult{
		ok:      true,
		total:   int64(d.RateLimit.Requests),
		details: fmt.Sprintf("free_tier=%v rate=%d/%s", d.FreeTier, d.RateLimit.Requests, d.RateLimit.Interval),
	}
}

// Groq: GET /openai/v1/models to validate key
func checkGroq(key string) checkResult {
	if key == "" {
		return checkResult{err: "no API key"}
	}
	req, _ := http.NewRequest("GET", "https://api.groq.com/openai/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := httpClient.Do(req)
	if err != nil {
		return checkResult{err: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return checkResult{err: "invalid API key"}
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return checkResult{err: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trunc(string(body), 120))}
	}
	var out struct {
		Data []struct{ ID string `json:"id"` } `json:"data"`
	}
	json.Unmarshal(body, &out)
	return checkResult{ok: true, total: 6000, details: fmt.Sprintf("%d models", len(out.Data))}
}

// Google AI Studio: GET /v1beta/models?key=KEY to validate
func checkGoogle(key string) checkResult {
	if key == "" {
		return checkResult{err: "no API key"}
	}
	resp, err := httpClient.Get("https://generativelanguage.googleapis.com/v1beta/models?key=" + key)
	if err != nil {
		return checkResult{err: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 400 || resp.StatusCode == 403 {
		return checkResult{err: "invalid API key"}
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return checkResult{err: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trunc(string(body), 120))}
	}
	var out struct {
		Models []struct{ Name string `json:"name"` } `json:"models"`
	}
	json.Unmarshal(body, &out)
	n := 0
	for _, m := range out.Models {
		if strings.Contains(m.Name, "gemini") {
			n++
		}
	}
	return checkResult{ok: true, total: 1500, details: fmt.Sprintf("%d gemini models", n)}
}

// healthLoop checks all platforms periodically
func healthLoop(db *sql.DB, interval time.Duration) {
	checkAll(db)
	for range time.Tick(interval) {
		checkAll(db)
	}
}

func checkAll(db *sql.DB) {
	rows, _ := db.Query(`SELECT slug, COALESCE(api_key,''), COALESCE(base_url,'') FROM platforms`)
	if rows == nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p platformRow
		rows.Scan(&p.slug, &p.apiKey, &p.baseURL)
		result := checkPlatform(p)

		// "no API key" is not an error — it's just unconfigured. Don't store it.
		var errPtr *string
		if result.err != "" && result.err != "no API key" {
			errPtr = &result.err
		}

		db.Exec(`UPDATE platforms SET available=?, last_error=?, tokens_total=CASE WHEN ?>0 THEN ? ELSE tokens_total END, last_checked=CURRENT_TIMESTAMP WHERE slug=?`,
			result.ok, errPtr, result.total, result.total, p.slug)

		if result.err != "" {
			log.Printf("[health] %s: %s", p.slug, result.err)
		} else {
			log.Printf("[health] %s: ok (%s)", p.slug, result.details)
		}
	}
}

// ---------------------------------------------------------------------------
// Env + chomp helpers
// ---------------------------------------------------------------------------

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}

func applyEnvKeys(db *sql.DB) {
	for slug, envKey := range map[string]string{
		"openrouter": "OPENROUTER_API_KEY",
		"groq":       "GROQ_API_KEY",
		"google":     "GEMINI_API_KEY",
	} {
		if v := os.Getenv(envKey); v != "" {
			db.Exec(`UPDATE platforms SET api_key=? WHERE slug=?`, v, slug)
			log.Printf("Loaded %s from $%s", slug, envKey)
		}
	}
}

func syncToChomp(prompt, dir string) {
	args := []string{"add", prompt}
	if dir != "" {
		args = append(args, "--dir", dir)
	}
	exec.Command(*flagChomp, args...).Run()
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1000)
	default:
		return strconv.FormatInt(n, 10)
	}
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}


