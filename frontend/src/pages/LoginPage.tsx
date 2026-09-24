import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { login as loginRequest } from "../api/auth";
import { useAuth } from "../context/AuthContext";

export default function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const { login } = useAuth();
  const navigate = useNavigate();

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const token = await loginRequest(email, password);
      login(token);
      navigate("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : "error de inicio de sesión");
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-50">
      <form onSubmit={handleSubmit} className="bg-white p-8 rounded-lg shadow-sm w-80 space-y-4">
        <h1 className="text-xl font-semibold text-slate-900">AI Energy Management</h1>
        <div>
          <label htmlFor="email" className="block text-sm text-slate-600">Email</label>
          <input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
            className="mt-1 w-full border rounded px-3 py-2" required />
        </div>
        <div>
          <label htmlFor="password" className="block text-sm text-slate-600">Contraseña</label>
          <input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)}
            className="mt-1 w-full border rounded px-3 py-2" required />
        </div>
        {error && <p className="text-sm text-red-600">{error}</p>}
        <button type="submit" className="w-full bg-blue-600 text-white rounded py-2">Entrar</button>
      </form>
    </div>
  );
}
