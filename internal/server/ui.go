package server

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(
	template.New("").Funcs(template.FuncMap{
		"dict": func(values ...any) map[string]any {
			m := make(map[string]any)
			for i := 0; i < len(values); i += 2 {
				key, _ := values[i].(string)
				m[key] = values[i+1]
			}
			return m
		},
	}).ParseFS(templateFS, "templates/*.html"),
)

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
	// AJAX requests get partial, regular requests get full page
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		templates.ExecuteTemplate(w, "task-detail.html", data)
	} else {
		templates.ExecuteTemplate(w, "task-page.html", data)
	}
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.tasks.Get(id)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	templates.ExecuteTemplate(w, "task-status.html", task)
}

func (s *Server) handleTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	logs, _ := s.logs.ListByTask(id)
	templates.ExecuteTemplate(w, "task-logs.html", map[string]any{
		"TaskID": id,
		"Logs":   logs,
	})
}

