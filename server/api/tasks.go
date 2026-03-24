package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type createTaskRequest struct {
	SessionID        string `json:"session_id"`
	Name             string `json:"name"`
	TaskType         string `json:"task_type"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	CronExpression   string `json:"cron_expression"`
}

type updateTaskRequest struct {
	Name             string `json:"name"`
	TaskType         string `json:"task_type"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	CronExpression   string `json:"cron_expression"`
	Enabled          bool   `json:"enabled"`
}

func validateTaskRequest(name, taskType, command, workingDir, cronExpr string) (string, bool) {
	if name == "" {
		return `{"error":"name is required"}`, false
	}
	if command == "" {
		return `{"error":"command is required"}`, false
	}
	if workingDir == "" {
		return `{"error":"working_directory is required"}`, false
	}
	if taskType != "shell" && taskType != "claude" {
		return `{"error":"task_type must be shell or claude"}`, false
	}
	if _, err := cronParser.Parse(cronExpr); err != nil {
		return `{"error":"invalid cron expression"}`, false
	}
	return "", true
}

func computeNextRun(cronExpr string) (*time.Time, error) {
	schedule, err := cronParser.Parse(cronExpr)
	if err != nil {
		return nil, err
	}
	next := schedule.Next(time.Now())
	return &next, nil
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if errMsg, ok := validateTaskRequest(req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression); !ok {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	task, err := s.store.CreateScheduledTask(req.SessionID, req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	// Compute and persist next_run_at
	if nextRun, err := computeNextRun(req.CronExpression); err == nil {
		if updateErr := s.store.UpdateTaskNextRun(task.ID, *nextRun); updateErr == nil {
			task.NextRunAt = nextRun
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")

	tasks, err := s.store.ListScheduledTasks(sessionID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []db.ScheduledTask{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")

	task, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")

	task, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if errMsg, ok := validateTaskRequest(req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression); !ok {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateScheduledTask(taskID, req.Name, req.TaskType, req.Command, req.WorkingDirectory, req.CronExpression, req.Enabled); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	// Recompute next_run_at after update
	if nextRun, err := computeNextRun(req.CronExpression); err == nil {
		s.store.UpdateTaskNextRun(taskID, *nextRun)
	}

	updated, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil || updated == nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")

	task, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	if err := s.store.DeleteScheduledTask(taskID); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleListTaskRuns(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")

	task, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	runs, err := s.store.ListTaskRuns(taskID, limit)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.TaskRun{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

func (s *Server) handleGetTaskRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runId")

	run, err := s.store.GetTaskRunByID(runID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, `{"error":"run not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

func (s *Server) handleTriggerTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")

	task, err := s.store.GetScheduledTaskByID(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	run, err := s.store.CreateTaskRun(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}
