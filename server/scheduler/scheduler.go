package scheduler

import (
	"context"
	"fmt"
	"log"
	"os/exec"
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
}

func New(store *db.Store) *Scheduler {
	return &Scheduler{store: store, done: make(chan struct{})}
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

func (s *Scheduler) executeTask(task db.ScheduledTask) {
	run, err := s.store.CreateTaskRun(task.ID)
	if err != nil {
		log.Printf("scheduler: failed to create run for task %q: %v", task.Name, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), executionTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch task.TaskType {
	case "shell":
		cmd = exec.CommandContext(ctx, "bash", "-c", task.Command)
	case "claude":
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
