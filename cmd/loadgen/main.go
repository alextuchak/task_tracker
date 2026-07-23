// loadgen drives realistic traffic through a running task-tracker instance
// so the Grafana dashboard has something to show: a mix of reads, writes,
// auth failures and not-founds at a steady request rate.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type worker struct {
	client *http.Client
	base   string
	token  string
	tasks  []int64
	teamID int64
}

var (
	total    atomic.Int64
	statuses sync.Map
)

func main() {
	base := flag.String("url", "http://localhost:8080", "base url")
	users := flag.Int("users", 600, "concurrent users")
	duration := flag.Duration("duration", 3*time.Minute, "how long to run")
	pause := flag.Duration("pause", 650*time.Millisecond, "pause between requests per user")
	flag.Parse()

	fmt.Printf("loadgen: %d users against %s for %s\n", *users, *base, *duration)

	var wg sync.WaitGroup
	deadline := time.Now().Add(*duration)
	for id := range *users {
		wg.Go(func() {
			time.Sleep(time.Duration(rand.Intn(30000)) * time.Millisecond)
			w, err := setupWorker(*base, id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "worker %d setup: %v\n", id, err)
				return
			}
			w.run(deadline, *pause)
		})
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	reportLoop(done)

	fmt.Printf("\ntotal requests: %d\n", total.Load())
	statuses.Range(func(k, v any) bool {
		fmt.Printf("  %v: %d\n", k, v.(*atomic.Int64).Load())
		return true
	})
}

func setupWorker(base string, id int) (*worker, error) {
	w := &worker{client: &http.Client{Timeout: 5 * time.Second}, base: base}
	email := fmt.Sprintf("load-%d-%d@loadgen.io", time.Now().UnixNano(), id)

	if _, err := w.post("/api/v1/register", map[string]any{
		"email": email, "name": fmt.Sprintf("loadgen-%d", id), "password": "password123",
	}); err != nil {
		return nil, err
	}
	body, err := w.post("/api/v1/login", map[string]any{"email": email, "password": "password123"})
	if err != nil {
		return nil, err
	}
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &login); err != nil {
		return nil, err
	}
	w.token = login.AccessToken

	body, err = w.post("/api/v1/teams", map[string]any{"name": fmt.Sprintf("loadgen-team-%d-%d", id, time.Now().Unix())})
	if err != nil {
		return nil, err
	}
	var team struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &team); err != nil {
		return nil, err
	}
	w.teamID = team.ID

	for i := 0; i < 10; i++ {
		body, err := w.post("/api/v1/tasks", map[string]any{
			"team_id": w.teamID, "title": fmt.Sprintf("load task %d", i),
		})
		if err != nil {
			return nil, err
		}
		var task struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(body, &task); err != nil {
			return nil, err
		}
		w.tasks = append(w.tasks, task.ID)
	}
	return w, nil
}

func (w *worker) run(deadline time.Time, pause time.Duration) {
	statusesList := []string{"todo", "in_progress", "done"}
	for time.Now().Before(deadline) {
		switch rand.Intn(10) {
		case 0, 1, 2:
			w.get(fmt.Sprintf("/api/v1/tasks?team_id=%d", w.teamID), w.token)
		case 3:
			w.get(fmt.Sprintf("/api/v1/tasks?team_id=%d&status=todo", w.teamID), w.token)
		case 4, 5:
			w.get("/api/v1/me", w.token)
		case 6:
			w.get("/api/v1/teams", w.token)
		case 7:
			taskID := w.tasks[rand.Intn(len(w.tasks))]
			status := statusesList[rand.Intn(len(statusesList))]
			_, _ = w.request(http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", taskID),
				map[string]any{"title": "load task", "status": status}, w.token)
		case 8:
			w.get("/api/v1/me", "garbage-token")
		case 9:
			w.get("/api/v1/tasks/999999999/history", w.token)
		}
		time.Sleep(pause + time.Duration(rand.Intn(60))*time.Millisecond)
	}
}

func (w *worker) get(path, token string) {
	_, _ = w.request(http.MethodGet, path, nil, token)
}

func (w *worker) post(path string, payload map[string]any) ([]byte, error) {
	return w.request(http.MethodPost, path, payload, w.token)
}

func (w *worker) request(method, path string, payload map[string]any, token string) ([]byte, error) {
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, w.base+path, &body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	total.Add(1)
	counter, _ := statuses.LoadOrStore(resp.StatusCode, &atomic.Int64{})
	counter.(*atomic.Int64).Add(1)

	var out bytes.Buffer
	if _, err := out.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest && method == http.MethodPost {
		return out.Bytes(), fmt.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}
	return out.Bytes(), nil
}

func reportLoop(done <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			fmt.Printf("  requests so far: %d\n", total.Load())
		}
	}
}
