export default function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="bg-white rounded-lg shadow-sm p-4 text-center">
      <p className="text-sm text-red-600">{message}</p>
      {onRetry && <button onClick={onRetry} className="mt-2 border rounded px-3 py-1.5 text-sm">Reintentar</button>}
    </div>
  );
}
