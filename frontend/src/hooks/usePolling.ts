import { useEffect, useRef, useState } from "react";

// usePolling corre `fetcher` cada `intervalMs` hasta que `isDone(data)` sea true.
// `key` identifica la corrida activa (p.ej. analysis_id): si cambia, se reinicia
// el polling; si no cambia, el efecto no se vuelve a montar (evita duplicar el intervalo).
export function usePolling<T>(
  fetcher: () => Promise<T>,
  isDone: (data: T | null) => boolean,
  intervalMs: number,
  key: string | number = "default"
) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<unknown>(null);
  const doneRef = useRef(false);

  useEffect(() => {
    doneRef.current = false;
    setError(null);
    let cancelled = false;

    async function tick() {
      if (cancelled || doneRef.current) return;
      // Sin este try/catch, un solo fetch fallido (p.ej. el análisis
      // desapareció porque el backend se reinició con otra base de datos, o
      // un hiccup de red) rechaza esta promesa sin que nadie la espere ni la
      // capture: el timeout nunca se reprograma y el polling muere en
      // silencio, dejando a quien lo usa (p.ej. el botón "Analizando…")
      // colgado para siempre sin ningún error visible.
      try {
        const result = await fetcher();
        if (cancelled) return;
        setData(result);
        if (isDone(result)) {
          doneRef.current = true;
          return;
        }
      } catch (err) {
        if (cancelled) return;
        doneRef.current = true;
        setError(err);
        return;
      }
      timeoutId = setTimeout(tick, intervalMs);
    }

    let timeoutId = setTimeout(tick, 0);
    return () => {
      cancelled = true;
      clearTimeout(timeoutId);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return { data, error };
}
