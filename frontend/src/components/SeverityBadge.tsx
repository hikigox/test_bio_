import { severityColor } from "../lib/format";

export default function SeverityBadge({ severity }: { severity: "HIGH" | "MEDIUM" | "LOW" }) {
  return <span className={`px-2 py-0.5 rounded text-xs font-medium ${severityColor(severity)}`}>{severity}</span>;
}
