import { useQuery } from "@tanstack/react-query";
import {
  RotateCcw,
  ChevronDown,
  ArrowUpRight,
  TriangleAlert,
  Check,
} from "lucide-react";
import { request, dateTime } from "./data";
type Recovery = {
  id: string;
  run_id: string;
  job_id: string;
  attempt_number: number;
  stage: string;
  result: "running" | "retry" | "exhausted" | "succeeded";
  error_code: string | null;
  receipt_id: string | null;
  next_retry_at: string | null;
  lease_expires_at: string | null;
};
export function RecoveryPanel({
  onSelect,
}: {
  onSelect: (id: string) => void;
}) {
  const query = useQuery({
    queryKey: ["recovery"],
    queryFn: ({ signal }) =>
      request<{ items: Recovery[]; limit: number }>("/v1/recovery", signal),
  });
  const rows = query.data?.items ?? [],
    unresolved = rows.filter((r) => r.result !== "succeeded").length;
  return (
    <details className="recovery-panel">
      <summary>
        <span className="recovery-label">
          <RotateCcw size={16} />
          Recovery watch
        </span>
        <span>
          {query.isError
            ? "Status unavailable"
            : query.isPending
              ? "Checking recovery…"
              : unresolved
                ? `${unresolved} unresolved`
                : "No unresolved recoveries in this view"}
          <ChevronDown size={14} />
        </span>
      </summary>
      <div className="recovery-content">
        <p>
          The local watchdog checks every five minutes while WSL and Docker are
          running. These are operational records, not signed cleanup verdicts.
          Refresh to reload.
        </p>
        {query.isError ? (
          <div role="alert">
            Recovery status is unavailable. The worker retains its retry state
            locally while the API or database is offline.
          </div>
        ) : query.isPending ? (
          <p>Loading recovery records…</p>
        ) : rows.length === 0 ? (
          <p>No recovery attempts have been recorded yet.</p>
        ) : (
          <ul tabIndex={0} aria-label="Recovery records">
            {rows.map((r) => (
              <li key={r.id} className={r.result}>
                <span className="recovery-symbol">
                  {r.result === "succeeded" ? (
                    <Check size={17} />
                  ) : r.result === "exhausted" ? (
                    <TriangleAlert size={17} />
                  ) : (
                    <RotateCcw size={17} />
                  )}
                </span>
                <div>
                  <strong>{r.job_id}</strong>
                  <span className="mono">
                    {r.run_id.slice(0, 12)} · attempt {r.attempt_number}/6 ·{" "}
                    {r.stage}
                  </span>
                  <p>
                    {r.result === "succeeded"
                      ? "Recovered — verified receipt stored."
                      : r.result === "exhausted"
                        ? r.error_code === "invalid_evidence"
                          ? "Automatic retries stopped. Retained evidence does not meet the finalizer’s required format or identity binding."
                          : "Automatic retries stopped. Review the local service and retained evidence."
                        : r.result === "running"
                          ? "Worker is processing this run."
                          : "Recovery is waiting to retry."}
                  </p>
                  {r.next_retry_at && (
                    <time dateTime={r.next_retry_at}>
                      Eligible {dateTime(r.next_retry_at)}
                    </time>
                  )}
                </div>
                {r.receipt_id ? (
                  <button
                    className="text-button"
                    onClick={() => onSelect(r.receipt_id!)}
                  >
                    Open receipt
                    <ArrowUpRight size={13} />
                  </button>
                ) : (
                  <span className="recovery-status">
                    {r.result === "exhausted"
                      ? "Needs operator"
                      : r.result === "running"
                        ? "In progress"
                        : "Retry pending"}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
        {rows.length >= 100 && (
          <p>
            Showing up to 100 recent run states. The local watchdog status
            retains the complete history.
          </p>
        )}
      </div>
    </details>
  );
}
