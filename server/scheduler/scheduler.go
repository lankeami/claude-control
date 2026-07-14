package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/robfig/cron/v3"
)

const (
	tickInterval     = 30 * time.Second
	executionTimeout = 1 * time.Hour
	missedTaskWindow = 5 * time.Minute
	maxOutputBytes   = 10 * 1024
	keepRunsPerTask  = 50
	shutdownTimeout  = 30 * time.Second
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type Scheduler struct {
	store   *db.Store
	done    chan struct{}
	wg      sync.WaitGroup
	running sync.Map

	// Loopback API access for running claude tasks through the managed
	// session pipeline (live visibility in the web UI). Empty baseURL falls
	// back to the legacy one-shot `claude -p` subprocess.
	loopbackURL string
	apiKey      string
	httpClient  *http.Client
}

func New(store *db.Store) *Scheduler {
	return &Scheduler{
		store:      store,
		done:       make(chan struct{}),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetLoopback configures the local API endpoint the scheduler uses to spawn
// managed sessions for claude tasks. Must be called before Start.
func (s *Scheduler) SetLoopback(baseURL, apiKey string) {
	s.loopbackURL = baseURL
	s.apiKey = apiKey
}

func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.run()
}

func (s *Scheduler) Stop() {
	close(s.done)
	ch := make(chan struct{})
	go func() { s.wg.Wait(); close(ch) }()
	select {
	case <-ch:
	case <-time.After(shutdownTimeout):
		log.Println("scheduler: shutdown timeout, some tasks may still be running")
	}
}

func (s *Scheduler) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.checkAndExecuteTasks()
		}
	}
}

func (s *Scheduler) Reconcile() {
	if err := s.store.MarkStaleRunsFailed(); err != nil {
		log.Printf("scheduler: failed to mark stale runs: %v", err)
	}
	tasks, err := s.store.GetEnabledTasks()
	if err != nil {
		log.Printf("scheduler: reconciliation failed: %v", err)
		return
	}
	now := time.Now()
	for _, task := range tasks {
		sched, err := cronParser.Parse(task.CronExpression)
		if err != nil {
			log.Printf("scheduler: invalid cron for task %q: %v", task.Name, err)
			continue
		}
		if task.NextRunAt != nil && task.NextRunAt.Before(now) && now.Sub(*task.NextRunAt) <= missedTaskWindow {
			log.Printf("scheduler: running missed task %q (was due %v ago)", task.Name, now.Sub(*task.NextRunAt).Round(time.Second))
			s.spawnTask(task)
		} else if task.NextRunAt != nil && task.NextRunAt.Before(now) {
			log.Printf("scheduler: skipping stale task %q (missed by %v)", task.Name, now.Sub(*task.NextRunAt).Round(time.Second))
		}
		nextRun := sched.Next(now)
		if err := s.store.UpdateTaskNextRun(task.ID, nextRun); err != nil {
			log.Printf("scheduler: failed to update next_run for task %q: %v", task.Name, err)
		}
	}
}

func (s *Scheduler) checkAndExecuteTasks() {
	tasks, err := s.store.GetTasksDueForExecution(time.Now())
	if err != nil {
		log.Printf("scheduler: failed to get due tasks: %v", err)
		return
	}
	for _, task := range tasks {
		if _, loaded := s.running.LoadOrStore(task.ID, true); loaded {
			continue
		}
		sched, err := cronParser.Parse(task.CronExpression)
		if err != nil {
			log.Printf("scheduler: invalid cron for task %q: %v", task.Name, err)
			s.running.Delete(task.ID)
			continue
		}
		nextRun := sched.Next(time.Now())
		s.store.UpdateTaskNextRun(task.ID, nextRun)
		s.spawnTask(task)
	}
}

func (s *Scheduler) spawnTask(task db.ScheduledTask) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.running.Delete(task.ID)
		s.executeTask(task)
	}()
}

// ErrAlreadyRunning is returned by Trigger when the task has an execution in flight.
var ErrAlreadyRunning = errors.New("task is already running")

// Trigger runs a task immediately, outside its cron schedule. The run record
// is created synchronously so callers can return it; execution happens in the
// background through the same pipeline as cron-fired runs.
func (s *Scheduler) Trigger(task db.ScheduledTask) (*db.TaskRun, error) {
	if _, loaded := s.running.LoadOrStore(task.ID, true); loaded {
		return nil, ErrAlreadyRunning
	}
	run, err := s.store.CreateTaskRun(task.ID)
	if err != nil {
		s.running.Delete(task.ID)
		return nil, fmt.Errorf("create run: %w", err)
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.running.Delete(task.ID)
		s.executeRun(task, run)
	}()
	return run, nil
}

func (s *Scheduler) executeTask(task db.ScheduledTask) {
	run, err := s.store.CreateTaskRun(task.ID)
	if err != nil {
		log.Printf("scheduler: failed to create run for task %q: %v", task.Name, err)
		return
	}
	s.executeRun(task, run)
}

func (s *Scheduler) executeRun(task db.ScheduledTask, run *db.TaskRun) {
	ctx, cancel := context.WithTimeout(context.Background(), executionTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch task.TaskType {
	case "shell":
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/c", task.Command)
		} else {
			cmd = exec.CommandContext(ctx, "bash", "-c", task.Command)
		}
	case "claude":
		if s.loopbackURL != "" {
			cancel()
			s.executeClaudeManaged(task, run)
			return
		}
		args := []string{"-p"}
		if task.Model != "" {
			args = append(args, "--model", task.Model)
		}
		args = append(args, task.Command)
		cmd = exec.CommandContext(ctx, "claude", args...)
	default:
		s.store.CompleteTaskRunWithError(run.ID, fmt.Sprintf("unknown task type: %s", task.TaskType))
		return
	}
	cmd.Dir = task.WorkingDirectory
	output, err := cmd.CombinedOutput()
	if err != nil {
		if cmd.ProcessState == nil {
			s.store.CompleteTaskRunWithError(run.ID, fmt.Sprintf("failed to start: %v", err))
			s.store.UpdateTaskLastRun(task.ID, time.Now())
			return
		}
	}
	outputStr := string(output)
	if len(outputStr) > maxOutputBytes {
		outputStr = outputStr[len(outputStr)-maxOutputBytes:]
	}
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	s.store.CompleteTaskRun(run.ID, exitCode, outputStr)
	s.store.UpdateTaskLastRun(task.ID, time.Now())
	s.store.CleanupOldRuns(task.ID, keepRunsPerTask)
}

// executeClaudeManaged runs a claude task through the managed session
// pipeline so the run is observable (and steerable) live from the web UI.
// The run record is linked to the session via session_id.
func (s *Scheduler) executeClaudeManaged(task db.ScheduledTask, run *db.TaskRun) {
	finish := func() {
		s.store.UpdateTaskLastRun(task.ID, time.Now())
		s.store.CleanupOldRuns(task.ID, keepRunsPerTask)
	}
	defer finish()

	sessID, err := s.ensureManagedSession(task)
	if err != nil {
		s.store.CompleteTaskRunWithError(run.ID, fmt.Sprintf("failed to prepare managed session: %v", err))
		return
	}
	if err := s.store.SetTaskRunSession(run.ID, sessID); err != nil {
		log.Printf("scheduler: failed to link run %s to session %s: %v", run.ID, sessID, err)
	}
	s.stampSchedulerSessionName(task, sessID)

	body, _ := json.Marshal(map[string]string{"message": task.Command, "model": task.Model})
	resp, err := s.loopbackPost("/api/sessions/"+sessID+"/message", body)
	if err != nil {
		s.store.CompleteTaskRunWithError(run.ID, fmt.Sprintf("failed to send prompt: %v", err))
		return
	}
	if resp.status < 200 || resp.status >= 300 {
		s.store.CompleteTaskRunWithError(run.ID, fmt.Sprintf("send prompt: HTTP %d: %s", resp.status, resp.body))
		return
	}

	state, err := s.waitForTurnCompletion(sessID)
	if err != nil {
		s.store.CompleteTaskRunWithError(run.ID, err.Error())
		return
	}
	if state == "idle" {
		s.store.CompleteTaskRunWithError(run.ID, "session ended unexpectedly (see session transcript)")
		return
	}
	s.store.CompleteTaskRun(run.ID, 0, fmt.Sprintf("completed in managed session %s — open the session for the full transcript", sessID))
}

// schedulerSessionPrefix marks sessions the scheduler owns in the session
// list; only these (or unnamed sessions) get renamed on each run, so sessions
// the user created and named are never clobbered.
const schedulerSessionPrefix = "\U0001F558 " // 🕘

func (s *Scheduler) stampSchedulerSessionName(task db.ScheduledTask, sessID string) {
	sess, err := s.store.GetSessionByID(sessID)
	if err != nil {
		return
	}
	if sess.Name != "" && !strings.HasPrefix(sess.Name, schedulerSessionPrefix) {
		return
	}
	name := fmt.Sprintf("%s%s — %s", schedulerSessionPrefix, task.Name, time.Now().Format("Jan 2 15:04"))
	if err := s.store.UpdateSessionName(sessID, name); err != nil {
		log.Printf("scheduler: failed to name session %s: %v", sessID, err)
	}
}

// ensureManagedSession picks the session a claude task run executes in:
// the task's linked session if it is managed, otherwise the managed session
// for the task's working directory, creating one via the local API if needed.
func (s *Scheduler) ensureManagedSession(task db.ScheduledTask) (string, error) {
	if task.SessionID != nil && *task.SessionID != "" {
		if sess, err := s.store.GetSessionByID(*task.SessionID); err == nil && sess.Mode == "managed" {
			return sess.ID, nil
		}
	}
	if sess, err := s.store.GetManagedSessionByCWD(task.WorkingDirectory); err != nil {
		return "", err
	} else if sess != nil {
		return sess.ID, nil
	}

	body, _ := json.Marshal(map[string]string{"cwd": task.WorkingDirectory})
	resp, err := s.loopbackPost("/api/sessions/create", body)
	if err != nil {
		return "", err
	}
	if resp.status < 200 || resp.status >= 300 {
		return "", fmt.Errorf("create session: HTTP %d: %s", resp.status, resp.body)
	}
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(resp.body), &sess); err != nil || sess.ID == "" {
		return "", fmt.Errorf("create session: unexpected response %q", resp.body)
	}
	return sess.ID, nil
}

// waitForTurnCompletion polls the session's activity_state until the turn
// finishes. Returns the terminal state ("waiting" or "idle").
func (s *Scheduler) waitForTurnCompletion(sessID string) (string, error) {
	const pollInterval = 3 * time.Second
	// Grace window: the state flips to "working" synchronously with the send
	// request, but allow a short window in case a fast turn already finished.
	graceDeadline := time.Now().Add(15 * time.Second)
	deadline := time.Now().Add(executionTimeout)
	sawWorking := false
	for time.Now().Before(deadline) {
		select {
		case <-s.done:
			return "", fmt.Errorf("server shutting down while task was running")
		case <-time.After(pollInterval):
		}
		sess, err := s.store.GetSessionByID(sessID)
		if err != nil {
			continue
		}
		switch sess.ActivityState {
		case "working":
			sawWorking = true
		default:
			if sawWorking || time.Now().After(graceDeadline) {
				return sess.ActivityState, nil
			}
		}
	}
	return "", fmt.Errorf("timed out after %v waiting for session turn to complete", executionTimeout)
}

type loopbackResponse struct {
	status int
	body   string
}

func (s *Scheduler) loopbackPost(path string, body []byte) (*loopbackResponse, error) {
	req, err := http.NewRequest(http.MethodPost, s.loopbackURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return &loopbackResponse{status: resp.StatusCode, body: string(data)}, nil
}
