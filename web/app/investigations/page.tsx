import Link from "next/link";
import { listInvestigations } from "@/lib/api";
import { invStatusBadge, fmtTime, fmtDuration } from "@/lib/utils";

export const dynamic = "force-dynamic";

export default async function InvestigationsPage({
  searchParams,
}: {
  searchParams: Promise<{ status?: string; offset?: string }>;
}) {
  const sp = await searchParams;
  const offset = Number(sp.offset ?? 0);
  const status = sp.status ?? "";

  let data;
  try {
    data = await listInvestigations({ limit: 20, offset, status: status || undefined });
  } catch {
    data = { investigations: [], total: 0, limit: 20, offset: 0 };
  }

  const { investigations, total, limit } = data;

  return (
    <div className="p-8 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Investigations</h1>
          <p className="text-gray-400 text-sm mt-1">{total} total</p>
        </div>
        {/* Status filter */}
        <div className="flex gap-2">
          {["", "running", "completed", "failed"].map((s) => (
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
              <th className="px-4 py-3 text-left">Status</th>
              <th className="px-4 py-3 text-left">Root Cause</th>
              <th className="px-4 py-3 text-left">LLM</th>
              <th className="px-4 py-3 text-left">Steps</th>
              <th className="px-4 py-3 text-left">Duration</th>
              <th className="px-4 py-3 text-left">Created</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {investigations.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-gray-500">
                  No investigations found
                </td>
              </tr>
            )}
            {investigations.map((inv) => (
              <tr key={inv.ID} className="hover:bg-gray-800 transition-colors">
                <td className="px-4 py-3">
                  <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${invStatusBadge(inv.Status)}`}>
                    {inv.Status}
                  </span>
                </td>
                <td className="px-4 py-3 max-w-xs">
                  <Link href={`/investigations/${inv.ID}`} className="text-white hover:text-blue-400 truncate block">
                    {inv.RootCause || <span className="text-gray-500 italic">in progress…</span>}
                  </Link>
                </td>
                <td className="px-4 py-3 text-gray-400 text-xs">{inv.LLMProvider}/{inv.LLMModel}</td>
                <td className="px-4 py-3 text-gray-400">{inv.Steps?.length ?? 0}</td>
                <td className="px-4 py-3 text-gray-400 text-xs">{fmtDuration(inv.StartedAt, inv.CompletedAt)}</td>
                <td className="px-4 py-3 text-gray-500 text-xs">{fmtTime(inv.CreatedAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      <div className="flex justify-between items-center text-sm text-gray-400">
        <span>{offset + 1}–{Math.min(offset + limit, total)} of {total}</span>
        <div className="flex gap-2">
          {offset > 0 && (
            <Link
              href={`?status=${status}&offset=${Math.max(0, offset - limit)}`}
              className="px-3 py-1.5 bg-gray-800 rounded hover:bg-gray-700 transition-colors"
            >
              Previous
            </Link>
          )}
          {offset + limit < total && (
            <Link
              href={`?status=${status}&offset=${offset + limit}`}
              className="px-3 py-1.5 bg-gray-800 rounded hover:bg-gray-700 transition-colors"
            >
              Next
            </Link>
          )}
        </div>
      </div>
    </div>
  );
}
