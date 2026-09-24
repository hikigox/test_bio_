import { statusColor } from "../lib/format";

export default function StatusBadge({ status }: { status: "UNKNOWN" | "OK" | "ALERT" | "CRITICAL" }) {
  return <span className={`px-2 py-0.5 rounded text-xs font-medium ${statusColor(status)}`}>{status}</span>;
}
