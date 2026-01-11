import { useState, useEffect, useCallback } from "react";
import { useParams, Link, useNavigate } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { TaskStatusBadge } from "@/components/TaskStatusBadge";
import { getEvent, deleteEvent, processEvent, listEventTasks } from "@/lib/api";
import type { Event, Task } from "@/lib/api";

export function EventDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [event, setEvent] = useState<Event | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);

  const fetchData = useCallback(async () => {
    if (!id) return;
    try {
      const eventId = parseInt(id);
      const [eventData, tasksData] = await Promise.all([
        getEvent(eventId),
        listEventTasks(eventId),
      ]);
      setEvent(eventData.event);
      setTasks(tasksData.tasks || []);
    } catch (error) {
      console.error("Failed to fetch event data:", error);
    }
  }, [id]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleDelete = async () => {
    if (!id || !confirm("Are you sure you want to delete this event?")) return;
    try {
      await deleteEvent(parseInt(id));
      navigate("/events");
    } catch (error) {
      console.error("Failed to delete event:", error);
    }
  };

  const handleProcess = async () => {
    if (!id) return;
    try {
      await processEvent(parseInt(id));
      await fetchData();
    } catch (error) {
      console.error("Failed to process event:", error);
    }
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString("en-US", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  };

  if (!event) {
    return <div>Loading...</div>;
  }

  return (
    <div className="space-y-5">
      <Link to="/events" className="text-blue-600 hover:underline inline-block mb-2">
        &larr; Back to Events
      </Link>

      <Card>
        <CardContent className="pt-6">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-xl font-bold">Event #{event.id}</h2>
            <div className="flex gap-2">
              <Button onClick={handleProcess}>Process</Button>
              <Button variant="destructive" onClick={handleDelete}>
                Delete
              </Button>
            </div>
          </div>

          <div className="space-y-2 text-sm text-gray-600 mb-4">
            <span>
              <strong>Created:</strong> {formatDate(event.createdAt)}
            </span>
          </div>

          <p className="mb-2">
            <strong>Description:</strong> {event.description}
          </p>

          {event.url && (
            <p className="mb-2">
              <strong>URL:</strong>{" "}
              <a
                href={event.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-600 hover:underline"
              >
                {event.url}
              </a>
            </p>
          )}

          <h3 className="text-lg font-semibold mt-6 mb-2">Data</h3>
          <pre className="bg-gray-100 p-4 rounded-md overflow-x-auto text-sm">
            {event.data || "(no data)"}
          </pre>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Linked Tasks ({tasks.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {tasks.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Workspace</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tasks.map((task) => (
                  <TableRow key={task.id}>
                    <TableCell>
                      <Link
                        to={`/tasks/${task.id}`}
                        className="text-blue-600 hover:underline"
                      >
                        {task.id}
                      </Link>
                    </TableCell>
                    <TableCell>{task.name || "-"}</TableCell>
                    <TableCell>{task.workspace}</TableCell>
                    <TableCell>
                      <TaskStatusBadge status={task.status} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-gray-500">No tasks linked to this event yet.</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
