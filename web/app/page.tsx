import Link from "next/link";
import { listAlerts, listInvestigations } from "@/lib/api";
import { severityBadge, invStatusBadge, fmtTime } from "@/lib/utils";

export const dynamic = "force-dynamic";

export default async function DashboardPage() {
  const [alertsData, invsData] = await Promise.allSettled([
    listAlerts({ limit: 5 }),
    listInvestigations({ limit: 5 }),
  ]);

  const alerts = alertsData.status === "fulfilled" ? alertsData.value.alerts : [];
  const invs = invsData.status === "fulfilled" ? invsData.value.investigations : [];

  return (
    <div className="p-8 space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <p className="text-gray-400 text-sm mt-1">Recent alerts and investigations</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Recent Alerts */}
        <section>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wider">Recent Alerts</h2>
            <Link href="/alerts" className="text-xs text-blue-400 hover:text-blue-300">View all →</Link>
          </div>
          <div className="bg-gray-900 rounded-lg border border-gray-800 divide-y divide-gray-800">
            {alerts.length === 0 && (
              <p className="px-4 py-6 text-center text-gray-500 text-sm">No alerts yet</p>
            )}
            {alerts.map((a) => (
              <div key={a.ID} className="px-4 py-3 flex items-start gap-3">
                <span className={`mt-0.5 inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${severityBadge(a.Severity)}`}>
                  {a.Severity}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-white truncate">{a.Title}</p>
                  <p className="text-xs text-gray-500 mt-0.5">{a.Service || a.Source} · {fmtTime(a.ReceivedAt)}</p>
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* Recent Investigations */}
        <section>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wider">Recent Investigations</h2>
            <Link href="/investigations" className="text-xs text-blue-400 hover:text-blue-300">View all →</Link>
          </div>
          <div className="bg-gray-900 rounded-lg border border-gray-800 divide-y divide-gray-800">
            {invs.length === 0 && (
              <p className="px-4 py-6 text-center text-gray-500 text-sm">No investigations yet</p>
            )}
            {invs.map((inv) => (
              <Link key={inv.ID} href={`/investigations/${inv.ID}`} className="px-4 py-3 flex items-start gap-3 hover:bg-gray-800 transition-colors block">
                <span className={`mt-0.5 inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${invStatusBadge(inv.Status)}`}>
                  {inv.Status}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-white truncate">{inv.RootCause || "Investigation in progress…"}</p>
                  <p className="text-xs text-gray-500 mt-0.5">{inv.LLMProvider}/{inv.LLMModel} · {fmtTime(inv.CreatedAt)}</p>
                </div>
              </Link>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}
