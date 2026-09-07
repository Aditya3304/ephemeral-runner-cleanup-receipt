import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useTable, flexRender, type ColumnDef } from "@tanstack/react-table";
import {
  ArrowDown,
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronDown,
  CircleHelp,
  Command,
  FileCheck2,
  Fingerprint,
  Layers,
  Laptop,
  LoaderCircle,
  Moon,
  RefreshCw,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Sun,
  TriangleAlert,
  X,
} from "lucide-react";
import {
  apiError,
  components,
  emptyFilters,
  filterError,
  labels,
  listPath,
  readLocation,
  REPOSITORY,
  request,
  shortDate,
  timeOnly,
  titles,
  type Component,
  type Filters,
  type Page,
  type Receipt,
} from "./data";
import { Badge, Empty, Footer, Mark, tabKeys } from "./shared";
import { Detail } from "./Detail";
import { RecoveryPanel } from "./Recovery";

function FiltersDialog({
  filters,
  onApply,
  onClose,
}: {
  filters: Filters;
  onApply: (f: Partial<Filters>) => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [draft, setDraft] = useState(filters);
  const error = filterError(draft);
  useEffect(() => {
    const dialog = ref.current;
    dialog?.showModal();
    return () => dialog?.close();
  }, []);
  return (
    <dialog
      className="filter-dialog"
      ref={ref}
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      aria-labelledby="filter-title"
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!error) onApply(draft);
        }}
      >
        <header>
          <div>
            <span className="eyebrow">NARROW THE EVIDENCE</span>
            <h2 id="filter-title">Find your receipt.</h2>
          </div>
          <button
            type="button"
            className="icon-button"
            aria-label="Close filters"
            onClick={onClose}
          >
            <X size={20} />
          </button>
        </header>
        <label>
          <span id="repository-label">Repository</span>
          <select
            aria-labelledby="repository-label"
            autoFocus
            value={draft.repository}
            onChange={(e) => setDraft({ ...draft, repository: e.target.value })}
          >
            <option value="">All repositories</option>
            <option value={REPOSITORY}>{REPOSITORY}</option>
            {draft.repository && draft.repository !== REPOSITORY && (
              <option value={draft.repository}>{draft.repository}</option>
            )}
          </select>
        </label>
        <label>
          <span id="incident-label">Incident state</span>
          <select
            aria-labelledby="incident-label"
            value={draft.incident_state}
            onChange={(e) =>
              setDraft({ ...draft, incident_state: e.target.value })
            }
          >
            <option value="">Any incident state</option>
            <option value="open">Open incident</option>
            <option value="resolved">Resolved incident</option>
            <option value="none">No incident</option>
          </select>
        </label>
        <fieldset>
          <legend>
            Cleanup completed <span>· your local time</span>
          </legend>
          <div className="date-inputs">
            <label>
              From
              <input
                type="date"
                value={draft.from}
                onChange={(e) => setDraft({ ...draft, from: e.target.value })}
              />
            </label>
            <label>
              Through
              <input
                type="date"
                value={draft.to}
                onChange={(e) => setDraft({ ...draft, to: e.target.value })}
              />
            </label>
          </div>
        </fieldset>
        {error && (
          <p role="alert" className="form-error">
            {error}
          </p>
        )}
        <footer>
          <button
            type="button"
            className="text-button"
            onClick={() =>
              setDraft({
                ...draft,
                repository: "",
                incident_state: "",
                from: "",
                to: "",
              })
            }
          >
            Reset these filters
          </button>
          <button className="primary-button" disabled={!!error}>
            Apply filters
            <ArrowRight size={16} />
          </button>
        </footer>
      </form>
    </dialog>
  );
}
function Ledger({
  rows,
  selected,
  onSelect,
}: {
  rows: Receipt[];
  selected: string;
  onSelect: (id: string, c?: Component) => void;
}) {
  const columns = useMemo<ColumnDef<{}, Receipt>[]>(
    () => [
      {
        id: "job",
        header: "JOB / RUN",
        cell: ({ row }) => (
          <button
            className="job-button"
            onClick={() => onSelect(row.original.id)}
            aria-label={`Open ${titles[row.original.job_name] ?? row.original.job_name} receipt`}
          >
            <span className={`job-symbol ${row.original.verdict}`}>
              <FileCheck2 size={19} strokeWidth={1.4} />
            </span>
            <span>
              <strong>
                {titles[row.original.job_name] ?? row.original.job_name}
              </strong>
              <span className="row-subtitle mono">
                {row.original.run_id.slice(0, 8)} <i /> {row.original.job_id}
              </span>
            </span>
          </button>
        ),
      },
      {
        id: "verdict",
        header: "CLEANUP",
        cell: ({ row }) => <Badge verdict={row.original.verdict} />,
      },
      {
        id: "coverage",
        header: "5 CHECKS",
        cell: ({ row }) => (
          <div className="mini-coverage">
            {components.map((c) => (
              <button
                key={c}
                className={`${row.original.coverage[c].status} observer-${row.original.coverage[c].observer}`}
                title={`${labels[c]} · ${row.original.coverage[c].status} · ${row.original.coverage[c].observer}`}
                aria-label={`Inspect ${labels[c]} for ${titles[row.original.job_name] ?? row.original.job_name}`}
                onClick={() => onSelect(row.original.id, c)}
              >
                {row.original.coverage[c].status === "verified" ? (
                  <Check size={9} />
                ) : row.original.coverage[c].status === "failed" ? (
                  "!"
                ) : (
                  "·"
                )}
              </button>
            ))}
          </div>
        ),
      },
      {
        id: "finished",
        header: "FINISHED",
        cell: ({ row }) => (
          <time dateTime={row.original.completed_at} className="finish-time">
            <strong>{timeOnly(row.original.completed_at)}</strong>
            <span>{shortDate(row.original.completed_at)}</span>
          </time>
        ),
      },
    ],
    [onSelect],
  );
  const table = useTable({
    features: {},
    columns,
    data: rows,
    getRowId: (r) => r.id,
  });
  return (
    <div className="ledger-scroll">
      <table className="ledger-table">
        <caption className="sr-only">
          Signed cleanup receipts. Select a job or a coverage check to inspect
          its evidence.
        </caption>
        <thead>
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id}>
              {group.headers.map((header) => (
                <th key={header.id} scope="col">
                  {flexRender(
                    header.column.columnDef.header,
                    header.getContext(),
                  )}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {table.getRowModel().rows.map((row) => (
            <tr
              key={row.id}
              data-selected={selected === row.id}
              data-verdict={row.original.verdict}
              aria-selected={selected === row.id}
              onClick={() => onSelect(row.id)}
            >
              {row.getAllCells().map((cell) => (
                <td key={cell.id} onClick={(e) => e.stopPropagation()}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
function Guide() {
  const [active, setActive] = useState(0);
  const steps = [
    {
      title: "Run in isolation",
      icon: Laptop,
      detail:
        "A disposable Kubernetes runner executes the job on this laptop. Its workspace and temporary credentials belong to that run.",
    },
    {
      title: "Observe the cleanup",
      icon: Search,
      detail:
        "Independent observations cover workspace, credentials, resources, logs, and runner disposal. Missing coverage can never become a passing verdict.",
    },
    {
      title: "Preserve & sign",
      icon: Fingerprint,
      detail:
        "The trusted finalizer preserves exact artifact versions and signs a canonical receipt using the private local Sigstore services.",
    },
    {
      title: "Verify independently",
      icon: ShieldCheck,
      detail:
        "The API checks the signature and archived artifacts before committing metadata. The Verify button repeats those checks when you ask.",
    },
  ];
  return (
    <section className="guide-view">
      <div className="guide-intro">
        <span className="eyebrow">A CHAIN YOU CAN INSPECT</span>
        <h2>
          Gone from the runner.
          <br />
          <em>Kept in the record.</em>
        </h2>
        <p>
          A signature identifies the finalizer. Observations establish what it
          could actually see. Both matter.
        </p>
      </div>
      <div
        className="guide-steps"
        role="tablist"
        aria-label="Evidence journey"
        onKeyDown={tabKeys}
      >
        {steps.map((s, i) => {
          const Icon = s.icon;
          return (
            <button
              key={s.title}
              role="tab"
              id={`journey-${i}`}
              aria-selected={active === i}
              tabIndex={active === i ? 0 : -1}
              aria-controls="journey-panel"
              onClick={() => setActive(i)}
            >
              <span className="step-number">0{i + 1}</span>
              <Icon size={25} strokeWidth={1.3} />
              <strong>{s.title}</strong>
              <ArrowUpRight size={16} />
            </button>
          );
        })}
      </div>
      <div
        id="journey-panel"
        className="journey-panel"
        role="tabpanel"
        aria-labelledby={`journey-${active}`}
      >
        <span className="huge-number">0{active + 1}</span>
        <div>
          <h3>{steps[active].title}</h3>
          <p>{steps[active].detail}</p>
        </div>
      </div>
      <div className="guide-notes">
        <article>
          <Check size={20} />
          <h3>Clean means all five.</h3>
          <p>
            A clean verdict requires all five checks to be verified by trusted
            observers. A failed command can still have clean cleanup.
          </p>
        </article>
        <article>
          <TriangleAlert size={20} />
          <h3>Gaps stay visible.</h3>
          <p>
            A failed check means “Needs attention.” Missing or unsupported
            coverage means “Incomplete.” Both have a linked incident.
          </p>
        </article>
        <article>
          <Laptop size={20} />
          <h3>Local by design.</h3>
          <p>
            The application, signing trust, database, and archive run on this
            laptop. Fonts and interface assets are served locally, too.
          </p>
        </article>
      </div>
    </section>
  );
}

export default function App() {
  const [filters, setFilters] = useState(readLocation);
  const [search, setSearch] = useState(filters.q);
  const [showFilters, setShowFilters] = useState(false);
  const [component, setComponent] = useState<Component>("workspace");
  const [selectionVersion, setSelectionVersion] = useState(0);
  const [closed, setClosed] = useState(false);
  const [theme, setTheme] = useState(() => {
    try {
      return localStorage.getItem("after-theme") === "dark" ? "dark" : "light";
    } catch {
      return "light";
    }
  });
  const searchRef = useRef<HTMLInputElement>(null),
    filterButtonRef = useRef<HTMLButtonElement>(null),
    timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const client = useQueryClient();
  const navigate = useCallback((patch: Partial<Filters>, replace = false) => {
    const next = { ...readLocation(), ...patch };
    const params = new URLSearchParams();
    Object.entries(next).forEach(([k, v]) => {
      if (v && !(k === "view" && v === "receipts")) params.set(k, v);
    });
    const url = `/${params.size ? "?" + params : ""}`;
    if (url !== window.location.pathname + window.location.search)
      window.history[replace ? "replaceState" : "pushState"]({}, "", url);
    setFilters(next);
    setSearch(next.q);
    setClosed(false);
  }, []);
  useEffect(() => {
    const back = () => {
      clearTimeout(timer.current);
      const f = readLocation();
      setFilters(f);
      setSearch(f.q);
      setClosed(false);
    };
    window.addEventListener("popstate", back);
    return () => {
      window.removeEventListener("popstate", back);
      clearTimeout(timer.current);
    };
  }, []);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem("after-theme", theme);
    } catch {
      /* Theme persistence is optional. */
    }
  }, [theme]);
  useEffect(() => {
    const keyboard = (e: KeyboardEvent) => {
      if (
        e.key === "/" &&
        !(
          e.target instanceof HTMLElement &&
          e.target.closest("input,textarea,select,[contenteditable],dialog")
        )
      ) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", keyboard);
    return () => window.removeEventListener("keydown", keyboard);
  }, []);
  const health = useQuery({
    queryKey: ["ready"],
    queryFn: ({ signal }) => request<{ status: string }>("/readyz", signal),
  });
  const invalid = filterError(filters);
  const receipts = useInfiniteQuery({
    queryKey: [
      "receipts",
      filters.view,
      filters.q,
      filters.verdict,
      filters.repository,
      filters.incident_state,
      filters.from,
      filters.to,
    ],
    queryFn: async ({ pageParam, signal }) => {
      const p = await request<Page>(listPath(filters, pageParam), signal);
      if (!Array.isArray(p.items)) throw new Error("Invalid receipt response");
      return p;
    },
    initialPageParam: "",
    getNextPageParam: (p) => p.next_cursor || undefined,
    enabled: !invalid && filters.view !== "guide",
  });
  const rows = useMemo(
    () => receipts.data?.pages.flatMap((p) => p.items) ?? [],
    [receipts.data],
  );
  const selected = closed ? "" : filters.receipt || rows[0]?.id || "";
  const select = useCallback(
    (id: string, c?: Component) => {
      setComponent(c ?? "workspace");
      setSelectionVersion((v) => v + 1);
      navigate({ receipt: id });
      if (window.innerWidth < 1000)
        requestAnimationFrame(() =>
          document.querySelector(".detail-panel")?.scrollIntoView({
            behavior: window.matchMedia("(prefers-reduced-motion: reduce)")
              .matches
              ? "instant"
              : "smooth",
            block: "start",
          }),
        );
    },
    [navigate],
  );
  const hasFilters = !!(
    filters.q ||
    filters.verdict ||
    filters.repository ||
    filters.incident_state ||
    filters.from ||
    filters.to
  );
  const extras = [
    filters.repository,
    filters.incident_state,
    filters.from || filters.to,
  ].filter(Boolean).length;
  const clean = rows.filter((r) => r.verdict === "pass").length,
    attention = rows.filter((r) => r.incident?.state === "open").length;
  const clear = () => {
    clearTimeout(timer.current);
    navigate({ ...emptyFilters, view: filters.view });
  };
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ["recovery"] });
    void client.invalidateQueries({ queryKey: ["receipts"] });
    void client.invalidateQueries({ queryKey: ["receipt"] });
    void client.invalidateQueries({ queryKey: ["ready"] });
  };
  const closeFilters = () => {
    setShowFilters(false);
    requestAnimationFrame(() => filterButtonRef.current?.focus());
  };
  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        Skip to evidence
      </a>
      <aside className="rail">
        <a
          className="logo"
          href="/"
          aria-label="AFTER home"
          onClick={(e) => {
            e.preventDefault();
            navigate({ ...emptyFilters });
          }}
        >
          <Mark />
        </a>
        <span className="rail-rule" />
        <nav aria-label="Main navigation">
          <button
            className={filters.view === "receipts" ? "active" : ""}
            aria-current={filters.view === "receipts" ? "page" : undefined}
            title="Receipt ledger"
            aria-label="Receipt ledger"
            onClick={() => navigate({ view: "receipts", receipt: "" })}
          >
            <Layers size={22} />
          </button>
          <button
            className={filters.view === "incidents" ? "active" : ""}
            aria-current={filters.view === "incidents" ? "page" : undefined}
            title="Incidents"
            aria-label="Incidents"
            onClick={() => navigate({ ...emptyFilters, view: "incidents" })}
          >
            <TriangleAlert size={21} />
          </button>
          <button
            className={filters.view === "guide" ? "active" : ""}
            aria-current={filters.view === "guide" ? "page" : undefined}
            title="How it works"
            aria-label="How it works"
            onClick={() => navigate({ view: "guide", receipt: "" })}
          >
            <CircleHelp size={22} />
          </button>
        </nav>
        <div className="rail-bottom">
          <span className="rail-local">
            LOCAL
            <br />
            FIRST
          </span>
          <button
            aria-label={
              theme === "light"
                ? "Switch to dark theme"
                : "Switch to light theme"
            }
            title="Switch appearance"
            onClick={() => setTheme(theme === "light" ? "dark" : "light")}
          >
            {theme === "light" ? <Moon size={19} /> : <Sun size={19} />}
          </button>
          <div className="avatar" title="Single-user local workspace">
            A
          </div>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <a
            className="wordmark"
            href="/"
            onClick={(e) => {
              e.preventDefault();
              navigate({ ...emptyFilters });
            }}
          >
            AFTER<span aria-hidden="true">↗</span>
          </a>
          <div className="breadcrumb">
            <span>/</span> Evidence desk
          </div>
          <div className="topbar-right">
            <span
              className={`connection ${health.isError ? "disconnected" : ""}`}
              title={
                health.dataUpdatedAt
                  ? `API checked ${new Date(health.dataUpdatedAt).toLocaleTimeString()}`
                  : "Checking the local API"
              }
            >
              <span className="dot" />
              {health.isError
                ? "API unavailable"
                : health.isPending
                  ? "Connecting"
                  : "Local connection"}
            </span>
            <span className="topbar-divider" />
            <Laptop size={15} />
            <span className="mono">ON THIS LAPTOP</span>
          </div>
        </header>
        <main id="main-content">
          <section className="page-intro">
            <div>
              <div className="eyebrow">
                <span className="index-label">
                  {filters.view === "guide"
                    ? "03"
                    : filters.view === "incidents"
                      ? "02"
                      : "01"}
                </span>{" "}
                /{" "}
                {filters.view === "guide"
                  ? "HOW IT WORKS"
                  : filters.view === "incidents"
                    ? "INCIDENT REGISTER"
                    : "SIGNED CLEANUP EVIDENCE"}
              </div>
              <h1>
                {filters.view === "guide" ? (
                  "Trust has a paper trail."
                ) : filters.view === "incidents" ? (
                  "A closer look at what remains."
                ) : (
                  <>
                    Every exit
                    <br className="mobile-break" /> leaves <span>proof.</span>
                  </>
                )}
              </h1>
              <p>
                {filters.view === "guide"
                  ? "Understand the checks behind each receipt."
                  : filters.view === "incidents"
                    ? "Cleanup failures and incomplete observations, kept in the open."
                    : "Ephemeral runners. Permanent evidence. Inspect what happened after the run."}
              </p>
            </div>
            <button
              className="refresh-button"
              onClick={refresh}
              disabled={receipts.isFetching || health.isFetching}
              title="Refresh stored receipts and API availability"
            >
              <RefreshCw
                size={15}
                className={receipts.isFetching ? "spin" : ""}
              />
              <span>Refresh</span>
            </button>
          </section>
          {filters.view === "guide" ? (
            <Guide />
          ) : (
            <>
              <section
                className="overview-strip"
                aria-label="Loaded receipt summary"
              >
                <div className="summary-primary">
                  <span className="eyebrow">
                    {filters.view === "incidents"
                      ? "INCIDENT RECEIPTS"
                      : "RECEIPTS IN VIEW"}
                  </span>
                  <strong>
                    {receipts.data ? String(rows.length).padStart(2, "0") : "—"}
                    <span>{receipts.hasNextPage ? "+" : ""}</span>
                  </strong>
                  <span className="overview-caption">
                    {receipts.data
                      ? receipts.hasNextPage
                        ? "loaded · more available"
                        : "signed & recorded"
                      : "loading the ledger"}
                  </span>
                  <svg
                    className="barcode"
                    viewBox="0 0 92 35"
                    aria-hidden="true"
                  >
                    {Array.from({ length: 24 }, (_, i) => (
                      <rect
                        key={i}
                        x={i * 3.8}
                        y={0}
                        width={i % 3 === 0 ? 2.8 : 1}
                        height={i % 4 === 0 ? 25 : 35}
                      />
                    ))}
                  </svg>
                </div>
                <button
                  className="summary-item"
                  onClick={() =>
                    navigate({
                      verdict: "pass",
                      incident_state: "",
                      receipt: "",
                    })
                  }
                >
                  <span className="summary-icon clean">
                    <Check size={17} />
                  </span>
                  <span>
                    <span className="eyebrow">CLEAN EXITS</span>
                    <strong>
                      {receipts.data ? String(clean).padStart(2, "0") : "—"}
                    </strong>
                  </span>
                  <ArrowUpRight size={17} />
                </button>
                <button
                  className={`summary-item ${attention ? "has-attention" : ""}`}
                  onClick={() =>
                    navigate({
                      verdict: "",
                      incident_state: "open",
                      receipt: "",
                    })
                  }
                >
                  <span className="summary-icon attention">
                    <TriangleAlert size={17} />
                  </span>
                  <span>
                    <span className="eyebrow">NEEDS REVIEW</span>
                    <strong>
                      {receipts.data ? String(attention).padStart(2, "0") : "—"}
                    </strong>
                  </span>
                  <ArrowUpRight size={17} />
                </button>
                <div className="overview-note">
                  <Fingerprint size={27} strokeWidth={1.1} />
                  <span>
                    A record you can
                    <br />
                    <strong>independently verify.</strong>
                  </span>
                </div>
              </section>
              <RecoveryPanel onSelect={select} />
              <div className="section-heading">
                <h2>
                  {filters.view === "incidents"
                    ? "Incident register"
                    : "Receipt ledger"}
                  <span>{receipts.data ? rows.length : "…"}</span>
                </h2>
                <span className="last-sync mono">
                  {receipts.dataUpdatedAt
                    ? `LOADED ${new Date(receipts.dataUpdatedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false })}`
                    : "FETCHING LOCAL RECORDS"}{" "}
                  <i />
                </span>
              </div>
              <div className="filter-toolbar">
                <form
                  className="search-field"
                  onSubmit={(e) => {
                    e.preventDefault();
                    clearTimeout(timer.current);
                    navigate({ q: search, receipt: "" });
                  }}
                >
                  <Search size={17} />
                  <input
                    ref={searchRef}
                    aria-label="Search receipts"
                    placeholder="Search jobs, runs, workflows…"
                    maxLength={255}
                    value={search}
                    onChange={(e) => {
                      setSearch(e.target.value);
                      clearTimeout(timer.current);
                      const q = e.target.value;
                      timer.current = setTimeout(
                        () => navigate({ q, receipt: "" }, true),
                        350,
                      );
                    }}
                  />
                  {search ? (
                    <button
                      type="button"
                      aria-label="Clear search"
                      onClick={() => {
                        clearTimeout(timer.current);
                        navigate({ q: "", receipt: "" });
                        searchRef.current?.focus();
                      }}
                    >
                      <X size={15} />
                    </button>
                  ) : (
                    <kbd>/</kbd>
                  )}
                </form>
                <div
                  className="verdict-filters"
                  role="group"
                  aria-label="Cleanup verdict"
                >
                  {[
                    { id: "", label: "All" },
                    { id: "pass", label: "Clean" },
                    { id: "fail", label: "Attention" },
                    { id: "partial", label: "Incomplete" },
                  ].map((v) => (
                    <button
                      key={v.id}
                      aria-pressed={filters.verdict === v.id}
                      className={filters.verdict === v.id ? "selected" : ""}
                      onClick={() => navigate({ verdict: v.id, receipt: "" })}
                    >
                      {v.label}
                    </button>
                  ))}
                </div>
                <button
                  className={`more-filters ${extras ? "applied" : ""}`}
                  ref={filterButtonRef}
                  onClick={() => setShowFilters(true)}
                >
                  <SlidersHorizontal size={15} />
                  Filters
                  {extras > 0 ? (
                    <span>{extras}</span>
                  ) : (
                    <ChevronDown size={13} />
                  )}
                </button>
              </div>
              {hasFilters && (
                <div className="active-filters">
                  <span>FILTERED VIEW</span>
                  {filters.q && (
                    <button onClick={() => navigate({ q: "", receipt: "" })}>
                      “{filters.q}”<X size={11} />
                    </button>
                  )}
                  {filters.repository && (
                    <button
                      onClick={() => navigate({ repository: "", receipt: "" })}
                    >
                      Repository
                      <X size={11} />
                    </button>
                  )}
                  {filters.incident_state && (
                    <button
                      onClick={() =>
                        navigate({ incident_state: "", receipt: "" })
                      }
                    >
                      {filters.incident_state === "none"
                        ? "No incident"
                        : `${filters.incident_state} incidents`}
                      <X size={11} />
                    </button>
                  )}
                  {(filters.from || filters.to) && (
                    <button
                      onClick={() =>
                        navigate({ from: "", to: "", receipt: "" })
                      }
                    >
                      {filters.from || "Any start"} → {filters.to || "Any end"}
                      <X size={11} />
                    </button>
                  )}
                  <button className="clear-filters" onClick={clear}>
                    Clear all
                    <X size={11} />
                  </button>
                </div>
              )}
              {invalid && (
                <div role="alert" className="notice error">
                  {invalid}
                  <button onClick={clear}>Reset filters</button>
                </div>
              )}
              {receipts.isError && (
                <div role="alert" className="notice error">
                  <TriangleAlert size={17} />
                  <span>
                    {apiError(receipts.error)}
                    {rows.length > 0
                      ? " Previously loaded records are shown below."
                      : ""}
                  </span>
                  <button onClick={() => void receipts.refetch()}>
                    Try again
                  </button>
                </div>
              )}
              <div
                className={`evidence-layout ${!selected ? "no-selection" : ""}`}
              >
                <section className="ledger-panel" aria-label="Receipt list">
                  {!invalid && receipts.isPending ? (
                    <div
                      className="ledger-loading"
                      aria-busy="true"
                      aria-label="Loading receipts"
                    >
                      {Array.from({ length: 5 }, (_, i) => (
                        <div className="skeleton-row" key={i}>
                          <div className="skeleton" />
                          <div className="skeleton" />
                          <div className="skeleton" />
                        </div>
                      ))}
                    </div>
                  ) : !invalid && rows.length > 0 ? (
                    <Ledger rows={rows} selected={selected} onSelect={select} />
                  ) : (
                    <Empty
                      title={
                        receipts.isError
                          ? "Waiting for your local API"
                          : invalid
                            ? "Check your filters"
                            : hasFilters
                              ? "No matching receipts"
                              : filters.view === "incidents"
                                ? "No incidents to review"
                                : "Your evidence starts here"
                      }
                      action={
                        hasFilters ? (
                          <button className="secondary-button" onClick={clear}>
                            Clear filters
                            <ArrowRight size={14} />
                          </button>
                        ) : undefined
                      }
                    >
                      {receipts.isError
                        ? "Your records stay in the database. Reconnect to see them here."
                        : hasFilters
                          ? "Try another search or broaden the filters."
                          : filters.view === "incidents"
                            ? "No incident records were returned for this view."
                            : "Once a local run is finalized and ingested, its signed cleanup record will appear here."}
                    </Empty>
                  )}
                  {rows.length > 0 && (
                    <div className="ledger-bottom">
                      <span>
                        {rows.length}{" "}
                        {rows.length === 1 ? "receipt" : "receipts"} loaded{" "}
                        <i /> Newest cleanup first
                      </span>
                      {receipts.hasNextPage ? (
                        <button
                          disabled={receipts.isFetchingNextPage}
                          onClick={() => void receipts.fetchNextPage()}
                        >
                          {receipts.isFetchingNextPage ? (
                            <LoaderCircle size={13} className="spin" />
                          ) : (
                            <ArrowDown size={13} />
                          )}
                          Load more
                        </button>
                      ) : (
                        <span className="end-marker">
                          END OF THE RECORD <Check size={12} />
                        </span>
                      )}
                    </div>
                  )}
                  <div className="ledger-note">
                    <div className="note-stamp">
                      <Fingerprint size={24} strokeWidth={1.2} />
                    </div>
                    <div>
                      <strong>Nothing disappears without a record.</strong>
                      <p>
                        Each row links cleanup observations to a signed receipt.
                        Select one to trace its evidence.
                      </p>
                    </div>
                    <span className="mono">
                      AFTER
                      <br />
                      THE RUN ↗
                    </span>
                  </div>
                </section>
                {selected && (
                  <Detail
                    key={`${selected}-${selectionVersion}`}
                    id={selected}
                    initialComponent={component}
                    onClose={() => {
                      setClosed(true);
                      navigate({ receipt: "" }, true);
                      setClosed(true);
                    }}
                  />
                )}
              </div>
            </>
          )}
          <Footer />
        </main>
        <div className="workspace-end">
          <span className="mono">AFTER / CLEANUP RECEIPTS</span>
          <span>
            <Command size={11} /> Press <kbd>/</kbd> to search
          </span>
        </div>
      </div>
      {showFilters && (
        <FiltersDialog
          filters={filters}
          onClose={closeFilters}
          onApply={(f) => {
            navigate({ ...f, receipt: "" });
            closeFilters();
          }}
        />
      )}
    </div>
  );
}
