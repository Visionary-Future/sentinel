const BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export async function apiFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`API ${path} → ${res.status}: ${text}`);
  }
  return res.json() as Promise<T>;
}

// ── Types ──────────────────────────────────────────────────────────────────

export type Severity = "critical" | "warning" | "info";
export type AlertStatus = "open" | "acked" | "resolved";
export type InvStatus = "pending" | "running" | "completed" | "failed";

export interface Alert {
  ID: string;
  Source: string;
  Severity: Severity;
  Title: string;
  Description: string;
  Service: string;
  Labels: Record<string, string>;
  Fingerprint: string;
  Status: AlertStatus;
  ReceivedAt: string;
}

export interface ToolCall {
  ID: string;
  Name: string;
  Input: Record<string, unknown>;
  Result: string;
  Error?: string;
}

export interface Step {
  Index: number;
  Description: string;
  ToolCalls: ToolCall[];
  Analysis: string;
  StartedAt: string;
  CompletedAt: string;
}

export interface Investigation {
  ID: string;
  AlertID: string;
  RunbookID?: string;
  Status: InvStatus;
  RootCause: string;
  Resolution: string;
  Summary: string;
  Steps: Step[];
  LLMProvider: string;
  LLMModel: string;
  TokenUsage: number;
  StartedAt?: string;
  CompletedAt?: string;
  CreatedAt: string;
}

export interface Runbook {
  ID: string;
  Name: string;
  Content: string;
  Steps: string[];
  Enabled: boolean;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface PageResult<T> {
  data: T[];
  total: number;
  limit: number;
  offset: number;
}

// ── API calls ──────────────────────────────────────────────────────────────

export async function listAlerts(params?: {
  limit?: number;
  offset?: number;
  status?: string;
}) {
  const q = new URLSearchParams();
  if (params?.limit) q.set("limit", String(params.limit));
  if (params?.offset) q.set("offset", String(params.offset));
  if (params?.status) q.set("status", params.status);
  const qs = q.toString();
  const raw = await apiFetch<{
    alerts: Alert[] | null;
    total: number;
    limit: number;
    offset: number;
  }>(`/api/v1/alerts${qs ? "?" + qs : ""}`);
  return { ...raw, alerts: raw.alerts ?? [] };
}

export async function getAlert(id: string) {
  return apiFetch<Alert>(`/api/v1/alerts/${id}`);
}

export async function listInvestigations(params?: {
  limit?: number;
  offset?: number;
  status?: string;
}) {
  const q = new URLSearchParams();
  if (params?.limit) q.set("limit", String(params.limit));
  if (params?.offset) q.set("offset", String(params.offset));
  if (params?.status) q.set("status", params.status);
  const qs = q.toString();
  const raw = await apiFetch<{
    investigations: Investigation[] | null;
    total: number;
    limit: number;
    offset: number;
  }>(`/api/v1/investigations${qs ? "?" + qs : ""}`);
  return { ...raw, investigations: raw.investigations ?? [] };
}

export async function getInvestigation(id: string) {
  return apiFetch<Investigation>(`/api/v1/investigations/${id}`);
}

export async function listRunbooks() {
  const raw = await apiFetch<{ runbooks: Runbook[] | null }>(
    `/api/v1/runbooks`,
  );
  return raw.runbooks ?? [];
}

export async function createRunbook(content: string): Promise<Runbook> {
  const BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  const res = await fetch(`${BASE}/api/v1/runbooks`, {
    method: "POST",
    headers: { "Content-Type": "text/plain" },
    body: content,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`创建失败: ${text}`);
  }
  return res.json();
}

export async function updateRunbook(
  id: string,
  content: string,
): Promise<Runbook> {
  const BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  const res = await fetch(`${BASE}/api/v1/runbooks/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "text/plain" },
    body: content,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`更新失败: ${text}`);
  }
  return res.json();
}

export async function deleteRunbook(id: string): Promise<void> {
  await apiFetch(`/api/v1/runbooks/${id}`, { method: "DELETE" });
}
