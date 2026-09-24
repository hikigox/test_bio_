import { Component, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
}

// Minimal render-time error boundary: without this, any uncaught render
// exception anywhere below (e.g. reading a field the backend can send back
// as JSON null) unmounts the whole React root to a blank white page with no
// feedback. This is a last-resort net, not a substitute for null-guarding
// individual pages.
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: unknown) {
    console.error(error);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="p-6 text-center">
          <p className="text-sm text-red-600">Ocurrió un error.</p>
          <button
            onClick={() => window.location.reload()}
            className="mt-2 border rounded px-3 py-1.5 text-sm"
          >
            Recargar
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
