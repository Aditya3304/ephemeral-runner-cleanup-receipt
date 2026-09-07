export type Verdict = "pass" | "fail" | "partial";
export type Component =
  "workspace" | "credentials" | "logs" | "resources" | "runner_disposal";
export type Observation = {
  status: "verified" | "failed" | "unsupported" | "unobservable";
  observer: "trusted" | "job" | "none";
  reason: string;
};
export type ObjectRef = {
  store: string;
  bucket: string;
  key: string;
  version_id: string;
  sha256: string;
  size_bytes: number;
};
export type Receipt = {
  id: string;
  job_id: string;
  job_name: string;
  run_id: string;
  run_attempt: number;
  repository: string;
  provider: string;
  workflow: string;
  source_revision: string;
  verdict: Verdict;
  signature_state: string;
  started_at: string;
  completed_at: string;
  finalized_at: string;
  ingested_at: string;
  coverage: Record<Component, Observation>;
  receipt_object: ObjectRef;
  bundle_object: ObjectRef;
  log_objects: ObjectRef[];
  signer_identity: string;
  signer_issuer: string;
  trust_root_sha256: string;
  resource_summary: {
    evidence_status: string;
    finalizer_revision: string;
    resource_namespace: string;
    runner_namespace: string;
  };
  incident: null | {
    id: string;
    state: "open" | "resolved";
    reason: string;
    reason_code: string;
    created_at: string;
    resolved_at: string | null;
    github_issue_url: string | null;
  };
};
export type Page = { items: Receipt[]; next_cursor?: string };
export type Verification = {
  signature: "verified";
  artifacts: "verified";
  verdict: Verdict;
  checked_at: string;
  receipt_sha256: string;
};
export type Filters = {
  view: "receipts" | "incidents" | "guide";
  q: string;
  verdict: string;
  repository: string;
  incident_state: string;
  from: string;
  to: string;
  receipt: string;
};
export const REPOSITORY = "Aditya3304/ephemeral-runner-cleanup-receipt";
export const components: Component[] = [
  "workspace",
  "credentials",
  "resources",
  "logs",
  "runner_disposal",
];
export const labels: Record<Component, string> = {
  workspace: "Workspace",
  credentials: "Credentials",
  resources: "Resources",
  logs: "Job logs",
  runner_disposal: "Runner",
};
export const titles: Record<string, string> = {
  isolation: "Isolation check",
  pass: "Successful command",
  fail: "Failed command",
  cancel: "Graceful cancellation",
  "missing-post": "Interrupted runner",
  restart: "Coordinator restart",
};
export const verdictLabels: Record<Verdict, string> = {
  pass: "Clean",
  fail: "Needs attention",
  partial: "Incomplete",
};
export const dateTime = (date: string) =>
  new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(new Date(date));
export const timeOnly = (date: string) =>
  new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(date));
export const shortDate = (date: string) =>
  new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit" }).format(
    new Date(date),
  );
export const bytes = (size: number) =>
  size < 1024 ? `${size} B` : `${(size / 1024).toFixed(1)} KB`;
export const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
export const emptyFilters: Filters = {
  view: "receipts",
  q: "",
  verdict: "",
  repository: "",
  incident_state: "",
  from: "",
  to: "",
  receipt: "",
};
export function readLocation(): Filters {
  const q = new URLSearchParams(window.location.search);
  return {
    ...emptyFilters,
    ...Object.fromEntries(
      Object.keys(emptyFilters).map((k) => [k, q.get(k) || ""]),
    ),
    view:
      q.get("view") === "incidents"
        ? "incidents"
        : q.get("view") === "guide"
          ? "guide"
          : "receipts",
  };
}
export function dayBoundary(date: string, end = false) {
  const d = new Date(`${date}T00:00:00`);
  if (end) d.setDate(d.getDate() + 1);
  return d.toISOString();
}
export function filterError(f: Filters): string {
  if (f.verdict && !["pass", "fail", "partial"].includes(f.verdict))
    return "This cleanup filter is not recognized.";
  if (
    f.incident_state &&
    !["open", "resolved", "none"].includes(f.incident_state)
  )
    return "This incident filter is not recognized.";
  for (const d of [f.from, f.to]) {
    if (
      d &&
      (!/^\d{4}-\d{2}-\d{2}$/.test(d) ||
        !Number.isFinite(Date.parse(`${d}T00:00:00Z`)) ||
        new Date(`${d}T00:00:00Z`).toISOString().slice(0, 10) !== d)
    )
      return "Choose valid completion dates.";
  }
  if (f.from && f.to && f.from > f.to)
    return "The end date must be on or after the start date.";
  if (f.q.length > 255 || f.repository.length > 255)
    return "Search and repository filters must be 255 characters or fewer.";
  return "";
}
export function listPath(f: Filters, cursor = "") {
  const p = new URLSearchParams({ limit: "25" });
  for (const k of ["q", "verdict", "repository", "incident_state"] as const) {
    if (f[k]) p.set(k, f[k]);
  }
  if (f.from) p.set("from", dayBoundary(f.from));
  if (f.to) p.set("to", dayBoundary(f.to, true));
  if (cursor) p.set("cursor", cursor);
  return `/v1/${f.view === "incidents" ? "incidents" : "receipts"}?${p}`;
}
export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
  ) {
    super(code);
  }
}
export async function request<T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(path, {
    credentials: "omit",
    cache: "no-store",
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(50000)])
      : AbortSignal.timeout(50000),
  });
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    throw new APIError(response.status, "invalid_response");
  }
  if (!response.ok)
    throw new APIError(
      response.status,
      typeof body === "object" && body && "error" in body
        ? String(body.error)
        : "request_failed",
    );
  return body as T;
}
export function apiError(error: unknown, verification = false) {
  if (error instanceof APIError) {
    if (error.status === 404)
      return "This receipt was not found. It may belong to a different local database.";
    if (error.status === 429)
      return "Another verification is running. Try again in a few seconds.";
    if (error.status === 422 && verification)
      return "Verification was rejected. Do not treat this receipt as freshly verified.";
  }
  return verification
    ? "Verification is unavailable. The API or archive may be offline. The stored cleanup verdict has not changed."
    : "The local API is unavailable. Start the local services, then try again.";
}
