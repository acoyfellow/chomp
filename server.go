package main

import (
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
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static/style.css
var staticCSS []byte

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
	Tokens         int    `json:"tokens"`
	BudgetExceeded bool      `json:"budget_exceeded,omitempty"`
	Sessions       []Session `json:"sessions,omitempty"`
}

type State struct {
	Tasks  []Task `json:"tasks"`
	NextID int    `json:"next_id"`
}

var allowedKeys = map[string]bool{
	"CLOUDFLARE_API_TOKEN":    true,
	"CLOUDFLARE_ACCOUNT_ID":   true,
	"CLOUDFLARE_AI_GATEWAY_ID": true,
	"OPENCODE_ZEN_API_KEY":    true,
	"OPENROUTER_API_KEY":      true,
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

	balanceMu     sync.Mutex
	balanceCached *BalanceResponse
	balanceCachedAt time.Time
)

var builtinAgentIDs = map[string]bool{
	"shelley":  true,
	"opencode": true,
	"pi":       true,
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
	cfg := ConfigResponse{
		Agents: agents,
		Routers: map[string]RouterConfig{
			"cf-ai": {
				Name: "Cloudflare AI Gateway",
				Keys: []KeyStatus{
					checkKey("API Token", "CLOUDFLARE_API_TOKEN"),
					checkKey("Account ID", "CLOUDFLARE_ACCOUNT_ID"),
					checkKey("AI Gateway ID", "CLOUDFLARE_AI_GATEWAY_ID"),
				},
			},
			"zen": {
				Name: "OpenCode Zen",
				Keys: []KeyStatus{
					checkKey("API Key", "OPENCODE_ZEN_API_KEY"),
				},
			},
			"openrouter": {
				Name: "OpenRouter",
				Keys: []KeyStatus{
					checkKey("API Key", "OPENROUTER_API_KEY"),
				},
			},
		},
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

	// Budget gate: refuse to start if daily budget exhausted
	bal := fetchBalance()
	burned := totalBurnedTokens(s)
	if burned >= bal.TotalDailyTokens {
		http.Error(w, "daily token budget exhausted", 429)
		return
	}

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

// --------------- Balance endpoint ---------------

type ProviderBalance struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Configured  bool    `json:"configured"`
	DailyTokens int     `json:"daily_tokens"`
	DailyUSD    float64 `json:"daily_usd"`
	Source      string  `json:"source"`
	Note        string  `json:"note,omitempty"`
	Error       string  `json:"error,omitempty"`
}

type BalanceResponse struct {
	TotalDailyTokens int               `json:"total_daily_tokens"`
	TotalDailyUSD    float64           `json:"total_daily_usd"`
	Providers        []ProviderBalance `json:"providers"`
	CheckedAt        string            `json:"checked_at"`
}

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

func fetchBalance() *BalanceResponse {
	var providers []ProviderBalance
	var totalTokens int
	var totalUSD float64

	// --- Shelley (exe.dev) — always configured ---
	shelley := ProviderBalance{
		ID:          "shelley",
		Name:        "Shelley (exe.dev)",
		Color:       "#C8A630",
		Configured:  true,
		DailyTokens: 1_000_000,
		DailyUSD:    3.00,
		Source:      "Claude Sonnet 4 via exe.dev runtime",
	}
	providers = append(providers, shelley)
	totalTokens += shelley.DailyTokens
	totalUSD += shelley.DailyUSD

	// --- Cloudflare Workers AI ---
	cfToken := os.Getenv("CLOUDFLARE_API_TOKEN")
	cfAccount := os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	cfConfigured := cfToken != "" && cfAccount != ""
	cf := ProviderBalance{
		ID:         "cloudflare",
		Name:       "Cloudflare AI",
		Color:      "#D96F0E",
		Configured: cfConfigured,
		Source:     "10,000 neurons/day free tier (developers.cloudflare.com/workers-ai/platform/pricing)",
	}
	if cfConfigured {
		cf.DailyTokens = 10_000
		cf.DailyUSD = 0.03
		totalTokens += cf.DailyTokens
		totalUSD += cf.DailyUSD
	}
	providers = append(providers, cf)

	// --- OpenRouter ---
	orKey := os.Getenv("OPENROUTER_API_KEY")
	orConfigured := orKey != ""
	or := ProviderBalance{
		ID:         "openrouter",
		Name:       "OpenRouter",
		Color:      "#7C3AED",
		Configured: orConfigured,
		Source:     "Free models (Llama 3, Gemma) rate-limited to ~200 req/day",
	}
	if orConfigured {
		or.DailyTokens = 800_000
		or.DailyUSD = 0.24
		totalTokens += or.DailyTokens
		totalUSD += or.DailyUSD

		// Check for real credits (additive to daily budget)
		credits, err := fetchOpenRouterCredits(orKey)
		if err != nil {
			or.Error = err.Error()
		} else if credits > 0.001 {
			or.Note = fmt.Sprintf("Plus $%.2f in credits", credits)
		}
	}
	providers = append(providers, or)

	// --- OpenCode Zen ---
	zenKey := os.Getenv("OPENCODE_ZEN_API_KEY")
	zenConfigured := zenKey != ""
	zen := ProviderBalance{
		ID:         "opencode",
		Name:       "OpenCode Zen",
		Color:      "#10B981",
		Configured: zenConfigured,
		Source:     "OpenCode Zen tier allocation",
	}
	if zenConfigured {
		zen.DailyTokens = 500_000
		zen.DailyUSD = 1.50
		totalTokens += zen.DailyTokens
		totalUSD += zen.DailyUSD
	}
	providers = append(providers, zen)

	return &BalanceResponse{
		TotalDailyTokens: totalTokens,
		TotalDailyUSD:    totalUSD,
		Providers:        providers,
		CheckedAt:        time.Now().UTC().Format(time.RFC3339),
	}
}

func apiBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", 405)
		return
	}

	balanceMu.Lock()
	if balanceCached != nil && time.Since(balanceCachedAt) < 60*time.Second {
		resp := balanceCached
		balanceMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}
	balanceMu.Unlock()

	resp := fetchBalance()

	balanceMu.Lock()
	balanceCached = resp
	balanceCachedAt = time.Now()
	balanceMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

func serveCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(staticCSS)
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

	// OpenCode Zen
	zenStatus := "unconfigured"
	if os.Getenv("OPENCODE_ZEN_API_KEY") != "" {
		zenStatus = "live"
	}
	out = append(out, PlatformStatus{
		Name: "OpenCode Zen", Color: "#10B981", Status: zenStatus,
	})

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

	data := map[string]interface{}{
		"ID": task.ID, "Prompt": task.Prompt, "Dir": task.Dir,
		"AgentName": agentName(task.Platform), "AgentColor": agentColorStr(task.Platform),
		"Elapsed": timeAgo(task.Created), "Stale": isStale(task.Created, 5),
		"StartedStr": startedStr, "TokensStr": fmtTokens(task.Tokens),
		"SessionCount": len(task.Sessions), "Sessions": sessions,
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
	mux.HandleFunc("/static/style.css", serveCSS)
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
	mux.HandleFunc("/api/balance", apiBalance)

	log.Printf("chomp dashboard on :%s (state: %s)", port, stateFile)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
