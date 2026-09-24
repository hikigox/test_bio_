import { NavLink, Outlet } from "react-router-dom";
import DateRangeControl from "./DateRangeControl";
import { useAuth } from "../../context/AuthContext";
import ErrorBoundary from "../ErrorBoundary";

const NAV = [
  { to: "/", label: "Dashboard", end: true },
  { to: "/meters", label: "Medidores" },
  { to: "/anomalies", label: "Anomalías" },
];

export default function AppShell() {
  const { logout } = useAuth();

  return (
    <div className="min-h-screen flex bg-slate-50">
      <aside className="w-56 bg-white border-r px-4 py-6 space-y-1">
        <h2 className="font-semibold text-slate-900 mb-4">Energy Management</h2>
        {NAV.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.end}
            className={({ isActive }) =>
              `block px-3 py-2 rounded text-sm ${isActive ? "bg-blue-50 text-blue-700" : "text-slate-600 hover:bg-slate-100"}`
            }>
            {item.label}
          </NavLink>
        ))}
        <button onClick={logout} className="mt-8 text-sm text-slate-400 hover:text-slate-600">Cerrar sesión</button>
      </aside>
      <div className="flex-1 flex flex-col">
        <header className="h-14 bg-white border-b flex items-center justify-end px-6">
          <DateRangeControl />
        </header>
        <main className="flex-1 p-6">
          <ErrorBoundary>
            <Outlet />
          </ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
