package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed templates/docs.html
var docsHTML []byte

//go:embed static/style.css
var staticCSS []byte

//go:embed static/htmx.min.js
var staticHTMX []byte

var tmpl *template.Template

type Session struct {
	ID        string `json:"id"`
	Agent     string `json:"agent"`
	Model     string `json:"model"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Tokens    int    `json:"tokens"`
	Result    string `json:"result,omitempty"`
	Summary   string `json:"summary,omitempty"`
	SandboxID string `json:"sandbox_id,omitempty"`
}

type Task struct {
	ID             string `json:"id"`
	Prompt         string `json:"prompt"`
	Dir            string `json:"dir"`
	Status         string `json:"status"`
	Created        string `json:"created"`
	Result         string `json:"result"`
	Platform       string `json:"platform"`
	Model          string `json:"model,omitempty"`
	RepoURL        string `json:"repo_url,omitempty"`
	Tokens         int    `json:"tokens"`
	BudgetExceeded bool      `json:"budget_exceeded,omitempty"`
	Sessions       []Session `json:"sessions,omitempty"`
}

type State struct {
	Tasks  []Task `json:"tasks"`
	NextID int    `json:"next_id"`
}

var allowedKeys = map[string]bool{
	"CLOUDFLARE_API_TOKEN":     true,
	"CLOUDFLARE_ACCOUNT_ID":    true,
	"CLOUDFLARE_AI_GATEWAY_ID": true,
	"OPENCODE_ZEN_API_KEY":     true,
	"OPENROUTER_API_KEY":       true,
	"GROQ_API_KEY":             true,
	"CEREBRAS_API_KEY":         true,
	"SAMBANOVA_API_KEY":        true,
	"TOGETHER_API_KEY":         true,
	"FIREWORKS_API_KEY":        true,
}

var (
	stateFile  string
	keysFile   string
	agentsFile string
	cacheMu    sync.RWMutex
	cached     *State
	cachedAt   time.Time
	stateMu    sync.Mutex
	keysMu     sync.Mutex
	agentsMu   sync.Mutex


)

var builtinAgentIDs = map[string]bool{
	"shelley":     true,
	"opencode":    true,
	"pi":          true,
	"cursor":      true,
	"claude-code": true,
	"codex":       true,
}

var agentIDRegexp = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func readState() (*State, error) {
	cacheMu.RLock()
	if cached != nil && time.Since(cachedAt) < 2*time.Second {
		defer cacheMu.RUnlock()
		return cached, nil
	}
	cacheMu.RUnlock()

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Tasks: []Task{}, NextID: 1}, nil
		}
		return nil, err
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Tasks == nil {
		s.Tasks = []Task{}
	}

	cacheMu.Lock()
	cached = &s
	cachedAt = time.Now()
	cacheMu.Unlock()

	return &s, nil
}

func readStateUnsafe() (*State, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Tasks: []Task{}, NextID: 1}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Tasks == nil {
		s.Tasks = []Task{}
	}
	return &s, nil
}

func writeState(s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, stateFile)
}

type KeyStatus struct {
	Name    string `json:"name"`
	EnvVar  string `json:"env_var"`
	Set     bool   `json:"set"`
	Preview string `json:"preview"` // first 4 + last 4 chars
}

type ConfigResponse struct {
	Agents  map[string]AgentConfig  `json:"agents"`
	Routers map[string]RouterConfig `json:"routers"`
}

type AgentConfig struct {
	Name      string   `json:"name"`
	Builtin   bool     `json:"builtin"`
	Available bool     `json:"available"`
	Command   string   `json:"command"`
	Models    []string `json:"models"`
	Color     string   `json:"color"`
	Note      string   `json:"note"`
}

// CustomAgent is the on-disk format for state/agents.json
type CustomAgent struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Models  []string `json:"models"`
	Color   string   `json:"color"`
}

type RouterConfig struct {
	Name string      `json:"name"`
	Keys []KeyStatus `json:"keys"`
}

func maskKey(val string) string {
	if len(val) <= 8 {
		return "****"
	}
	return val[:4] + "..." + val[len(val)-4:]
}

func checkKey(name, envVar string) KeyStatus {
	val := os.Getenv(envVar)
	ks := KeyStatus{Name: name, EnvVar: envVar, Set: val != ""}
	if ks.Set {
		ks.Preview = maskKey(val)
	}
	return ks
}

// loadKeys reads state/keys.json and sets env vars on startup.
func loadKeys() {
	data, err := os.ReadFile(keysFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("warning: could not read keys file: %v", err)
		return
	}
	var keys map[string]string
	if err := json.Unmarshal(data, &keys); err != nil {
		log.Printf("warning: could not parse keys file: %v", err)
		return
	}
	for k, v := range keys {
		if allowedKeys[k] {
			os.Setenv(k, v)
		}
	}
	log.Printf("loaded %d API key(s) from %s", len(keys), keysFile)
}

// saveKeys writes the current persisted keys map to state/keys.json.
func saveKeys(keys map[string]string) error {
	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	tmp := keysFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, keysFile)
}

// readKeys loads the persisted keys map from disk.
func readKeys() (map[string]string, error) {
	data, err := os.ReadFile(keysFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var keys map[string]string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// readCustomAgents loads the persisted custom agents map from disk.
func readCustomAgents() (map[string]CustomAgent, error) {
	data, err := os.ReadFile(agentsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]CustomAgent{}, nil
		}
		return nil, err
	}
	var agents map[string]CustomAgent
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// saveCustomAgents writes the custom agents map to state/agents.json.
func saveCustomAgents(agents map[string]CustomAgent) error {
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	tmp := agentsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, agentsFile)
}

// builtinAgents returns the hardcoded built-in agent configs.
func builtinAgents() map[string]AgentConfig {
	return map[string]AgentConfig{
		"shelley": {
			Name:      "Shelley",
			Builtin:   true,
			Available: true,
			Command:   "",
			Models:    []string{"claude-sonnet-4", "claude-opus-4"},
			Color:     "#C8A630",
			Note:      "exe.dev worker loops",
		},
		"opencode": {
			Name:      "OpenCode",
			Builtin:   true,
			Available: func() bool { _, err := os.Stat("/usr/local/bin/opencode"); return err == nil }(),
			Command:   "opencode",
			Models:    []string{"claude-sonnet-4", "claude-opus-4", "gpt-4.1", "gemini-2.5-pro", "o3", "o4-mini"},
			Color:     "#4F6EC5",
			Note:      "CLI agent",
		},
		"pi": {
			Name:      "Pi",
			Builtin:   true,
			Available: false,
			Command:   "",
			Models:    []string{"claude-sonnet-4", "claude-opus-4", "gpt-4.1", "gemini-2.5-pro"},
			Color:     "#E05D44",
			Note:      "Not yet configured",
		},
		"cursor": {
			Name:      "Cursor",
			Builtin:   true,
			Available: func() bool { _, err := exec.LookPath("agent"); return err == nil }(),
			Command:   "agent",
			Models:    []string{"gpt-5.2", "claude-sonnet-4", "gemini-2.5-pro"},
			Color:     "#00D1FF",
			Note:      "Cursor Pro/Business subscription",
		},
		"claude-code": {
			Name:      "Claude Code",
			Builtin:   true,
			Available: func() bool { _, err := exec.LookPath("claude"); return err == nil }(),
			Command:   "claude",
			Models:    []string{"claude-sonnet-4", "claude-opus-4"},
			Color:     "#D97706",
			Note:      "Claude Max or API key",
		},
		"codex": {
			Name:      "Codex",
			Builtin:   true,
			Available: func() bool { _, err := exec.LookPath("codex"); return err == nil }(),
			Command:   "codex",
			Models:    []string{"o3", "o4-mini", "gpt-4.1"},
			Color:     "#10A37F",
			Note:      "ChatGPT Pro or API key",
		},
	}
}

// mergedAgents returns built-in agents merged with custom agents from disk.
func mergedAgents() (map[string]AgentConfig, error) {
	agents := builtinAgents()

	custom, err := readCustomAgents()
	if err != nil {
		return nil, err
	}

	for id, ca := range custom {
		if builtinAgentIDs[id] {
			continue // never override built-ins
		}
		available := false
		if ca.Command != "" {
			if _, err := exec.LookPath(ca.Command); err == nil {
				available = true
			}
		}
		agents[id] = AgentConfig{
			Name:      ca.Name,
			Builtin:   false,
			Available: available,
			Command:   ca.Command,
			Models:    ca.Models,
			Color:     ca.Color,
			Note:      "",
		}
	}

	return agents, nil
}

func apiConfigAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agents, err := mergedAgents()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agents)

	case http.MethodPost:
		var body struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Command string   `json:"command"`
			Models  []string `json:"models"`
			Color   string   `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" || body.Name == "" {
			http.Error(w, "need id and name", 400)
			return
		}
		if !agentIDRegexp.MatchString(body.ID) {
			http.Error(w, "id must be lowercase alphanumeric + hyphens", 400)
			return
		}
		if builtinAgentIDs[body.ID] {
			http.Error(w, fmt.Sprintf("cannot overwrite built-in agent %q", body.ID), 400)
			return
		}

		agentsMu.Lock()
		defer agentsMu.Unlock()

		agents, err := readCustomAgents()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		models := body.Models
		if models == nil {
			models = []string{}
		}
		agents[body.ID] = CustomAgent{
			Name:    body.Name,
			Command: body.Command,
			Models:  models,
			Color:   body.Color,
		}

		if err := saveCustomAgents(agents); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": body.ID})

	case http.MethodDelete:
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
			http.Error(w, "need id", 400)
			return
		}
		if builtinAgentIDs[body.ID] {
			http.Error(w, fmt.Sprintf("cannot delete built-in agent %q", body.ID), 400)
			return
		}

		agentsMu.Lock()
		defer agentsMu.Unlock()

		agents, err := readCustomAgents()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		delete(agents, body.ID)

		if err := saveCustomAgents(agents); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": body.ID})

	default:
		http.Error(w, "GET, POST, or DELETE only", 405)
	}
}

func apiConfigKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Key == "" {
		http.Error(w, "need key", 400)
		return
	}
	if !allowedKeys[body.Key] {
		http.Error(w, fmt.Sprintf("key %q not in whitelist", body.Key), 400)
		return
	}

	keysMu.Lock()
	defer keysMu.Unlock()

	keys, err := readKeys()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if body.Value == "" {
		// Delete
		os.Unsetenv(body.Key)
		delete(keys, body.Key)
	} else {
		// Set
		os.Setenv(body.Key, body.Value)
		keys[body.Key] = body.Value
	}

	if err := saveKeys(keys); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "key": body.Key})
}

func apiConfig(w http.ResponseWriter, r *http.Request) {
	agents, err := mergedAgents()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	routers := map[string]RouterConfig{
		"cf-ai": {
			Name: "Cloudflare AI Gateway",
			Keys: []KeyStatus{
				checkKey("API Token", "CLOUDFLARE_API_TOKEN"),
				checkKey("Account ID", "CLOUDFLARE_ACCOUNT_ID"),
				checkKey("AI Gateway ID", "CLOUDFLARE_AI_GATEWAY_ID"),
			},
		},
	}
	// Add all registered routers
	for _, rd := range routerDefs {
		routers[rd.ID] = RouterConfig{
			Name: rd.Name,
			Keys: []KeyStatus{checkKey("API Key", rd.EnvKey)},
		}
	}
	cfg := ConfigResponse{
		Agents:  agents,
		Routers: routers,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func apiState(w http.ResponseWriter, r *http.Request) {
	s, err := readState()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

// decodeBody reads JSON or form-encoded POST body into dst (a map).
func decodeBody(r *http.Request) map[string]string {
	m := make(map[string]string)
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		json.NewDecoder(r.Body).Decode(&m)
	} else {
		r.ParseForm()
		for k, v := range r.PostForm {
			if len(v) > 0 {
				m[k] = v[0]
			}
		}
	}
	return m
}

func apiAddTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	fields := decodeBody(r)
	prompt := fields["prompt"]
	if prompt == "" {
		http.Error(w, "need prompt", 400)
		return
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := readStateUnsafe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	id := s.NextID
	task := Task{
		ID:       fmt.Sprintf("%d", id),
		Prompt:   prompt,
		Dir:      fields["dir"],
		Status:   "queued",
		Created:  time.Now().UTC().Format(time.RFC3339),
		Platform: fields["agent"],
		Model:    fields["model"],
		RepoURL:  fields["repo_url"],
	}
	s.Tasks = append(s.Tasks, task)
	s.NextID = id + 1

	if err := writeState(s); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Invalidate cache
	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()

	w.Header().Set("HX-Trigger", "refreshTasks")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// Budget constants
const (
	perTaskTokenLimit = 300_000 // per-task soft cap (flag, don't kill)
)

// Sandbox worker URL
// Sandbox worker URL
var sandboxWorkerURL = getEnvOr("SANDBOX_WORKER_URL", "https://chomp-sandbox.coy.workers.dev")

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// dispatchToSandbox POSTs to the sandbox Worker to spin up a container.
// Non-blocking: errors are logged but don't fail the API response.
func dispatchToSandbox(task Task, sess Session) {
	go func() {
		payload := map[string]string{
			"taskId":  task.ID,
			"prompt":  task.Prompt,
			"agent":   sess.Agent,
			"model":   sess.Model,
		}
		if task.RepoURL != "" {
			payload["repoUrl"] = task.RepoURL
		}
		if task.Dir != "" {
			payload["dir"] = task.Dir
		}
		b, _ := json.Marshal(payload)
		resp, err := http.Post(sandboxWorkerURL+"/dispatch", "application/json", bytes.NewReader(b))
		if err != nil {
			log.Printf("[sandbox] dispatch error for task %s: %v", task.ID, err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("[sandbox] dispatch failed for task %s: %d %s", task.ID, resp.StatusCode, string(body))
			return
		}
		var result struct {
			SandboxID string `json:"sandboxId"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.SandboxID != "" {
			// Update session with sandbox ID
			stateMu.Lock()
			defer stateMu.Unlock()
			st, err := readStateUnsafe()
			if err != nil {
				return
			}
			for i := range st.Tasks {
				if st.Tasks[i].ID == task.ID {
					for j := range st.Tasks[i].Sessions {
						if st.Tasks[i].Sessions[j].ID == sess.ID {
							st.Tasks[i].Sessions[j].SandboxID = result.SandboxID
							break
						}
					}
					break
				}
			}
			_ = writeState(st)
		}
		log.Printf("[sandbox] dispatched task %s → sandbox %s", task.ID, result.SandboxID)
	}()
}

// totalBurnedTokens sums all tokens across tasks.
func totalBurnedTokens(s *State) int {
	total := 0
	for _, t := range s.Tasks {
		total += t.Tokens
	}
	return total
}

func apiRunTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	fields := decodeBody(r)
	if fields["id"] == "" {
		http.Error(w, "need id", 400)
		return
	}
	body := struct {
		ID, Agent, Router string
	}{ID: fields["id"], Agent: fields["agent"], Router: fields["router"]}

	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := readStateUnsafe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var dispatchTask Task
	var dispatchSess Session
	found := false
	for i := range s.Tasks {
		if s.Tasks[i].ID == body.ID && s.Tasks[i].Status == "queued" {
			s.Tasks[i].Status = "active"
			if body.Agent != "" {
				s.Tasks[i].Platform = body.Agent
			}
			sess := Session{
				ID:        fmt.Sprintf("s%d", len(s.Tasks[i].Sessions)+1),
				Agent:     s.Tasks[i].Platform,
				Model:     s.Tasks[i].Model,
				StartedAt: time.Now().UTC().Format(time.RFC3339),
			}
			s.Tasks[i].Sessions = append(s.Tasks[i].Sessions, sess)
			dispatchTask = s.Tasks[i]
			dispatchSess = sess
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "task not found or not queued", 404)
		return
	}

	if err := writeState(s); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()

	// Dispatch to Cloudflare Sandbox (async, non-blocking)
	dispatchToSandbox(dispatchTask, dispatchSess)

	w.Header().Set("HX-Trigger", "refreshTasks")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func apiDoneTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	fields := decodeBody(r)
	if fields["id"] == "" {
		http.Error(w, "need id", 400)
		return
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := readStateUnsafe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	found := false
	for i := range s.Tasks {
		if s.Tasks[i].ID == fields["id"] {
			s.Tasks[i].Status = "done"
			if fields["result"] != "" {
				s.Tasks[i].Result = fields["result"]
			}
			if fields["tokens"] != "" {
				var tk int
				fmt.Sscanf(fields["tokens"], "%d", &tk)
				if tk > 0 {
					s.Tasks[i].Tokens = tk
				}
			}
			if n := len(s.Tasks[i].Sessions); n > 0 {
				s.Tasks[i].Sessions[n-1].EndedAt = time.Now().UTC().Format(time.RFC3339)
				s.Tasks[i].Sessions[n-1].Result = "done"
				if fields["result"] != "" {
					s.Tasks[i].Sessions[n-1].Summary = fields["result"]
				}
				if fields["tokens"] != "" {
					var tk int
					fmt.Sscanf(fields["tokens"], "%d", &tk)
					if tk > 0 {
						s.Tasks[i].Sessions[n-1].Tokens = tk
					}
				}
			}
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "task not found", 404)
		return
	}

	if err := writeState(s); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()

	w.Header().Set("HX-Trigger", "refreshTasks")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// apiUpdateTask allows partial updates to a task (tokens, status, result).
func apiUpdateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	fields := decodeBody(r)
	if fields["id"] == "" {
		http.Error(w, "need id", 400)
		return
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := readStateUnsafe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	found := false
	for i := range s.Tasks {
		if s.Tasks[i].ID == fields["id"] {
			if fields["tokens"] != "" {
				var tk int
				fmt.Sscanf(fields["tokens"], "%d", &tk)
				if tk > 0 {
					s.Tasks[i].Tokens = tk
					if n := len(s.Tasks[i].Sessions); n > 0 {
						s.Tasks[i].Sessions[n-1].Tokens = tk
					}
					// Flag if per-task budget exceeded
					if tk >= perTaskTokenLimit {
						s.Tasks[i].BudgetExceeded = true
					}
				}
			}
			if fields["result"] != "" {
				s.Tasks[i].Result = fields["result"]
			}
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "task not found", 404)
		return
	}

	if err := writeState(s); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()

	w.Header().Set("HX-Trigger", "refreshTasks")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func apiHandoffTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	fields := decodeBody(r)
	if fields["id"] == "" {
		http.Error(w, "need id", 400)
		return
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := readStateUnsafe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	found := false
	for i := range s.Tasks {
		if s.Tasks[i].ID == fields["id"] && s.Tasks[i].Status == "active" {
			// Close current session
			if n := len(s.Tasks[i].Sessions); n > 0 {
				s.Tasks[i].Sessions[n-1].EndedAt = time.Now().UTC().Format(time.RFC3339)
				s.Tasks[i].Sessions[n-1].Result = "handoff"
				if fields["summary"] != "" {
					s.Tasks[i].Sessions[n-1].Summary = fields["summary"]
				}
			}
			// Re-queue the task
			s.Tasks[i].Status = "queued"
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "task not found or not active", 404)
		return
	}

	if err := writeState(s); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()

	w.Header().Set("HX-Trigger", "refreshTasks")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func apiDeleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	fields := decodeBody(r)
	id := fields["id"]
	if id == "" {
		http.Error(w, "need id", 400)
		return
	}
	body := struct{ ID string }{ID: id}

	stateMu.Lock()
	defer stateMu.Unlock()

	s, err := readStateUnsafe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	newTasks := make([]Task, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		if t.ID != body.ID {
			newTasks = append(newTasks, t)
		}
	}
	s.Tasks = newTasks

	if err := writeState(s); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()

	w.Header().Set("HX-Trigger", "refreshTasks")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// apiSandboxOutput proxies agent output from the sandbox Worker.
func apiSandboxOutput(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/sandbox/output/")
	if taskID == "" {
		http.Error(w, "need task id", 400)
		return
	}

	// Find the sandbox ID from the active session
	s, err := readState()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var sandboxID, processID string
	for _, t := range s.Tasks {
		if t.ID == taskID {
			for i := len(t.Sessions) - 1; i >= 0; i-- {
				if t.Sessions[i].SandboxID != "" {
					sandboxID = t.Sessions[i].SandboxID
					processID = "agent-" + taskID
					break
				}
			}
			break
		}
	}
	if sandboxID == "" {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("No sandbox running"))
		return
	}

	// Fetch logs from sandbox Worker
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("%s/logs/%s/%s", sandboxWorkerURL, sandboxID, processID)
	resp, err := client.Get(url)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Error fetching sandbox output: %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Logs struct {
			Stdout string `json:"stdout"`
			Stderr string `json:"stderr"`
		} `json:"logs"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Sandbox output unavailable"))
		return
	}
	if result.Error != "" {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Sandbox: %s", result.Error)
		return
	}

	// Strip ANSI escape codes for clean display
	output := stripAnsi(result.Logs.Stdout)
	if result.Logs.Stderr != "" {
		output += "\n" + stripAnsi(result.Logs.Stderr)
	}
	if output == "" {
		output = "Waiting for agent output..."
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(output))
}

// stripAnsi removes ANSI escape sequences from text.
func stripAnsi(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			// Skip ESC sequences
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
					i++
				}
				if i < len(s) {
					i++
				}
			} else if i < len(s) && s[i] == ']' {
				// OSC sequence — skip until BEL or ST
				i++
				for i < len(s) && s[i] != '\x07' && s[i] != '\x1b' {
					i++
				}
				if i < len(s) && s[i] == '\x07' {
					i++
				}
			} else if i < len(s) && s[i] == '?' {
				i++
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
					i++
				}
				if i < len(s) {
					i++
				}
			}
		} else if s[i] < ' ' && s[i] != '\n' && s[i] != '\r' && s[i] != '\t' {
			// Skip other control characters
			i++
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// --------------- Platform checks ---------------

// fetchOpenRouterCredits returns the remaining credits in USD for the given API key.
func fetchOpenRouterCredits(apiKey string) (float64, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/auth/key", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("auth/key request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading auth/key response: %w", err)
	}
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("auth/key returned %d: %s", resp.StatusCode, string(body))
	}

	var keyResp struct {
		Data struct {
			LimitRemaining float64 `json:"limit_remaining"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &keyResp); err != nil {
		return 0, fmt.Errorf("parsing auth/key: %w", err)
	}

	return keyResp.Data.LimitRemaining, nil
}

// --- Free model scanning (OpenRouter :free models) ---

type FreeModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	MaxOutput     int    `json:"max_output,omitempty"`
	Created       int64  `json:"created,omitempty"`
}

var (
	freeModelsCache   []FreeModel
	freeModelsCachedAt time.Time
	freeModelsMu       sync.Mutex
)

// fetchFreeModels queries OpenRouter for currently free models.
func fetchFreeModels() ([]FreeModel, error) {
	freeModelsMu.Lock()
	defer freeModelsMu.Unlock()

	// Cache for 15 minutes
	if freeModelsCache != nil && time.Since(freeModelsCachedAt) < 15*time.Minute {
		return freeModelsCache, nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			TopProvider   struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
			CreatedAt int64 `json:"created"`
			Pricing   struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding models: %w", err)
	}

	var free []FreeModel
	for _, m := range result.Data {
		if !strings.HasSuffix(m.ID, ":free") {
			continue
		}
		// Skip tiny models (<10B params based on name heuristic)
		name := strings.ToLower(m.Name)
		if strings.Contains(name, "1b") || strings.Contains(name, "3b") || strings.Contains(name, "7b") || strings.Contains(name, "8b") {
			// Allow "80b" etc but skip small ones
			if !strings.Contains(name, "80b") && !strings.Contains(name, "70b") && !strings.Contains(name, "180b") {
				continue
			}
		}
		free = append(free, FreeModel{
			ID:            m.ID,
			Name:          m.Name,
			ContextLength: m.ContextLength,
			MaxOutput:     m.TopProvider.MaxCompletionTokens,
			Created:       m.CreatedAt,
		})
	}

	freeModelsCache = free
	freeModelsCachedAt = time.Now()
	return free, nil
}

// apiFreeModels returns the currently free OpenRouter models.
func apiFreeModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", 405)
		return
	}
	models, err := fetchFreeModels()
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":  len(models),
		"models": models,
	})
}

// ── Dispatch layer: Shelley's staff ──
//
// POST /api/dispatch  → send prompt to a free model, get job ID
// GET  /api/result/:id → poll for completion
// GET  /api/jobs       → list recent jobs

type Job struct {
	ID        string `json:"id"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model"`
	Router    string `json:"router"`
	Status    string `json:"status"` // pending, running, done, error
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	TokensIn  int    `json:"tokens_in,omitempty"`
	TokensOut int    `json:"tokens_out,omitempty"`
	Created   string `json:"created"`
	Finished  string `json:"finished,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	System    string `json:"system,omitempty"`
}

var (
	jobsMu    sync.RWMutex
	jobs      = make(map[string]*Job)
	jobNextID atomic.Int64
)

// pickBestFreeModel selects the best available free model.
// Prefers largest context, filters out known-bad models.
func pickBestFreeModel() (string, error) {
	models, err := fetchFreeModels()
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no free models available")
	}
	// Sort by context length descending
	sort.Slice(models, func(i, j int) bool {
		return models[i].ContextLength > models[j].ContextLength
	})
	return models[0].ID, nil
}

// callOpenAICompat sends a chat completion to any OpenAI-compatible API.
func callOpenAICompat(ctx context.Context, baseURL, apiKey, model, system, prompt string, extraHeaders map[string]string) (string, int, int, error) {
	if apiKey == "" {
		return "", 0, 0, fmt.Errorf("API key not set")
	}

	var messages []map[string]string
	if system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": system})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})

	body, _ := json.Marshal(map[string]interface{}{
		"model":    model,
		"messages": messages,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, 0, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", 0, 0, fmt.Errorf("%d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", 0, 0, fmt.Errorf("parsing response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("no choices in response")
	}

	return result.Choices[0].Message.Content, result.Usage.PromptTokens, result.Usage.CompletionTokens, nil
}

// ── Router registry ──
//
// Each router is an OpenAI-compatible API with a base URL, env var for the key,
// and a default model to use when auto-picking.

type RouterDef struct {
	ID           string
	Name         string
	BaseURL      string
	EnvKey       string
	Color        string
	DefaultModel string            // used when model=auto for this router
	Headers      map[string]string // extra headers per request
}

var routerDefs = []RouterDef{
	{
		ID: "zen", Name: "OpenCode Zen",
		BaseURL: "https://opencode.ai/zen/v1", EnvKey: "OPENCODE_ZEN_API_KEY",
		Color: "#10B981", DefaultModel: "minimax-m2.5-free",
	},
	{
		ID: "groq", Name: "Groq",
		BaseURL: "https://api.groq.com/openai/v1", EnvKey: "GROQ_API_KEY",
		Color: "#F55036", DefaultModel: "llama-3.3-70b-versatile",
	},
	{
		ID: "cerebras", Name: "Cerebras",
		BaseURL: "https://api.cerebras.ai/v1", EnvKey: "CEREBRAS_API_KEY",
		Color: "#5046E4", DefaultModel: "llama-3.3-70b",
	},
	{
		ID: "sambanova", Name: "SambaNova",
		BaseURL: "https://api.sambanova.ai/v1", EnvKey: "SAMBANOVA_API_KEY",
		Color: "#FF6600", DefaultModel: "Meta-Llama-3.3-70B-Instruct",
	},
	{
		ID: "together", Name: "Together",
		BaseURL: "https://api.together.xyz/v1", EnvKey: "TOGETHER_API_KEY",
		Color: "#0EA5E9", DefaultModel: "meta-llama/Llama-3.3-70B-Instruct-Turbo",
	},
	{
		ID: "fireworks", Name: "Fireworks",
		BaseURL: "https://api.fireworks.ai/inference/v1", EnvKey: "FIREWORKS_API_KEY",
		Color: "#FF4500", DefaultModel: "accounts/fireworks/models/llama-v3p3-70b-instruct",
	},
	{
		ID: "openrouter", Name: "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1", EnvKey: "OPENROUTER_API_KEY",
		Color: "#7C3AED", DefaultModel: "auto",
		Headers: map[string]string{"HTTP-Referer": "https://chomp.dev", "X-Title": "chomp"},
	},
}

func getRouter(id string) *RouterDef {
	for i := range routerDefs {
		if routerDefs[i].ID == id {
			return &routerDefs[i]
		}
	}
	return nil
}

func routerNames() []string {
	var names []string
	for _, r := range routerDefs {
		names = append(names, r.ID)
	}
	return names
}

// callRouter dispatches to any registered router.
func callRouter(ctx context.Context, routerID, model, system, prompt string) (string, int, int, error) {
	rd := getRouter(routerID)
	if rd == nil {
		return "", 0, 0, fmt.Errorf("unknown router: %s", routerID)
	}
	apiKey := os.Getenv(rd.EnvKey)
	if apiKey == "" {
		return "", 0, 0, fmt.Errorf("%s not set", rd.EnvKey)
	}
	return callOpenAICompat(ctx, rd.BaseURL, apiKey, model, system, prompt, rd.Headers)
}

// callOpenRouter is kept for backward compat in free model scanning.
func callOpenRouter(ctx context.Context, model, system, prompt string) (string, int, int, error) {
	return callRouter(ctx, "openrouter", model, system, prompt)
}

// ── Generic model listing (works for any OpenAI-compatible router) ──

type RouterModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type modelCache struct {
	models   []RouterModel
	cachedAt time.Time
	mu       sync.Mutex
}

var routerModelCaches = make(map[string]*modelCache)
var routerModelCachesMu sync.Mutex

func getModelCache(routerID string) *modelCache {
	routerModelCachesMu.Lock()
	defer routerModelCachesMu.Unlock()
	if c, ok := routerModelCaches[routerID]; ok {
		return c
	}
	c := &modelCache{}
	routerModelCaches[routerID] = c
	return c
}

// fetchRouterModels lists models from any OpenAI-compatible /models endpoint.
func fetchRouterModels(routerID string) ([]RouterModel, error) {
	rd := getRouter(routerID)
	if rd == nil {
		return nil, fmt.Errorf("unknown router: %s", routerID)
	}
	apiKey := os.Getenv(rd.EnvKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%s not set", rd.EnvKey)
	}

	cache := getModelCache(routerID)
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if cache.models != nil && time.Since(cache.cachedAt) < 15*time.Minute {
		return cache.models, nil
	}

	req, _ := http.NewRequest("GET", rd.BaseURL+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s models: %w", rd.Name, err)
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding %s models: %w", rd.Name, err)
	}

	var models []RouterModel
	for _, m := range result.Data {
		models = append(models, RouterModel{ID: m.ID, Name: m.ID})
	}

	cache.models = models
	cache.cachedAt = time.Now()
	return models, nil
}

// pickDefaultModel returns the default model for a router.
func pickDefaultModel(routerID string) (string, error) {
	rd := getRouter(routerID)
	if rd == nil {
		return "", fmt.Errorf("unknown router: %s", routerID)
	}
	// OpenRouter special case: use free model scanner
	if routerID == "openrouter" {
		return pickBestFreeModel()
	}
	// For Zen, prefer free models
	if routerID == "zen" {
		models, err := fetchRouterModels("zen")
		if err == nil {
			for _, m := range models {
				if strings.Contains(m.ID, "free") {
					return m.ID, nil
				}
			}
		}
	}
	return rd.DefaultModel, nil
}

// apiRouterModels handles GET /api/models/:router
func apiRouterModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", 405)
		return
	}
	routerID := strings.TrimPrefix(r.URL.Path, "/api/models/")
	if routerID == "free" {
		// Legacy endpoint — handled by apiFreeModels
		apiFreeModels(w, r)
		return
	}
	rd := getRouter(routerID)
	if rd == nil {
		http.Error(w, fmt.Sprintf(`{"error":"unknown router: %s"}`, routerID), 400)
		return
	}
	models, err := fetchRouterModels(routerID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"router": routerID,
		"count":  len(models),
		"models": models,
	})
}

// requireAuth checks Bearer token against CHOMP_API_TOKEN env var.
// Returns true if authorized, false if rejected (and writes 401).
func requireAuth(w http.ResponseWriter, r *http.Request) bool {
	token := os.Getenv("CHOMP_API_TOKEN")
	if token == "" {
		// No token configured = locked down, reject everything
		http.Error(w, `{"error":"API not configured"}`, 503)
		return false
	}
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != token {
		w.Header().Set("WWW-Authenticate", `Bearer realm="chomp"`)
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return false
	}
	return true
}

func apiDispatch(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}

	var body struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
		System string `json:"system"`
		Router string `json:"router"` // "openrouter", "zen", or "auto" (default)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	if body.Prompt == "" {
		http.Error(w, `{"error":"prompt required"}`, 400)
		return
	}

	router := body.Router
	if router == "" {
		router = "auto"
	}

	model := body.Model

	// Resolve router + model
	if router == "auto" {
		// Pick first configured router (order = routerDefs priority)
		found := false
		for _, rd := range routerDefs {
			if os.Getenv(rd.EnvKey) != "" {
				router = rd.ID
				found = true
				break
			}
		}
		if !found {
			http.Error(w, `{"error":"no router configured"}`, 502)
			return
		}
	}

	rd := getRouter(router)
	if rd == nil {
		http.Error(w, fmt.Sprintf(`{"error":"unknown router: %s (options: %s)"}`, router, strings.Join(routerNames(), ", ")), 400)
		return
	}

	if model == "" || model == "auto" {
		var err error
		model, err = pickDefaultModel(router)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), 502)
			return
		}
	}

	id := strconv.FormatInt(jobNextID.Add(1), 10)
	job := &Job{
		ID:      id,
		Prompt:  body.Prompt,
		Model:   model,
		System:  body.System,
		Router:  router,
		Status:  "running",
		Created: time.Now().UTC().Format(time.RFC3339),
	}

	jobsMu.Lock()
	jobs[id] = job
	jobsMu.Unlock()

	log.Printf("[dispatch] job %s → %s/%s (%d chars)", id, router, model, len(body.Prompt))

	// Fire and forget — caller polls /api/result/:id
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		result, tokIn, tokOut, err := callRouter(ctx, router, model, body.System, body.Prompt)

		latency := time.Since(start).Milliseconds()

		jobsMu.Lock()
		defer jobsMu.Unlock()

		job.LatencyMs = latency
		job.Finished = time.Now().UTC().Format(time.RFC3339)

		if err != nil {
			job.Status = "error"
			job.Error = err.Error()
			log.Printf("[dispatch] job %s failed: %v", id, err)
		} else {
			job.Status = "done"
			job.Result = result
			job.TokensIn = tokIn
			job.TokensOut = tokOut
			log.Printf("[dispatch] job %s done: %s/%s %d→%d tokens, %dms", id, router, model, tokIn, tokOut, latency)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id, "model": model, "router": router, "status": "running"})
}

func apiResult(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", 405)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/result/")
	if id == "" {
		http.Error(w, `{"error":"id required"}`, 400)
		return
	}

	jobsMu.RLock()
	job, ok := jobs[id]
	jobsMu.RUnlock()

	if !ok {
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

func apiJobs(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", 405)
		return
	}

	jobsMu.RLock()
	all := make([]*Job, 0, len(jobs))
	for _, j := range jobs {
		all = append(all, j)
	}
	jobsMu.RUnlock()

	// Most recent first
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })

	// Cap at 50
	if len(all) > 50 {
		all = all[:50]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(all)
}

// ── OpenAI-compatible proxy (/v1/*) ──
//
// This is the core product. Any OpenAI SDK can point at chomp and get
// free model access through whichever routers are configured.
// No auth required on localhost — keys are pre-configured server-side.

// v1Auth checks Bearer token for /v1/ endpoints. Same CHOMP_API_TOKEN.
// Returns true if authorized. For local-only use, set CHOMP_V1_NO_AUTH=1 to skip.
func v1Auth(w http.ResponseWriter, r *http.Request) bool {
	if os.Getenv("CHOMP_V1_NO_AUTH") == "1" {
		return true
	}
	token := os.Getenv("CHOMP_API_TOKEN")
	if token == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		fmt.Fprint(w, `{"error":{"message":"CHOMP_API_TOKEN not configured","type":"server_error"}}`)
		return false
	}
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != token {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"invalid api key","type":"authentication_error"}}`)
		return false
	}
	return true
}

// v1ChatCompletions handles POST /v1/chat/completions (OpenAI-compatible).
func v1ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":{"message":"POST only","type":"invalid_request_error"}}`, 405)
		return
	}
	if !v1Auth(w, r) {
		return
	}

	var body struct {
		Model    string              `json:"model"`
		Messages []map[string]string `json:"messages"`
		Router   string              `json:"router"` // chomp extension: pick a router
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":{"message":"invalid JSON: %s","type":"invalid_request_error"}}`, err.Error())
		return
	}
	if len(body.Messages) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"messages required","type":"invalid_request_error"}}`)
		return
	}

	// Resolve router
	router := body.Router
	if router == "" || router == "auto" {
		for _, rd := range routerDefs {
			if os.Getenv(rd.EnvKey) != "" {
				router = rd.ID
				break
			}
		}
		if router == "" || router == "auto" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			fmt.Fprint(w, `{"error":{"message":"no router configured","type":"server_error"}}`)
			return
		}
	}

	rd := getRouter(router)
	if rd == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":{"message":"unknown router: %s","type":"invalid_request_error"}}`, router)
		return
	}

	// Resolve model
	model := body.Model
	if model == "" || model == "auto" {
		var err error
		model, err = pickDefaultModel(router)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			fmt.Fprintf(w, `{"error":{"message":"%s","type":"server_error"}}`, err.Error())
			return
		}
	}

	// Extract system + user prompt from messages
	var system, prompt string
	for _, m := range body.Messages {
		switch m["role"] {
		case "system":
			system = m["content"]
		case "user":
			prompt = m["content"]
		}
	}
	if prompt == "" {
		// Use last message as prompt regardless of role
		if len(body.Messages) > 0 {
			prompt = body.Messages[len(body.Messages)-1]["content"]
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	start := time.Now()
	result, tokIn, tokOut, err := callRouter(ctx, router, model, system, prompt)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		log.Printf("[v1] %s/%s failed (%dms): %v", router, model, latency, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		fmt.Fprintf(w, `{"error":{"message":"%s","type":"upstream_error"}}`, err.Error())
		return
	}

	log.Printf("[v1] %s/%s %d→%d tokens, %dms", router, model, tokIn, tokOut, latency)

	// Return standard OpenAI response format
	resp := map[string]interface{}{
		"id":      fmt.Sprintf("chomp-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": result},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     tokIn,
			"completion_tokens": tokOut,
			"total_tokens":      tokIn + tokOut,
		},
		// chomp extensions
		"router":     router,
		"latency_ms": latency,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// v1Models handles GET /v1/models — aggregates models from all configured routers.
func v1Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", 405)
		return
	}
	if !v1Auth(w, r) {
		return
	}

	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}

	var models []modelEntry
	for _, rd := range routerDefs {
		if os.Getenv(rd.EnvKey) == "" {
			continue
		}
		rModels, err := fetchRouterModels(rd.ID)
		if err != nil {
			continue
		}
		for _, m := range rModels {
			models = append(models, modelEntry{
				ID:      rd.ID + "/" + m.ID,
				Object:  "model",
				OwnedBy: rd.ID,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

// apiPlatforms returns real platform status as JSON.
func apiPlatforms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", 405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(platformStatuses())
}

// ── Template helpers ──

func fmtTokens(n int) string {
	if n >= 1e6 {
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	if n >= 1e3 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func timeAgo(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		return "just now"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func isStale(ts string, thresholdMin int) bool {
	if ts == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return time.Since(t) > time.Duration(thresholdMin)*time.Minute
}

// taskProgress computes a 0-100 progress percentage.
// For active tasks: based on tokens burned vs estimated session budget (200k).
// For done tasks: 100. For queued: 0.
func taskProgress(t Task) int {
	switch t.Status {
	case "done":
		return 100
	case "queued":
		return 0
	case "active":
		if t.Tokens <= 0 {
			// No token data yet — estimate from elapsed time
			if t.Created == "" {
				return 5
			}
			created, err := time.Parse(time.RFC3339, t.Created)
			if err != nil {
				return 5
			}
			elapsed := time.Since(created)
			// Assume ~30min for a typical task
			pct := int(elapsed.Minutes() / 30.0 * 100)
			if pct < 5 {
				pct = 5
			}
			if pct > 90 {
				pct = 90 // never show 100 while active
			}
			return pct
		}
		// Token-based: estimate 200k tokens per session
		pct := t.Tokens * 100 / 200_000
		if pct < 5 {
			pct = 5
		}
		if pct > 95 {
			pct = 95
		}
		return pct
	default:
		return 0
	}
}

func agentName(platform string) string {
	agents, _ := mergedAgents()
	if a, ok := agents[platform]; ok {
		return a.Name
	}
	if platform != "" {
		return platform
	}
	return "Unassigned"
}

func agentColorStr(platform string) string {
	agents, _ := mergedAgents()
	if a, ok := agents[platform]; ok && a.Color != "" {
		return a.Color
	}
	return "#999"
}

// ── Page handler ──

func pageIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "layout", map[string]interface{}{"DarkMode": false})
}

func pageDocs(w http.ResponseWriter, r *http.Request) {
	docsTmpl, err := template.New("docs").Parse(string(docsHTML))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	docsTmpl.ExecuteTemplate(w, "layout", nil)
}

func serveCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(staticCSS)
}

func serveHTMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(staticHTMX)
}

// ── Platform status (real, no theater) ──

type PlatformStatus struct {
	Name    string
	Color   string
	Status  string // "live", "limited", "down", "unconfigured"
	Credits string // real credits if available (e.g. "$4.20")
}

func platformStatuses() []PlatformStatus {
	var out []PlatformStatus

	// Shelley — check if worker binary or exe.dev environment exists
	shelleyStatus := "unconfigured"
	for _, p := range []string{"/home/exedev/bin/worker", "/usr/local/bin/worker"} {
		if _, err := os.Stat(p); err == nil {
			shelleyStatus = "live"
			break
		}
	}
	if shelleyStatus == "unconfigured" {
		if _, err := exec.LookPath("worker"); err == nil {
			shelleyStatus = "live"
		}
	}
	out = append(out, PlatformStatus{
		Name: "Shelley", Color: "#C8A630", Status: shelleyStatus,
	})

	// Cloudflare AI — configured = keys present
	cfStatus := "unconfigured"
	if os.Getenv("CLOUDFLARE_API_TOKEN") != "" && os.Getenv("CLOUDFLARE_ACCOUNT_ID") != "" {
		cfStatus = "live" // can't cheaply verify without a real API call
	}
	out = append(out, PlatformStatus{
		Name: "Cloudflare AI", Color: "#D96F0E", Status: cfStatus,
	})

	// OpenRouter — real credit check
	orStatus := "unconfigured"
	var orCredits string
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		orStatus = "live"
		if credits, err := fetchOpenRouterCredits(key); err != nil {
			orStatus = "down"
		} else if credits > 0.001 {
			orCredits = fmt.Sprintf("$%.2f", credits)
		}
	}
	out = append(out, PlatformStatus{
		Name: "OpenRouter", Color: "#7C3AED", Status: orStatus, Credits: orCredits,
	})

	// All routers from registry (except OpenRouter which is above)
	for _, rd := range routerDefs {
		if rd.ID == "openrouter" {
			continue // already handled above with credit check
		}
		status := "unconfigured"
		if os.Getenv(rd.EnvKey) != "" {
			status = "live"
		}
		out = append(out, PlatformStatus{
			Name: rd.Name, Color: rd.Color, Status: status,
		})
	}

	return out
}

// ── Partial handlers ──

func partialsBalance(w http.ResponseWriter, r *http.Request) {
	statuses := platformStatuses()
	s, _ := readState()

	var live, totalTasks, burned int
	for _, t := range s.Tasks {
		totalTasks++
		if t.Status == "active" {
			live++
		}
		burned += t.Tokens
	}

	data := map[string]interface{}{
		"Providers":  statuses,
		"Live":       live,
		"TotalTasks": totalTasks,
		"BurnedStr":  fmtTokens(burned),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "balance.html", data)
}

func partialsTasks(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "active"
	}
	s, _ := readState()

	type taskView struct {
		ID, Prompt, Platform, PlatformName, Elapsed, TokensStr, Status string
		Stale                                                         bool
		ProgressPct                                                   int
	}

	var active, queued, done []taskView
	for _, t := range s.Tasks {
		tv := taskView{
			ID: t.ID, Prompt: t.Prompt, Platform: t.Platform,
			PlatformName: agentName(t.Platform),
			Elapsed:   timeAgo(t.Created),
			TokensStr: fmtTokens(t.Tokens),
			Status:    t.Status,
			Stale:       isStale(t.Created, 5),
			ProgressPct: taskProgress(t),
		}
		switch t.Status {
		case "active":
			active = append(active, tv)
		case "queued":
			queued = append(queued, tv)
		case "done", "failed":
			done = append(done, tv)
		}
	}

	data := map[string]interface{}{
		"Tab": tab, "Active": active, "Queued": queued, "Done": done,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "tasks.html", data)
}

func partialsDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/partials/detail/")
	s, _ := readState()
	var task *Task
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			task = &s.Tasks[i]
			break
		}
	}
	if task == nil {
		http.NotFound(w, r)
		return
	}

	var startedStr string
	if t, err := time.Parse(time.RFC3339, task.Created); err == nil {
		startedStr = t.Local().Format("Jan 2, 3:04 PM")
	}

	type sessionView struct {
		Agent, Model, StartedStr, Duration, TokensStr, Result, Summary string
	}
	var sessions []sessionView
	for _, sess := range task.Sessions {
		sv := sessionView{
			Agent:     sess.Agent,
			Model:     sess.Model,
			TokensStr: fmtTokens(sess.Tokens),
			Result:    sess.Result,
			Summary:   sess.Summary,
		}
		if t, err := time.Parse(time.RFC3339, sess.StartedAt); err == nil {
			sv.StartedStr = t.Local().Format("Jan 2, 3:04 PM")
		}
		if sess.EndedAt != "" {
			if st, err1 := time.Parse(time.RFC3339, sess.StartedAt); err1 == nil {
				if et, err2 := time.Parse(time.RFC3339, sess.EndedAt); err2 == nil {
					d := et.Sub(st)
					if d < time.Minute {
						sv.Duration = fmt.Sprintf("%ds", int(d.Seconds()))
					} else {
						sv.Duration = fmt.Sprintf("%dm", int(d.Minutes()))
					}
				}
			}
		} else {
			sv.Duration = "running"
		}
		sessions = append(sessions, sv)
	}

	// Get sandbox ID from active session (last one without EndedAt)
	var activeSandboxID string
	for i := len(task.Sessions) - 1; i >= 0; i-- {
		if task.Sessions[i].EndedAt == "" && task.Sessions[i].SandboxID != "" {
			activeSandboxID = task.Sessions[i].SandboxID
			break
		}
	}

	data := map[string]interface{}{
		"ID": task.ID, "Prompt": task.Prompt, "Dir": task.Dir,
		"AgentName": agentName(task.Platform), "AgentColor": agentColorStr(task.Platform),
		"Elapsed": timeAgo(task.Created), "Stale": isStale(task.Created, 5),
		"StartedStr": startedStr, "TokensStr": fmtTokens(task.Tokens),
		"SessionCount": len(task.Sessions), "Sessions": sessions,
		"SandboxID": activeSandboxID,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "detail.html", data)
}

func partialsSettings(w http.ResponseWriter, r *http.Request) {
	cfg := buildConfig()

	type keyView struct{ Name, EnvVar, Preview string; Set bool }
	type routerView struct {
		Name, Color                  string
		Keys                         []keyView
		AllSet, SomeSet              bool
		MissingCount                 int
	}
	type agentView struct {
		ID, Name, Color, Note string
		Available, Builtin    bool
	}

	ma, _ := mergedAgents()
	var agents []agentView
	for id, a := range ma {
		agents = append(agents, agentView{ID: id, Name: a.Name, Color: a.Color, Note: a.Note, Available: a.Available, Builtin: a.Builtin})
	}

	var routers []routerView
	var keysSet, keysTotal int
	for _, rc := range cfg.Routers {
		rv := routerView{Name: rc.Name, Color: rc.Color}
		for _, k := range rc.Keys {
			rv.Keys = append(rv.Keys, keyView{Name: k.Name, EnvVar: k.EnvVar, Preview: k.Preview, Set: k.Set})
			keysTotal++
			if k.Set {
				keysSet++
			}
		}
		rv.AllSet = len(rv.Keys) > 0 && keysSet == keysTotal // this is wrong, needs per-router
		allSet := true
		someSet := false
		missing := 0
		for _, k := range rv.Keys {
			if !k.Set {
				allSet = false
				missing++
			} else {
				someSet = true
			}
		}
		rv.AllSet = allSet
		rv.SomeSet = someSet
		rv.MissingCount = missing
		routers = append(routers, rv)
	}

	data := map[string]interface{}{
		"KeysSet": keysSet, "KeysTotal": keysTotal,
		"Agents": agents, "Routers": routers,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "settings.html", data)
}

func partialsCreate(w http.ResponseWriter, r *http.Request) {
	step := r.URL.Query().Get("step")
	if step == "" {
		step = "1"
	}

	// Carry forward wizard fields from query params
	data := map[string]interface{}{
		"Step":   step,
		"Prompt": r.URL.Query().Get("prompt"),
		"Dir":    r.URL.Query().Get("dir"),
		"Agent":  r.URL.Query().Get("agent"),
		"Model":  r.URL.Query().Get("model"),
	}

	if step == "2" || step == "3" || step == "4" {
		agents, _ := mergedAgents()
		type agentItem struct {
			ID, Name, Color, Note string
			Available             bool
			Models                []string
		}
		var agentList []agentItem
		for id, a := range agents {
			agentList = append(agentList, agentItem{
				ID: id, Name: a.Name, Color: a.Color, Note: a.Note,
				Available: a.Available, Models: a.Models,
			})
		}
		data["Agents"] = agentList

		// Find selected agent's models for step 3
		if step == "3" || step == "4" {
			agentID := r.URL.Query().Get("agent")
			if a, ok := agents[agentID]; ok {
				data["AgentName"] = a.Name
				data["AgentColor"] = a.Color
				data["Models"] = a.Models
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, "create.html", data)
}

// buildConfig returns the config data for the settings page
func buildConfig() struct {
	Agents  []struct{ Name, Color, Note string; Available bool }
	Routers []struct {
		Name, Color string
		Keys        []KeyStatus
	}
} {
	type agentInfo struct{ Name, Color, Note string; Available bool }
	type routerInfo struct {
		Name, Color string
		Keys        []KeyStatus
	}
	var result struct {
		Agents  []struct{ Name, Color, Note string; Available bool }
		Routers []struct {
			Name, Color string
			Keys        []KeyStatus
		}
	}

	ma, _ := mergedAgents()
	for _, a := range ma {
		result.Agents = append(result.Agents, struct{ Name, Color, Note string; Available bool }{
			Name: a.Name, Color: a.Color, Note: a.Note, Available: a.Available,
		})
	}

	routers := []struct {
		ID, Name, Color string
		Keys            []KeyStatus
	}{
		{"cf-ai", "Cloudflare AI Gateway", "#D96F0E", []KeyStatus{
			checkKey("API Token", "CLOUDFLARE_API_TOKEN"),
			checkKey("Account ID", "CLOUDFLARE_ACCOUNT_ID"),
			checkKey("AI Gateway ID", "CLOUDFLARE_AI_GATEWAY_ID"),
		}},
		{"openrouter", "OpenRouter", "#7C3AED", []KeyStatus{
			checkKey("API Key", "OPENROUTER_API_KEY"),
		}},
		{"zen", "OpenCode Zen", "#4F6EC5", []KeyStatus{
			checkKey("API Key", "OPENCODE_ZEN_API_KEY"),
		}},
	}
	for _, rt := range routers {
		result.Routers = append(result.Routers, struct {
			Name, Color string
			Keys        []KeyStatus
		}{Name: rt.Name, Color: rt.Color, Keys: rt.Keys})
	}

	return result
}

func main() {
	dir := os.Getenv("CHOMP_DIR")
	if dir == "" {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	// Check for state subdir (Docker volume), fall back to dir
	stateDir := filepath.Join(dir, "state")
	if info, err := os.Stat(stateDir); err == nil && info.IsDir() {
		stateFile = filepath.Join(stateDir, "state.json")
		keysFile = filepath.Join(stateDir, "keys.json")
		agentsFile = filepath.Join(stateDir, "agents.json")
	} else {
		stateFile = filepath.Join(dir, "state.json")
		keysFile = filepath.Join(dir, "keys.json")
		agentsFile = filepath.Join(dir, "agents.json")
	}

	// Load persisted API keys into env vars
	loadKeys()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	// Parse embedded templates
	var err error
	tmpl, err = template.New("").ParseFS(templateFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		log.Fatalf("failed to parse templates: %v", err)
	}

	mux := http.NewServeMux()
	// Pages
	mux.HandleFunc("/", pageIndex)
	mux.HandleFunc("/docs", pageDocs)
	mux.HandleFunc("/static/style.css", serveCSS)
	mux.HandleFunc("/static/htmx.min.js", serveHTMX)
	// Partials (HTMX)
	mux.HandleFunc("/partials/balance", partialsBalance)
	mux.HandleFunc("/partials/tasks", partialsTasks)
	mux.HandleFunc("/partials/detail/", partialsDetail)
	mux.HandleFunc("/partials/settings", partialsSettings)
	mux.HandleFunc("/partials/create", partialsCreate)
	// API
	mux.HandleFunc("/api/state", apiState)
	mux.HandleFunc("/api/config", apiConfig)
	mux.HandleFunc("/api/config/keys", apiConfigKeys)
	mux.HandleFunc("/api/config/agents", apiConfigAgents)
	mux.HandleFunc("/api/tasks", apiAddTask)
	mux.HandleFunc("/api/tasks/run", apiRunTask)
	mux.HandleFunc("/api/tasks/done", apiDoneTask)
	mux.HandleFunc("/api/tasks/update", apiUpdateTask)
	mux.HandleFunc("/api/tasks/handoff", apiHandoffTask)
	mux.HandleFunc("/api/tasks/delete", apiDeleteTask)
	mux.HandleFunc("/api/sandbox/output/", apiSandboxOutput)
	mux.HandleFunc("/api/platforms", apiPlatforms)
	mux.HandleFunc("/api/models/free", apiFreeModels)
	mux.HandleFunc("/api/models/", apiRouterModels)
	mux.HandleFunc("/api/dispatch", apiDispatch)
	mux.HandleFunc("/api/result/", apiResult)
	mux.HandleFunc("/api/jobs", apiJobs)

	// OpenAI-compatible proxy — the core product
	mux.HandleFunc("/v1/chat/completions", v1ChatCompletions)
	mux.HandleFunc("/v1/models", v1Models)

	log.Printf("chomp dashboard on :%s (state: %s)", port, stateFile)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
