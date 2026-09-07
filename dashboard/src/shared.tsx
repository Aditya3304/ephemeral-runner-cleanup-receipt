import { useState, type KeyboardEvent } from "react";
import {
  Check,
  Copy,
  CheckCheck,
  ArrowUpRight,
  TriangleAlert,
  Minus,
  FolderClosed,
  KeyRound,
  Box,
  FileText,
  Server,
} from "lucide-react";
import { type Component, type Verdict, verdictLabels } from "./data";
// Native buttons handle Enter/Space; roving focus adds the tablist arrow keys.
export function tabKeys(event: KeyboardEvent<HTMLDivElement>) {
  const tabs = Array.from(
    event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
  );
  const index = tabs.indexOf(event.target as HTMLButtonElement);
  if (index < 0) return;
  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? tabs.length - 1
        : event.key === "ArrowRight"
          ? (index + 1) % tabs.length
          : event.key === "ArrowLeft"
            ? (index + tabs.length - 1) % tabs.length
            : -1;
  if (next >= 0) {
    event.preventDefault();
    tabs[next].focus();
    tabs[next].click();
  }
}
export const componentIcons: Record<Component, typeof Box> = {
  workspace: FolderClosed,
  credentials: KeyRound,
  resources: Box,
  logs: FileText,
  runner_disposal: Server,
};
export function Badge({ verdict }: { verdict: Verdict }) {
  const Icon =
    verdict === "pass"
      ? CheckCheck
      : verdict === "fail"
        ? TriangleAlert
        : Minus;
  return (
    <span className={`badge ${verdict}`}>
      <Icon size={13} />
      {verdictLabels[verdict] ?? verdict}
    </span>
  );
}
export function Mark({ small = false }: { small?: boolean }) {
  return (
    <svg
      viewBox="0 0 40 40"
      className={small ? "brand-mark small" : "brand-mark"}
      aria-hidden="true"
    >
      <path
        d="M8 31 20 7l12 24M13 23h14"
        fill="none"
        stroke="currentColor"
        strokeWidth="4.5"
        strokeLinejoin="round"
      />
      <path d="M6 35h28" stroke="currentColor" strokeWidth="2" />
    </svg>
  );
}
export function CopyButton({
  value,
  label = "Copy",
  compact = false,
}: {
  value: string;
  label?: string;
  compact?: boolean;
}) {
  const [state, setState] = useState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setState("Copied");
    } catch {
      setState("Copy unavailable");
    }
  }
  return (
    <button
      className={`copy-button ${compact ? "compact" : ""}`}
      aria-label={state || label}
      title={state || label}
      onClick={() => void copy()}
    >
      {state === "Copied" ? <Check size={13} /> : <Copy size={13} />}
      <span className={compact ? "sr-only" : ""} role="status">
        {state || label}
      </span>
    </button>
  );
}
export function Empty({
  title,
  children,
  action,
}: {
  title: string;
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div className="empty-state">
      <div className="empty-mark">
        <FileText size={28} strokeWidth={1} />
      </div>
      <h3>{title}</h3>
      <p>{children}</p>
      {action}
    </div>
  );
}
export function Footer() {
  return (
    <footer className="app-footer">
      <span>
        <i /> BUILT FOR EPHEMERAL RUNS. KEPT AS EVIDENCE.
      </span>
      <span>
        On this laptop <ArrowUpRight size={12} />
      </span>
    </footer>
  );
}
