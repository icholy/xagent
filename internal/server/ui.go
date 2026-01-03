package server

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	tasks, _ := s.tasks.List()
	templates.ExecuteTemplate(w, "index.html", map[string]any{
		"Tasks": tasks,
	})
}

func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	tasks, _ := s.tasks.List()
	templates.ExecuteTemplate(w, "task-list.html", map[string]any{
		"Tasks": tasks,
	})
}

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.tasks.Get(id)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	logs, _ := s.logs.ListByTask(id)
	links, _ := s.links.ListByTask(id)
	data := map[string]any{
		"Task":  task,
		"Logs":  logs,
		"Links": links,
	}
	// HTMX requests get partial, regular requests get full page
	if r.Header.Get("HX-Request") == "true" {
		templates.ExecuteTemplate(w, "task-detail.html", data)
	} else {
		templates.ExecuteTemplate(w, "task-page.html", data)
	}
}

func (s *Server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	workspace := r.FormValue("workspace")
	prompt := r.FormValue("prompt")

	if workspace == "" || prompt == "" {
		http.Error(w, "workspace and prompt required", http.StatusBadRequest)
		return
	}

	task := &store.Task{
		ID:        uuid.NewString(),
		Workspace: workspace,
		Prompts:   []string{prompt},
		Status:    store.TaskStatusPending,
	}
	if err := s.tasks.Create(task); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "taskCreated")
	templates.ExecuteTemplate(w, "task-row.html", task)
}

func (s *Server) handleTaskUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	status := r.FormValue("status")
	prompt := r.FormValue("prompt")

	update := store.TaskUpdate{
		Status: store.TaskStatus(status),
	}
	if prompt != "" {
		update.AddPrompts = []string{prompt}
	}

	if err := s.tasks.Update(id, update); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated task detail
	task, err := s.tasks.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logs, _ := s.logs.ListByTask(id)
	links, _ := s.links.ListByTask(id)
	templates.ExecuteTemplate(w, "task-detail.html", map[string]any{
		"Task":  task,
		"Logs":  logs,
		"Links": links,
	})
}
