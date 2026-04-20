"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { updateRunbook } from "@/lib/api";

export function EditRunbookButton({
  id,
  initialContent,
}: {
  id: string;
  initialContent: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [content, setContent] = useState(initialContent);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function handleSave() {
    setLoading(true);
    setError("");
    try {
      await updateRunbook(id, content);
      setOpen(false);
      router.refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "保存失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="px-3 py-1.5 text-sm border border-gray-700 hover:border-gray-500 text-gray-300 hover:text-white rounded-md font-medium"
      >
        编辑
      </button>

      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70">
          <div className="bg-gray-900 border border-gray-700 rounded-xl w-full max-w-2xl mx-4 flex flex-col max-h-[90vh]">
            <div className="px-6 py-4 border-b border-gray-800 flex items-center justify-between">
              <h2 className="text-base font-semibold text-white">
                编辑 Runbook
              </h2>
              <button
                onClick={() => setOpen(false)}
                className="text-gray-500 hover:text-gray-300 text-xl leading-none"
              >
                &times;
              </button>
            </div>

            <div className="px-6 py-4 flex-1 overflow-auto">
              <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                rows={20}
                className="w-full bg-gray-950 border border-gray-700 rounded-lg p-3 text-sm text-gray-200 font-mono resize-none focus:outline-none focus:border-blue-600"
              />
              {error && <p className="text-red-400 text-xs mt-2">{error}</p>}
            </div>

            <div className="px-6 py-4 border-t border-gray-800 flex justify-end gap-3">
              <button
                onClick={() => { setOpen(false); setContent(initialContent); }}
                className="px-4 py-1.5 text-sm text-gray-400 hover:text-gray-200"
              >
                取消
              </button>
              <button
                onClick={handleSave}
                disabled={loading}
                className="px-4 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-md font-medium"
              >
                {loading ? "保存中..." : "保存"}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
