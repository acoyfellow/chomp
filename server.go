package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Task struct {
	ID       string `json:"id"`
	Prompt   string `json:"prompt"`
	Dir      string `json:"dir"`
	Status   string `json:"status"`
	Created  string `json:"created"`
	Result   string `json:"result"`
	Platform string `json:"platform"`
	Tokens   int    `json:"tokens"`
}

type State struct {
	Tasks  []Task `json:"tasks"`
	NextID int    `json:"next_id"`
}

var (
	stateFile string
	cacheMu   sync.RWMutex
	cached    *State
	cachedAt  time.Time
	stateMu   sync.Mutex
)

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

func apiState(w http.ResponseWriter, r *http.Request) {
	s, err := readState()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func apiAddTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
		Dir    string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Prompt == "" {
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
		ID:      fmt.Sprintf("%d", id),
		Prompt:  body.Prompt,
		Dir:     body.Dir,
		Status:  "queued",
		Created: time.Now().UTC().Format(time.RFC3339),
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func apiRunTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var body struct {
		ID       string `json:"id"`
		Agent    string `json:"agent"`
		Router   string `json:"router"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
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
		if s.Tasks[i].ID == body.ID && s.Tasks[i].Status == "queued" {
			s.Tasks[i].Status = "active"
			if body.Agent != "" {
				s.Tasks[i].Platform = body.Agent
			}
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func apiDoneTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var body struct {
		ID     string `json:"id"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
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
		if s.Tasks[i].ID == body.ID {
			s.Tasks[i].Status = "done"
			s.Tasks[i].Result = body.Result
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func apiDeleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
	} else {
		stateFile = filepath.Join(dir, "state.json")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	dashDir := filepath.Join(dir, "dashboard")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", apiState)
	mux.HandleFunc("/api/tasks", apiAddTask)
	mux.HandleFunc("/api/tasks/run", apiRunTask)
	mux.HandleFunc("/api/tasks/done", apiDoneTask)
	mux.HandleFunc("/api/tasks/delete", apiDeleteTask)
	mux.Handle("/", http.FileServer(http.Dir(dashDir)))

	log.Printf("chomp dashboard on :%s (state: %s)", port, stateFile)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
