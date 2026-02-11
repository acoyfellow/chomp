package main

import (
	"encoding/json"
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

func apiState(w http.ResponseWriter, r *http.Request) {
	s, err := readState()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
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
	mux.Handle("/", http.FileServer(http.Dir(dashDir)))

	log.Printf("chomp dashboard on :%s (state: %s)", port, stateFile)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
