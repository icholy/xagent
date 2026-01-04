package server

import (
	"embed"
	"html/template"
	"net/http"
	"strconv"
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
	children, _ := s.tasks.ListChildren(id)
	data := map[string]any{
		"Task":     task,
		"Logs":     logs,
		"Links":    links,
		"Children": children,
	}
	if task.Parent != "" {
		data["Parent"], _ = s.tasks.Get(task.Parent)
	}
	templates.ExecuteTemplate(w, "task-page.html", data)
}

func (s *Server) handleTaskDetailPartial(w http.ResponseWriter, r *http.Request) {
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
	if task.Parent != "" {
		data["Parent"], _ = s.tasks.Get(task.Parent)
	}
	templates.ExecuteTemplate(w, "task-detail.html", data)
}

func (s *Server) handleTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	logs, _ := s.logs.ListByTask(id)
	templates.ExecuteTemplate(w, "task-logs.html", map[string]any{
		"TaskID": id,
		"Logs":   logs,
	})
}

func (s *Server) handleTaskChildren(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	children, _ := s.tasks.ListChildren(id)
	templates.ExecuteTemplate(w, "task-children.html", map[string]any{
		"Children": children,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	events, _ := s.events.List()
	templates.ExecuteTemplate(w, "events.html", map[string]any{
		"Events": events,
	})
}

func (s *Server) handleEventList(w http.ResponseWriter, r *http.Request) {
	events, _ := s.events.List()
	templates.ExecuteTemplate(w, "event-list.html", map[string]any{
		"Events": events,
	})
}

func (s *Server) handleEventDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	event, err := s.events.Get(id)
	if err != nil {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}
	tasks, _ := s.tasks.ListByEvent(id)
	templates.ExecuteTemplate(w, "event-page.html", map[string]any{
		"Event": event,
		"Tasks": tasks,
	})
}


