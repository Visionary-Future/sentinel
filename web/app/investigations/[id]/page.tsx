import Link from "next/link";
import { getInvestigation } from "@/lib/api";
import { invStatusBadge, fmtTime, fmtDuration } from "@/lib/utils";
import { notFound } from "next/navigation";

export const dynamic = "force-dynamic";

export default async function InvestigationDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let inv;
  try {
    inv = await getInvestigation(id);
  } catch {
    notFound();
  }

  return (
    <div className="p-8 space-y-6 max-w-5xl">
      {/* Header */}
      <div className="flex items-start gap-4">
        <div className="flex-1">
          <div className="flex items-center gap-3 mb-2">
            <Link href="/investigations" className="text-gray-500 hover:text-gray-300 text-sm">← Investigations</Link>
          </div>
          <div className="flex items-center gap-3">
            <span className={`inline-flex items-center px-2.5 py-1 rounded text-xs font-medium border ${invStatusBadge(inv.Status)}`}>
              {inv.Status}
            </span>
            <span className="text-xs text-gray-500 font-mono">{inv.ID}</span>
          </div>
        </div>
        <div className="text-right text-xs text-gray-500 space-y-1">
          <div>LLM: <span className="text-gray-300">{inv.LLMProvider}/{inv.LLMModel}</span></div>
          <div>Tokens: <span className="text-gray-300">{inv.TokenUsage.toLocaleString()}</span></div>
          <div>Duration: <span className="text-gray-300">{fmtDuration(inv.StartedAt, inv.CompletedAt)}</span></div>
        </div>
      </div>

      {/* Root Cause & Resolution */}
      {inv.RootCause && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="bg-red-950/30 border border-red-900/50 rounded-lg p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-red-400 mb-2">Root Cause</h3>
            <p className="text-sm text-gray-200 leading-relaxed">{inv.RootCause}</p>
          </div>
          {inv.Resolution && (
            <div className="bg-green-950/30 border border-green-900/50 rounded-lg p-4">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-green-400 mb-2">Resolution</h3>
              <p className="text-sm text-gray-200 leading-relaxed">{inv.Resolution}</p>
            </div>
          )}
        </div>
      )}

      {/* Summary */}
      {inv.Summary && (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-gray-400 mb-2">Summary</h3>
          <p className="text-sm text-gray-200 leading-relaxed whitespace-pre-wrap">{inv.Summary}</p>
        </div>
      )}

      {/* Steps Timeline */}
      <div>
        <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-400 mb-4">
          Investigation Steps ({inv.Steps?.length ?? 0})
        </h2>
        <div className="space-y-4">
          {(inv.Steps ?? []).map((step, i) => (
            <div key={i} className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
              {/* Step header */}
              <div className="px-4 py-3 border-b border-gray-800 flex items-center gap-3">
                <span className="w-6 h-6 rounded-full bg-blue-900 text-blue-300 text-xs flex items-center justify-center font-mono font-bold shrink-0">
                  {step.Index}
                </span>
                <p className="text-sm text-white flex-1">{step.Description}</p>
                <span className="text-xs text-gray-500">{fmtDuration(step.StartedAt, step.CompletedAt)}</span>
              </div>

              {/* Tool calls */}
              {step.ToolCalls?.length > 0 && (
                <div className="divide-y divide-gray-800">
                  {step.ToolCalls.map((tc) => (
                    <div key={tc.ID} className="px-4 py-3">
                      <div className="flex items-center gap-2 mb-2">
                        <span className="text-xs font-mono px-2 py-0.5 bg-gray-800 text-purple-300 rounded">{tc.Name}</span>
                        {tc.Error && <span className="text-xs text-red-400">Error</span>}
                      </div>
                      {tc.Result && (
                        <pre className="text-xs text-gray-400 bg-gray-950 rounded p-3 overflow-x-auto whitespace-pre-wrap max-h-40">
                          {tc.Result}
                        </pre>
                      )}
                    </div>
                  ))}
                </div>
              )}

              {/* Analysis */}
              {step.Analysis && (
                <div className="px-4 py-3 bg-gray-950/50">
                  <p className="text-xs text-gray-400 font-semibold mb-1">Analysis</p>
                  <p className="text-sm text-gray-300 leading-relaxed whitespace-pre-wrap">{step.Analysis}</p>
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      <div className="text-xs text-gray-600 pt-4 border-t border-gray-800">
        Created: {fmtTime(inv.CreatedAt)} · Alert ID: {inv.AlertID}
      </div>
    </div>
  );
}
