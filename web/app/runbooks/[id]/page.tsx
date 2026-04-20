import Link from "next/link";
import { notFound } from "next/navigation";
import { apiFetch } from "@/lib/api";
import type { Runbook } from "@/lib/api";
import { fmtTime } from "@/lib/utils";
import { DeleteRunbookButton } from "../create-runbook-button";
import { EditRunbookButton } from "./edit-runbook-button";

export const dynamic = "force-dynamic";

export default async function RunbookDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;

  let rb: Runbook;
  try {
    rb = await apiFetch<Runbook>(`/api/v1/runbooks/${id}`);
  } catch {
    notFound();
  }

  return (
    <div className="p-8 space-y-6 max-w-3xl">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="mb-2">
            <Link
              href="/runbooks"
              className="text-gray-500 hover:text-gray-300 text-sm"
            >
              ← Runbooks
            </Link>
          </div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold text-white">{rb.Name}</h1>
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
        <div className="flex items-center gap-2">
          <EditRunbookButton id={rb.ID} initialContent={rb.Content} />
          <DeleteRunbookButton id={rb.ID} redirectTo="/runbooks" />
        </div>
      </div>

      {/* Content */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-5">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-400 mb-3">
          Runbook Content
        </h2>
        <pre className="text-sm text-gray-300 font-mono whitespace-pre-wrap leading-relaxed">
          {rb.Content}
        </pre>
      </div>
    </div>
  );
}
