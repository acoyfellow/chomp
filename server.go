package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)






type KeyStatus struct {
	Name    string `json:"name"`
	EnvVar  string `json:"env_var"`
	Set     bool   `json:"set"`
	Preview string `json:"preview"` // first 4 + last 4 chars
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


func apiConfig(w http.ResponseWriter, r *http.Request) {
	routers := make(map[string]RouterConfig)
	for _, rd := range routerDefs {
		routers[rd.ID] = RouterConfig{
			Name: rd.Name,
			Keys: []KeyStatus{checkKey("API Key", rd.EnvKey)},
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"routers": routers})
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




// ── Platform status (real, no theater) ──

type PlatformStatus struct {
	Name    string
	Color   string
	Status  string // "live", "limited", "down", "unconfigured"
	Credits string // real credits if available (e.g. "$4.20")
}

func platformStatuses() []PlatformStatus {
	var out []PlatformStatus
	for _, rd := range routerDefs {
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




// apiRoot returns basic server info as JSON.
func apiRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	var configured int
	for _, rd := range routerDefs {
		if os.Getenv(rd.EnvKey) != "" {
			configured++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    "chomp",
		"version": "2.0.0",
		"routers": configured,
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", apiRoot)
	// API
	mux.HandleFunc("/api/config", apiConfig)
	mux.HandleFunc("/api/platforms", apiPlatforms)
	mux.HandleFunc("/api/models/free", apiFreeModels)
	mux.HandleFunc("/api/models/", apiRouterModels)
	mux.HandleFunc("/api/dispatch", apiDispatch)
	mux.HandleFunc("/api/result/", apiResult)
	mux.HandleFunc("/api/jobs", apiJobs)

	// OpenAI-compatible proxy — the core product
	mux.HandleFunc("/v1/chat/completions", v1ChatCompletions)
	mux.HandleFunc("/v1/models", v1Models)

	log.Printf("chomp API on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
