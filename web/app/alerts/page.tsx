import Link from "next/link";
import { listAlerts } from "@/lib/api";
import { severityBadge, alertStatusBadge, fmtTime } from "@/lib/utils";

export const dynamic = "force-dynamic";

export default async function AlertsPage({
  searchParams,
}: {
  searchParams: Promise<{ status?: string; offset?: string }>;
}) {
  const sp = await searchParams;
  const offset = Number(sp.offset ?? 0);
  const status = sp.status ?? "";

  let data;
  try {
    data = await listAlerts({ limit: 20, offset, status: status || undefined });
  } catch {
    data = { alerts: [], total: 0, limit: 20, offset: 0 };
  }

  const { alerts, total, limit } = data;

  return (
    <div className="p-8 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Alerts</h1>
          <p className="text-gray-400 text-sm mt-1">{total} total</p>
        </div>
        <div className="flex gap-2">
          {["", "open", "acked", "resolved"].map((s) => (
            <Link
              key={s}
              href={s ? `?status=${s}` : "?"}
              className={`px-3 py-1.5 rounded text-xs font-medium transition-colors ${
                status === s
                  ? "bg-blue-600 text-white"
                  : "bg-gray-800 text-gray-300 hover:bg-gray-700"
              }`}
            >
              {s || "All"}
            </Link>
          ))}
        </div>
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-800 text-gray-400 text-xs uppercase tracking-wider">
              <th className="px-4 py-3 text-left">Severity</th>
              <th className="px-4 py-3 text-left">Title</th>
              <th className="px-4 py-3 text-left">Service</th>
              <th className="px-4 py-3 text-left">Source</th>
              <th className="px-4 py-3 text-left">Status</th>
              <th className="px-4 py-3 text-left">Received</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {alerts.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-gray-500">
                  No alerts found
                </td>
              </tr>
            )}
            {alerts.map((a) => (
              <tr key={a.ID} className="hover:bg-gray-800 transition-colors">
                <td className="px-4 py-3">
                  <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${severityBadge(a.Severity)}`}>
                    {a.Severity}
                  </span>
                </td>
                <td className="px-4 py-3 max-w-xs">
                  <p className="text-white truncate">{a.Title}</p>
                  {a.Description && (
                    <p className="text-gray-500 text-xs truncate mt-0.5">{a.Description}</p>
                  )}
                </td>
                <td className="px-4 py-3 text-gray-400 text-xs">{a.Service || "—"}</td>
                <td className="px-4 py-3 text-gray-500 text-xs">{a.Source}</td>
                <td className="px-4 py-3">
                  <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${alertStatusBadge(a.Status)}`}>
                    {a.Status}
                  </span>
                </td>
                <td className="px-4 py-3 text-gray-500 text-xs">{fmtTime(a.ReceivedAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex justify-between items-center text-sm text-gray-400">
        <span>{offset + 1}–{Math.min(offset + limit, total)} of {total}</span>
        <div className="flex gap-2">
          {offset > 0 && (
            <Link href={`?status=${status}&offset=${Math.max(0, offset - limit)}`}
              className="px-3 py-1.5 bg-gray-800 rounded hover:bg-gray-700 transition-colors">
              Previous
            </Link>
          )}
          {offset + limit < total && (
            <Link href={`?status=${status}&offset=${offset + limit}`}
              className="px-3 py-1.5 bg-gray-800 rounded hover:bg-gray-700 transition-colors">
              Next
            </Link>
          )}
        </div>
      </div>
    </div>
  );
}
