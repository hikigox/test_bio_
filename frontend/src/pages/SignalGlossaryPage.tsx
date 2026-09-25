import { Link } from "react-router-dom";

interface SignalEntry {
  name: string;
  meaning: string;
  calculation: string;
}

const SIGNALS: SignalEntry[] = [
  {
    name: "Cambio de nivel sostenido",
    meaning:
      "El consumo cambió de nivel y se mantuvo así por un tiempo — no es un pico aislado, sino un nuevo \"normal\" para el medidor.",
    calculation:
      "Se busca un punto en el tiempo donde el consumo acumulado se aparta de su tendencia (método CUSUM) y se comprueba que la variación se sostiene después de ese punto, no solo en una lectura suelta.",
  },
  {
    name: "Lectura atípica puntual",
    meaning:
      "Una lectura horaria individual se desvía mucho de lo esperado para esa hora del día, sin que el resto del período cambie.",
    calculation:
      "Se compara cada lectura contra el perfil histórico de consumo de esa misma hora del día, usando un puntaje de desviación (z-score robusto) que ignora el ruido normal del medidor.",
  },
  {
    name: "Patrón horario alterado",
    meaning:
      "El perfil de consumo a lo largo del día (cuándo sube y cuándo baja) se desvió del patrón habitual, aunque el total no haya cambiado tanto.",
    calculation:
      "Se dispara cuando varias horas del día (3 o más) muestran lecturas atípicas de forma concentrada, en vez de una sola hora suelta.",
  },
  {
    name: "Inconsistencia eléctrica",
    meaning:
      "Voltaje, corriente o factor de potencia cambiaron de una forma que no cuadra con el consumo reportado — indicio de un problema de medición o cableado más que de un cambio real de uso.",
    calculation:
      "Se calcula una relación entre el consumo y el producto voltaje·corriente·factor de potencia por lectura, y se compara contra su valor de referencia histórico; se dispara si una fracción relevante de las lecturas se desvía de esa referencia.",
  },
  {
    name: "Problema de calidad de datos",
    meaning:
      "Lecturas fuera de rango físico posible (voltaje o corriente negativos, factor de potencia fuera de 0–1, etc.) o datos faltantes — el problema está en el dato, no necesariamente en el consumo real.",
    calculation:
      "Se cuentan las lecturas que violan límites físicos básicos y se dispara si superan una proporción mínima del total de lecturas del período.",
  },
];

export default function SignalGlossaryPage() {
  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <Link to="/anomalies" className="text-sm text-blue-600 hover:underline">
          ← Volver a Anomalías
        </Link>
        <h2 className="text-xl font-semibold text-slate-900 mt-2">Glosario de señales</h2>
        <p className="text-sm text-slate-500 mt-1">
          La IA analiza cada medidor con 5 detectores independientes. Cada uno puede disparar una
          "señal" cuando algo se sale de lo esperado; una anomalía puede tener varias señales a la vez.
        </p>
      </div>

      <div className="bg-white rounded-lg shadow-sm p-4">
        <h3 className="font-medium text-slate-900 mb-2">Umbral adaptativo (idea general)</h3>
        <p className="text-sm text-slate-600">
          Ningún umbral es fijo para todos los medidores: cada uno tiene su propio "umbral adaptativo",
          calculado como el mayor entre un piso mínimo y un múltiplo del ruido histórico propio de ese
          medidor. Así, un medidor con consumo naturalmente más variable no dispara señales por su
          variabilidad normal, y uno muy estable sí detecta cambios pequeños.
        </p>
      </div>

      <div className="space-y-4">
        {SIGNALS.map((s) => (
          <div key={s.name} className="bg-white rounded-lg shadow-sm p-4">
            <h3 className="font-medium text-slate-900">{s.name}</h3>
            <p className="text-sm text-slate-600 mt-1">{s.meaning}</p>
            <p className="text-xs text-slate-400 mt-2">
              <span className="font-medium text-slate-500">Cómo se calcula (en general): </span>
              {s.calculation}
            </p>
          </div>
        ))}
      </div>
    </div>
  );
}
