import Link from "next/link";
import type { Runbook } from "@/lib/api";
import { listRunbooks } from "@/lib/api";
import { fmtTime } from "@/lib/utils";
import {
  CreateRunbookButton,
  DeleteRunbookButton,
} from "./create-runbook-button";

export const dynamic = "force-dynamic";

export default async function RunbooksPage() {
  let runbooks: Runbook[];
  try {
    runbooks = await listRunbooks();
  } catch {
    runbooks = [];
  }

  return (
    <div className="p-8 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Runbooks</h1>
          <p className="text-gray-400 text-sm mt-1">
            {runbooks.length} runbooks
          </p>
        </div>
        <CreateRunbookButton />
      </div>

      <div className="space-y-3">
        {runbooks.length === 0 && (
          <div className="bg-gray-900 border border-gray-800 rounded-lg px-6 py-10 text-center">
            <p className="text-gray-500 text-sm">No runbooks configured.</p>
            <p className="text-gray-600 text-xs mt-2">
              Create one via{" "}
              <code className="font-mono bg-gray-800 px-1.5 py-0.5 rounded">
                POST /api/v1/runbooks
              </code>
            </p>
          </div>
        )}
        {runbooks.map((rb) => (
          <div
            key={rb.ID}
            className="bg-gray-900 border border-gray-800 rounded-lg p-5"
          >
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-3">
                  <Link
                    href={`/runbooks/${rb.ID}`}
                    className="text-base font-semibold text-white hover:text-blue-400"
                  >
                    {rb.Name}
                  </Link>
                  <span
                    className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium border ${
                      rb.Enabled
                        ? "bg-green-900/30 text-green-400 border-green-800"
                        : "bg-gray-800 text-gray-500 border-gray-700"
                    }`}
                  >
                    {rb.Enabled ? "enabled" : "disabled"}
                  </span>
                </div>
                <p className="text-xs text-gray-500 mt-1">
                  Updated: {fmtTime(rb.UpdatedAt)} · ID:{" "}
                  <span className="font-mono">{rb.ID}</span>
                </p>
              </div>
              <DeleteRunbookButton id={rb.ID} />
            </div>
            {/* Markdown preview (first 300 chars) */}
            <pre className="mt-3 text-xs text-gray-400 bg-gray-950 rounded p-3 overflow-hidden max-h-24 whitespace-pre-wrap">
              {rb.Content?.slice(0, 300)}
              {rb.Content?.length > 300 ? "…" : ""}
            </pre>
          </div>
        ))}
      </div>
    </div>
  );
}
