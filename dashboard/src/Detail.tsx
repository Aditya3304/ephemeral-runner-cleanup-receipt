import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ArrowUpRight,
  Check,
  ChevronDown,
  Download,
  Fingerprint,
  GitBranch,
  LoaderCircle,
  ShieldCheck,
  TriangleAlert,
  Clock3,
  Link2,
  X,
} from "lucide-react";
import {
  apiError,
  bytes,
  components,
  dateTime,
  labels,
  request,
  timeOnly,
  titles,
  uuid,
  type Component,
  type ObjectRef,
  type Receipt,
  type Verification,
} from "./data";
import { Badge, CopyButton, Empty, componentIcons, tabKeys } from "./shared";

function Reference({
  name,
  refObject,
}: {
  name: string;
  refObject: ObjectRef;
}) {
  return (
    <details className="reference">
      <summary>
        <span>
          <Link2 size={14} />
          {name}
        </span>
        <span>
          {bytes(refObject.size_bytes)}
          <ChevronDown size={12} />
        </span>
      </summary>
      <div className="reference-body">
        <div className="tiny-label">
          SHA-256{" "}
          <CopyButton
            compact
            value={refObject.sha256}
            label={`Copy ${name} digest`}
          />
        </div>
        <code>{refObject.sha256}</code>
        <div className="tiny-label">
          EXACT VERSION{" "}
          <CopyButton
            compact
            value={refObject.version_id}
            label={`Copy ${name} version`}
          />
        </div>
        <code>{refObject.version_id}</code>
        <p>
          {refObject.store} / {refObject.bucket}
        </p>
        <code>{refObject.key}</code>
      </div>
    </details>
  );
}
export function CoverageMap({
  receipt,
  component,
  onSelect,
}: {
  receipt: Receipt;
  component: Component;
  onSelect: (c: Component) => void;
}) {
  const trusted = components.filter(
    (c) =>
      receipt.coverage[c].status === "verified" &&
      receipt.coverage[c].observer === "trusted",
  ).length;
  const points = [
    [180, 31],
    [307, 104],
    [255, 228],
    [105, 228],
    [53, 104],
  ];
  return (
    <div className="coverage-map" aria-label="Interactive cleanup coverage map">
      <svg className="coverage-lines" viewBox="0 0 360 265" aria-hidden="true">
        <circle cx="180" cy="130" r="89" className="orbit" />
        <circle cx="180" cy="130" r="65" className="inner-orbit" />
        {components.map((c, i) => (
          <path
            key={c}
            d={`M180 130 L${points[i][0]} ${points[i][1]}`}
            className={`spoke ${receipt.coverage[c].status} ${component === c ? "active" : ""}`}
          />
        ))}
        <path
          d="M170 45h20M170 215h20M95 120v20M265 120v20"
          className="ticks"
        />
      </svg>
      <div className={`map-center ${receipt.verdict}`}>
        <span className="map-count">
          {trusted}
          <small>/5</small>
        </span>
        <span className="tiny-label">TRUSTED CHECKS</span>
      </div>
      {components.map((c) => {
        const Icon = componentIcons[c];
        const observation = receipt.coverage[c];
        return (
          <button
            key={c}
            type="button"
            className={`coverage-node node-${c} ${observation.status} ${component === c ? "selected" : ""}`}
            onClick={() => onSelect(c)}
            aria-pressed={component === c}
            aria-label={`${labels[c]}: ${observation.status}, ${observation.observer} observer`}
          >
            <span className="node-symbol">
              <Icon size={17} strokeWidth={1.7} />
              <i>
                {observation.status === "verified" ? (
                  <Check size={8} />
                ) : observation.status === "failed" ? (
                  "!"
                ) : (
                  "?"
                )}
              </i>
            </span>
            <span>{labels[c]}</span>
          </button>
        );
      })}
    </div>
  );
}
export function Detail({
  id,
  initialComponent,
  onClose,
}: {
  id: string;
  initialComponent?: Component;
  onClose: () => void;
}) {
  const valid = uuid.test(id);
  const query = useQuery({
    queryKey: ["receipt", id],
    queryFn: ({ signal }) => request<Receipt>(`/v1/receipts/${id}`, signal),
    enabled: valid,
  });
  const [component, setComponent] = useState<Component>(
    initialComponent ?? "workspace",
  );
  const [tab, setTab] = useState<"coverage" | "archive" | "timeline">(
    "coverage",
  );
  const verification = useMutation({
    mutationFn: async () => {
      const v = await request<Verification>(`/v1/receipts/${id}/verification`);
      if (
        v.signature !== "verified" ||
        v.artifacts !== "verified" ||
        !v.checked_at ||
        v.receipt_sha256 !== query.data?.receipt_object.sha256
      )
        throw new Error("Unexpected verification response");
      return v;
    },
  });
  const r = query.data;
  if (!valid || (query.isError && !r))
    return (
      <aside className="detail-panel">
        <Empty title="Receipt unavailable">
          {valid ? apiError(query.error) : "This receipt link is not valid."}
        </Empty>
        <button className="text-button" onClick={onClose}>
          Return to the ledger
        </button>
      </aside>
    );
  if (!r)
    return (
      <aside className="detail-panel detail-loading" aria-busy="true">
        <span className="eyebrow">OPENING RECEIPT</span>
        <div className="skeleton h-8 w-3/4" />
        <div className="skeleton skeleton-map" />
        <p>Loading stored evidence…</p>
      </aside>
    );
  const observation = r.coverage[component],
    ComponentIcon = componentIcons[component];
  const exportMetadata = () => {
    const blob = new Blob([JSON.stringify(r, null, 2) + "\n"], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `cleanup-metadata-${r.run_id}.json`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return (
    <aside
      className={`detail-panel receipt-${r.verdict}`}
      aria-label="Selected receipt"
      data-testid="receipt-detail"
    >
      <div className="detail-topline">
        <span className="eyebrow">
          <Fingerprint size={14} /> RECEIPT INSPECTOR
        </span>
        <button
          className="icon-button"
          onClick={onClose}
          title="Close receipt"
          aria-label="Close receipt"
        >
          <X size={16} />
        </button>
      </div>
      <div className="detail-title">
        <div>
          <h2>{titles[r.job_name] ?? r.job_name}</h2>
          <div className="mono muted">
            {r.run_id.slice(0, 12)} <span>· attempt {r.run_attempt}</span>
          </div>
        </div>
        <Badge verdict={r.verdict} />
      </div>
      {query.isError && (
        <div className="notice error" role="alert">
          Refresh failed. Previously loaded receipt shown.
        </div>
      )}
      <div
        className="detail-tabs"
        role="tablist"
        aria-label="Receipt information"
        onKeyDown={tabKeys}
      >
        {(["coverage", "archive", "timeline"] as const).map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={t === tab}
            tabIndex={t === tab ? 0 : -1}
            aria-controls={`panel-${t}`}
            id={`tab-${t}`}
            onClick={() => setTab(t)}
          >
            {t === "coverage"
              ? "Cleanup map"
              : t === "archive"
                ? "Evidence"
                : "Timeline"}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === "coverage" && (
          <>
            <CoverageMap
              receipt={r}
              component={component}
              onSelect={setComponent}
            />
            <div
              className={`observation-box ${observation.status}`}
              aria-live="polite"
            >
              <div className="observation-heading">
                <ComponentIcon size={16} />
                <strong>{labels[component]}</strong>
                <span>{observation.status}</span>
              </div>
              <p>
                {observation.status === "verified" &&
                observation.observer === "trusted"
                  ? "A trusted observer confirmed this cleanup check."
                  : observation.status === "failed"
                    ? "This cleanup check failed. Review the linked incident and preserved evidence."
                    : observation.observer === "job"
                      ? "Reported by the job. This is not an independent observation."
                      : "This check could not be fully confirmed. A clean exit is not proven."}
              </p>
              <div className="observation-source">
                <span className="dot" />
                {observation.observer === "trusted"
                  ? "Independent observation"
                  : observation.observer === "job"
                    ? "Untrusted job observation"
                    : "No observation available"}
              </div>
            </div>
          </>
        )}
        {tab === "archive" && (
          <div className="archive-view">
            <p className="section-note">
              Exact versions, preserved with the receipt. Expand an artifact to
              inspect or copy its reference.
            </p>
            <Reference name="Signed receipt" refObject={r.receipt_object} />
            <Reference name="Signature bundle" refObject={r.bundle_object} />
            {r.log_objects.map((ref, i) => (
              <Reference
                key={ref.version_id}
                name={`Job log${r.log_objects.length > 1 ? ` ${i + 1}` : ""}`}
                refObject={ref}
              />
            ))}
            <div className="archive-footnote">
              <ShieldCheck size={15} />
              <p>
                Re-verification checks all signed references, including the
                archived observations. These references are metadata, not
                downloadable signed bytes.
              </p>
            </div>
          </div>
        )}
        {tab === "timeline" && (
          <div className="timeline-view">
            <span className="eyebrow">THE RECORDED JOURNEY</span>
            {[
              {
                label: "Run started",
                time: r.started_at,
                note: "Disposable runner assigned",
              },
              {
                label: "Cleanup completed",
                time: r.completed_at,
                note: "Observed cleanup outcome recorded",
              },
              {
                label: "Receipt signed",
                time: r.finalized_at,
                note: "Finalizer attestation created",
              },
              {
                label: "Added to the ledger",
                time: r.ingested_at,
                note: "Independently verified at ingestion",
              },
            ].map((step, i) => (
              <div className="timeline-step" key={step.label}>
                <span className="timeline-dot">{i + 1}</span>
                <div>
                  <strong>{step.label}</strong>
                  <p>{step.note}</p>
                  <time dateTime={step.time} title={dateTime(step.time)}>
                    {dateTime(step.time)}
                  </time>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      {r.incident && (
        <div className="incident-note">
          <TriangleAlert size={18} />
          <div>
            <strong>
              {r.incident.state === "open"
                ? "An incident needs review"
                : "Incident resolved"}
            </strong>
            <p>{r.incident.reason}</p>
            <span className="mono">
              INCIDENT {r.incident.id.slice(0, 8)} ·{" "}
              {r.incident.state.toUpperCase()}
            </span>
          </div>
        </div>
      )}
      <div className="verification-box">
        <div className="verification-caption">
          <ShieldCheck size={16} />
          <strong>Proof, not a promise.</strong>
          <span className="tiny-label">RE-CHECK</span>
        </div>
        <p>
          Independently check the signature and every archived artifact now.
        </p>
        <button
          className="verify-button"
          disabled={verification.isPending}
          onClick={() => verification.mutate()}
        >
          {verification.isPending ? (
            <>
              <LoaderCircle size={16} className="spin" />
              Checking the evidence…
            </>
          ) : (
            <>
              <Fingerprint size={18} />
              {verification.isSuccess ? "Verify again" : "Verify this receipt"}
              <ArrowUpRight size={17} />
            </>
          )}
        </button>
        <div
          aria-live="polite"
          aria-atomic="true"
          className={`verification-result ${verification.isSuccess ? "success" : verification.isError ? "failure" : ""}`}
        >
          {verification.isPending ? (
            <>
              <Clock3 size={13} />
              <span>Signature and archive check in progress.</span>
            </>
          ) : verification.isError ? (
            <>
              <TriangleAlert size={15} />
              <span>{apiError(verification.error, true)}</span>
            </>
          ) : verification.isSuccess ? (
            <>
              <Check size={14} />
              <span>
                Signature + all artifacts verified
                <br />
                <time
                  dateTime={verification.data.checked_at}
                  title={dateTime(verification.data.checked_at)}
                >
                  Checked {timeOnly(verification.data.checked_at)} ·{" "}
                  {dateTime(verification.data.checked_at)}
                </time>
              </span>
            </>
          ) : (
            <>
              <Check size={13} />
              <span>
                Verified at ingestion · {shortIngestion(r.ingested_at)}
                <br />
                <span className="muted">
                  No fresh check performed in this view.
                </span>
              </span>
            </>
          )}
        </div>
      </div>
      <details className="identity-details">
        <summary>
          <span>Identity & signing trust</span>
          <ChevronDown size={14} />
        </summary>
        <dl>
          <dt>Repository</dt>
          <dd>{r.repository}</dd>
          <dt>Source revision</dt>
          <dd className="hash">
            <GitBranch size={12} />
            {r.source_revision}
            <CopyButton
              compact
              label="Copy source revision"
              value={r.source_revision}
            />
          </dd>
          <dt>Signer</dt>
          <dd>{r.signer_identity}</dd>
          <dt>Issuer</dt>
          <dd>{r.signer_issuer}</dd>
          <dt>Trust root digest</dt>
          <dd>
            <code>{r.trust_root_sha256}</code>
          </dd>
          <dt>Receipt ID</dt>
          <dd>
            {r.id}
            <CopyButton compact label="Copy receipt ID" value={r.id} />
          </dd>
        </dl>
      </details>
      <button className="metadata-export" onClick={exportMetadata}>
        <Download size={14} />
        Export metadata <span>JSON</span>
      </button>
    </aside>
  );
}
function shortIngestion(date: string) {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(date));
}
