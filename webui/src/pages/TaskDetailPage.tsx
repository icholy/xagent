import { useState, useEffect, useCallback } from "react";
import { useParams, Link, useNavigate } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { TaskStatusBadge } from "@/components/TaskStatusBadge";
import { LogTypeBadge } from "@/components/LogTypeBadge";
import {
  getTask,
  updateTask,
  listLogs,
  listLinks,
  listChildTasks,
  listEventsByTask,
  addEventTask,
  removeEventTask,
} from "@/lib/api";
import type { Task, LogEntry, Link as TaskLink, Event } from "@/lib/api";
import { X, RotateCcw, Archive } from "lucide-react";

export function TaskDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [task, setTask] = useState<Task | null>(null);
  const [parent, setParent] = useState<Task | null>(null);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [links, setLinks] = useState<TaskLink[]>([]);
  const [children, setChildren] = useState<Task[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [instruction, setInstruction] = useState("");
  const [eventId, setEventId] = useState("");
  const [isSubmittingInstruction, setIsSubmittingInstruction] = useState(false);
  const [isSubmittingEvent, setIsSubmittingEvent] = useState(false);

  const fetchAll = useCallback(async () => {
    if (!id) return;
    try {
      const [taskData, logsData, linksData, childrenData, eventsData] = await Promise.all([
        getTask(id),
        listLogs(id),
        listLinks(id),
        listChildTasks(id),
        listEventsByTask(id),
      ]);
      setTask(taskData.task);
      setLogs(logsData.entries || []);
      setLinks(linksData.links || []);
      setChildren(childrenData.tasks || []);
      setEvents(eventsData.events || []);

      // Fetch parent if exists
      if (taskData.task.parent) {
        const parentData = await getTask(taskData.task.parent);
        setParent(parentData.task);
      } else {
        setParent(null);
      }
    } catch (error) {
      console.error("Failed to fetch task data:", error);
    }
  }, [id]);

  useEffect(() => {
    fetchAll();
    const interval = setInterval(fetchAll, 3000);
    return () => clearInterval(interval);
  }, [fetchAll]);

  const handleUpdateTask = async (status: string) => {
    if (!id) return;
    try {
      await updateTask({ id, status });
      if (status === "archived") {
        navigate("/");
      } else {
        await fetchAll();
      }
    } catch (error) {
      console.error("Failed to update task:", error);
    }
  };

  const handleAddInstruction = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!id || !instruction) return;

    setIsSubmittingInstruction(true);
    try {
      await updateTask({
        id,
        status: "restarting",
        addInstructions: [{ text: instruction }],
      });
      setInstruction("");
      await fetchAll();
    } catch (error) {
      console.error("Failed to add instruction:", error);
    } finally {
      setIsSubmittingInstruction(false);
    }
  };

  const handleLinkEvent = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!id || !eventId) return;

    setIsSubmittingEvent(true);
    try {
      await addEventTask(parseInt(eventId), id);
      setEventId("");
      await fetchAll();
    } catch (error) {
      console.error("Failed to link event:", error);
    } finally {
      setIsSubmittingEvent(false);
    }
  };

  const handleUnlinkEvent = async (eventIdToRemove: number) => {
    if (!id) return;
    try {
      await removeEventTask(eventIdToRemove, id);
      await fetchAll();
    } catch (error) {
      console.error("Failed to unlink event:", error);
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

  const formatTime = (dateStr: string) => {
    return new Date(dateStr).toLocaleTimeString("en-US", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  };

  if (!task) {
    return <div>Loading...</div>;
  }

  const canCancel = task.status === "running" || task.status === "pending";
  const canRestart =
    task.status === "running" ||
    task.status === "completed" ||
    task.status === "failed";
  const canArchive = task.status === "completed" || task.status === "failed";
  const isArchived = task.status === "archived";

  return (
    <div className="space-y-5">
      <Link to="/" className="text-blue-600 hover:underline inline-block mb-2">
        &larr; Back to Tasks
      </Link>

      <Card>
        <CardContent className="pt-6">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-xl font-bold">{task.name || `Task ${task.id}`}</h2>
            <div className="flex gap-2">
              {canCancel && (
                <Button
                  variant="destructive"
                  size="icon"
                  onClick={() => handleUpdateTask("cancelled")}
                  title="Cancel"
                >
                  <X className="h-4 w-4" />
                </Button>
              )}
              {canRestart && (
                <Button
                  variant="default"
                  size="icon"
                  onClick={() => handleUpdateTask("restarting")}
                  title="Restart"
                >
                  <RotateCcw className="h-4 w-4" />
                </Button>
              )}
              {canArchive && (
                <Button
                  variant="secondary"
                  size="icon"
                  onClick={() => handleUpdateTask("archived")}
                  title="Archive"
                >
                  <Archive className="h-4 w-4" />
                </Button>
              )}
            </div>
          </div>

          <div className="space-y-2 text-sm">
            <p>
              <strong>Workspace:</strong> {task.workspace}
            </p>
            {parent && (
              <p>
                <strong>Parent:</strong>{" "}
                <Link to={`/tasks/${parent.id}`} className="text-blue-600 hover:underline">
                  {parent.name || parent.id}
                </Link>
              </p>
            )}
            <p>
              <strong>Status:</strong> <TaskStatusBadge status={task.status} />
            </p>
            <p>
              <strong>Created:</strong> {formatDate(task.createdAt)}
            </p>
          </div>

          {links.length > 0 && (
            <>
              <h3 className="text-lg font-semibold mt-6 mb-2">Links</h3>
              <ul className="space-y-1">
                {links.map((link) => (
                  <li key={link.id}>
                    <a
                      href={link.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-blue-600 hover:underline"
                    >
                      {link.title || link.url}
                    </a>
                    {link.notify && (
                      <Badge variant="secondary" className="ml-2 bg-blue-100 text-blue-800">
                        notify
                      </Badge>
                    )}
                    {link.relevance && (
                      <span className="block text-sm text-gray-500">{link.relevance}</span>
                    )}
                  </li>
                ))}
              </ul>
            </>
          )}

          <h3 className="text-lg font-semibold mt-6 mb-2">Instructions</h3>
          <div className="space-y-3">
            {task.instructions?.map((instr, idx) => (
              <div
                key={idx}
                className="bg-gray-50 border border-gray-200 rounded-md p-3"
              >
                <div className="whitespace-pre-wrap text-gray-800">{instr.text}</div>
                {instr.url && (
                  <a
                    href={instr.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sm text-gray-500 hover:text-blue-600 mt-2 block break-all"
                  >
                    {instr.url}
                  </a>
                )}
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      {!isArchived && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Add Instruction</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleAddInstruction} className="space-y-3">
              <Textarea
                placeholder="Enter a new instruction..."
                value={instruction}
                onChange={(e) => setInstruction(e.target.value)}
                required
                className="min-h-[100px]"
              />
              <Button type="submit" disabled={isSubmittingInstruction}>
                {isSubmittingInstruction ? "Adding..." : "Add Instruction"}
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      {children.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Child Tasks</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Workspace</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {children.map((child) => (
                  <TableRow key={child.id}>
                    <TableCell>
                      <Link
                        to={`/tasks/${child.id}`}
                        className="text-blue-600 hover:underline"
                      >
                        {child.name || <code className="text-xs">{child.id}</code>}
                      </Link>
                    </TableCell>
                    <TableCell>{child.workspace}</TableCell>
                    <TableCell>
                      <TaskStatusBadge status={child.status} />
                    </TableCell>
                    <TableCell>{formatDate(child.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Link Event</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleLinkEvent} className="flex gap-2">
            <Input
              type="number"
              placeholder="Event ID"
              value={eventId}
              onChange={(e) => setEventId(e.target.value)}
              required
              className="w-32"
            />
            <Button type="submit" disabled={isSubmittingEvent}>
              {isSubmittingEvent ? "Linking..." : "Link Event"}
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Events</CardTitle>
        </CardHeader>
        <CardContent>
          {events.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell>
                      <Link
                        to={`/events/${event.id}`}
                        className="text-blue-600 hover:underline"
                      >
                        {event.id}
                      </Link>
                    </TableCell>
                    <TableCell>{event.description}</TableCell>
                    <TableCell>
                      {event.url && (
                        <a
                          href={event.url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-blue-600 hover:underline"
                        >
                          {event.url}
                        </a>
                      )}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => handleUnlinkEvent(event.id)}
                      >
                        Remove
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-gray-500">No events linked to this task.</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Logs</CardTitle>
        </CardHeader>
        <CardContent>
          {logs.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-24">Time</TableHead>
                  <TableHead className="w-20">Type</TableHead>
                  <TableHead>Content</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {logs.map((log, idx) => (
                  <TableRow key={idx}>
                    <TableCell className="text-gray-500">{formatTime(log.createdAt)}</TableCell>
                    <TableCell>
                      <LogTypeBadge type={log.type} />
                    </TableCell>
                    <TableCell className="whitespace-pre-wrap">{log.content}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-gray-500">No logs yet.</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
