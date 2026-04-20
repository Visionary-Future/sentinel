"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { createRunbook, deleteRunbook } from "@/lib/api";

const TEMPLATE = `# Runbook 标题

## Trigger
- alert.title contains "关键词"
- alert.severity in [critical, warning]

## Steps
- 查询过去 30 分钟的错误日志
- 检查关键指标（延迟、错误率）
- 搜索历史相似告警
- 确认根因并给出处理建议

## Escalation
- team: ops
- channel: #ops-alerts
`;

export function DeleteRunbookButton({
  id,
  redirectTo,
}: {
  id: string;
  redirectTo?: string;
}) {
  const router = useRouter();
  const [loading, setLoading] = useState(false);

  async function handleDelete() {
    if (!confirm("确认删除该 Runbook？")) return;
    setLoading(true);
    try {
      await deleteRunbook(id);
      if (redirectTo) {
        router.push(redirectTo);
      } else {
        router.refresh();
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <button
      onClick={handleDelete}
      disabled={loading}
      className="px-2.5 py-1 text-xs text-red-400 hover:text-red-300 border border-red-900/50 hover:border-red-700 rounded disabled:opacity-50"
    >
      {loading ? "删除中..." : "删除"}
    </button>
  );
}

export function CreateRunbookButton() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [content, setContent] = useState(TEMPLATE);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit() {
    setLoading(true);
    setError("");
    try {
      await createRunbook(content);
      setOpen(false);
      setContent(TEMPLATE);
      router.refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "创建失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md font-medium"
      >
        + New Runbook
      </button>

      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70">
          <div className="bg-gray-900 border border-gray-700 rounded-xl w-full max-w-2xl mx-4 flex flex-col max-h-[90vh]">
            <div className="px-6 py-4 border-b border-gray-800 flex items-center justify-between">
              <h2 className="text-base font-semibold text-white">
                New Runbook
              </h2>
              <button
                onClick={() => setOpen(false)}
                className="text-gray-500 hover:text-gray-300 text-xl leading-none"
              >
                &times;
              </button>
            </div>

            <div className="px-6 py-4 flex-1 overflow-auto">
              <p className="text-xs text-gray-500 mb-2">
                Markdown 格式。第一行{" "}
                <code className="bg-gray-800 px-1 rounded"># 标题</code> 为
                runbook 名称。
              </p>
              <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                rows={18}
                className="w-full bg-gray-950 border border-gray-700 rounded-lg p-3 text-sm text-gray-200 font-mono resize-none focus:outline-none focus:border-blue-600"
              />
              {error && <p className="text-red-400 text-xs mt-2">{error}</p>}
            </div>

            <div className="px-6 py-4 border-t border-gray-800 flex justify-end gap-3">
              <button
                onClick={() => setOpen(false)}
                className="px-4 py-1.5 text-sm text-gray-400 hover:text-gray-200"
              >
                取消
              </button>
              <button
                onClick={handleSubmit}
                disabled={loading}
                className="px-4 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-md font-medium"
              >
                {loading ? "创建中..." : "创建"}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
