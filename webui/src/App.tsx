import { BrowserRouter, Routes, Route, Navigate } from "react-router";
import { Layout } from "@/components/Layout";
import { TasksPage } from "@/pages/TasksPage";
import { TaskDetailPage } from "@/pages/TaskDetailPage";
import { EventsPage } from "@/pages/EventsPage";
import { EventDetailPage } from "@/pages/EventDetailPage";

function App() {
  return (
    <BrowserRouter basename="/ui">
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<TasksPage />} />
          <Route path="/tasks" element={<Navigate to="/" replace />} />
          <Route path="/tasks/:id" element={<TaskDetailPage />} />
          <Route path="/events" element={<EventsPage />} />
          <Route path="/events/:id" element={<EventDetailPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

export default App;
