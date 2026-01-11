import { useState, useEffect, useCallback } from "react";
import { Link } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { TaskStatusBadge } from "@/components/TaskStatusBadge";
import { listTasks, createTask } from "@/lib/api";
import type { Task } from "@/lib/api";

export function TasksPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [showChildren, setShowChildren] = useState(false);
  const [name, setName] = useState("");
  const [workspace, setWorkspace] = useState("");
  const [prompt, setPrompt] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const fetchTasks = useCallback(async () => {
    try {
      const data = await listTasks();
      setTasks(data.tasks || []);
    } catch (error) {
      console.error("Failed to fetch tasks:", error);
    }
  }, []);

  useEffect(() => {
    fetchTasks();
    const interval = setInterval(fetchTasks, 5000);
    return () => clearInterval(interval);
  }, [fetchTasks]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!workspace || !prompt) return;

    setIsSubmitting(true);
    try {
      await createTask({
        name: name || undefined,
        workspace,
        instructions: [{ text: prompt }],
      });
      setName("");
      setWorkspace("");
      setPrompt("");
      await fetchTasks();
    } catch (error) {
      console.error("Failed to create task:", error);
    } finally {
      setIsSubmitting(false);
    }
  };

  const filteredTasks = showChildren ? tasks : tasks.filter((t) => !t.parent);

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleDateString("en-US", {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  return (
    <div className="space-y-5">
      <h1 className="text-2xl font-bold text-gray-800">XAgent</h1>

      <Card>
        <CardHeader>
          <CardTitle>New Task</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-3">
            <div>
              <Input
                placeholder="Name (optional)"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div>
              <Input
                placeholder="Workspace (e.g. default)"
                value={workspace}
                onChange={(e) => setWorkspace(e.target.value)}
                required
              />
            </div>
            <div>
              <Textarea
                placeholder="Instructions"
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                required
                className="min-h-[100px]"
              />
            </div>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? "Creating..." : "Create Task"}
            </Button>
          </form>
        </CardContent>
      </Card>

      {filteredTasks.length > 0 && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle>Tasks</CardTitle>
            <label className="text-sm text-gray-600 cursor-pointer flex items-center gap-2">
              children
              <input
                type="checkbox"
                checked={showChildren}
                onChange={(e) => setShowChildren(e.target.checked)}
              />
            </label>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Workspace</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredTasks.map((task) => (
                  <TableRow key={task.id} className={task.parent ? "opacity-70" : ""}>
                    <TableCell>
                      <Link
                        to={`/tasks/${task.id}`}
                        className="text-blue-600 hover:underline"
                      >
                        {task.name || <code className="text-xs">{task.id}</code>}
                      </Link>
                    </TableCell>
                    <TableCell>{task.workspace}</TableCell>
                    <TableCell>
                      <TaskStatusBadge status={task.status} />
                    </TableCell>
                    <TableCell>{formatDate(task.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
