import { Link, Outlet, useLocation } from "react-router";
import { cn } from "@/lib/utils";

export function Layout() {
  const location = useLocation();

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="max-w-4xl mx-auto p-5">
        <nav className="mb-5 flex gap-4">
          <Link
            to="/"
            className={cn(
              "text-blue-600 hover:underline",
              location.pathname === "/" && "font-medium"
            )}
          >
            Tasks
          </Link>
          <Link
            to="/events"
            className={cn(
              "text-blue-600 hover:underline",
              location.pathname.startsWith("/events") && "font-medium"
            )}
          >
            Events
          </Link>
        </nav>
        <Outlet />
      </div>
    </div>
  );
}
