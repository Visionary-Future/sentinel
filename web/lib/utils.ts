import type { Severity, InvStatus, AlertStatus } from "./api";

export function severityColor(s: Severity): string {
  return { critical: "text-red-500", warning: "text-yellow-500", info: "text-blue-400" }[s] ?? "text-gray-400";
}

export function severityBadge(s: Severity): string {
  return (
    {
      critical: "bg-red-100 text-red-700 border-red-200",
      warning: "bg-yellow-100 text-yellow-700 border-yellow-200",
      info: "bg-blue-100 text-blue-700 border-blue-200",
    }[s] ?? "bg-gray-100 text-gray-600"
  );
}

export function invStatusBadge(s: InvStatus): string {
  return (
    {
      completed: "bg-green-100 text-green-700 border-green-200",
      running: "bg-blue-100 text-blue-700 border-blue-200",
      pending: "bg-gray-100 text-gray-600 border-gray-200",
      failed: "bg-red-100 text-red-700 border-red-200",
    }[s] ?? "bg-gray-100 text-gray-600"
  );
}

export function alertStatusBadge(s: AlertStatus): string {
  return (
    {
      open: "bg-orange-100 text-orange-700 border-orange-200",
      acked: "bg-yellow-100 text-yellow-700 border-yellow-200",
      resolved: "bg-green-100 text-green-700 border-green-200",
    }[s] ?? "bg-gray-100 text-gray-600"
  );
}

export function fmtTime(iso?: string): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleString("zh-CN", { hour12: false });
}

export function fmtDuration(startIso?: string, endIso?: string): string {
  if (!startIso || !endIso) return "—";
  const ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (ms < 0) return "—";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}
