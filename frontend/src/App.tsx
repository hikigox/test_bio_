import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { useEffect, useState } from "react";
import { AuthProvider, useAuth } from "./context/AuthContext";
import { DateRangeProvider } from "./context/DateRangeContext";
import { getDashboardSummary } from "./api/dashboard";
import AppShell from "./components/layout/AppShell";
import LoginPage from "./pages/LoginPage";
import DashboardPage from "./pages/DashboardPage";
import MetersPage from "./pages/MetersPage";
import MeterDetailPage from "./pages/MeterDetailPage";
import AnomaliesPage from "./pages/AnomaliesPage";
import AnomalyDetailPage from "./pages/AnomalyDetailPage";

function ProtectedLayout() {
  const { token } = useAuth();
  const [dataRange, setDataRange] = useState<{ from: string; to: string } | null>(null);

  useEffect(() => {
    if (token) {
      getDashboardSummary().then((s) => setDataRange(s.data_range)).catch(() => {});
    }
  }, [token]);

  if (!token) return <Navigate to="/login" replace />;
  if (!dataRange) return <div className="p-6">Cargando…</div>;

  return (
    <DateRangeProvider dataRange={dataRange}>
      <AppShell />
    </DateRangeProvider>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route element={<ProtectedLayout />}>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/meters" element={<MetersPage />} />
            <Route path="/meters/:meterId" element={<MeterDetailPage />} />
            <Route path="/anomalies" element={<AnomaliesPage />} />
            <Route path="/anomalies/:id" element={<AnomalyDetailPage />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  );
}
