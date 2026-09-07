# AFTER — local dashboard

AFTER is the cleanup evidence desk at http://localhost:8080/. It reads the verified
metadata API and can request a fresh signature and archived-artifact check.

## Start on the prepared laptop

Start Docker Desktop, then run from the repository in WSL Ubuntu:

```bash
bash dev dashboard-up
```

This starts/preserves PostgreSQL, the archive and API/gateway using cached images.
It does not create jobs or regenerate trust. The Windows launcher
`Run-cleanup-dashboard.cmd` in the supplied outputs folder starts the same services
and opens the browser.

After UI changes, use `bash dev api-build`, then `bash dev api-up`. The build uses
cached Go/npm dependencies with package network lookups disabled and Docker
`--network=none --pull=false`. `bash dev dashboard-build` builds only the static UI
and stages it for the next API/gateway image; it does not replace running containers.

Initial setup uses `bash dev dashboard-prepare` to download the exact dependencies
in the dashboard lockfile and Chromium test browser. It expects the Node 24 toolchain
prepared by milestone c. Downloads are setup activity, not a runtime requirement.
The application has no CDN, remote fonts, analytics or public signing dependency.
No paid service is used.

## Explore the evidence

- The left rail switches between receipt ledger, incidents and the evidence journey.
  The moon/sun button switches theme.
- Select a job or one of its five small check cells. The inspector opens the
  corresponding receipt and observation. On narrow screens it scrolls into view.
- Select nodes in the cleanup map to inspect workspace, credentials, resources,
  logs or runner disposal. Status and observer provenance stay distinct.
- Evidence expands exact object versions, digests and storage references. Copy
  controls copy those values. Export downloads **metadata JSON**, not the canonical
  signed receipt or signature bundle.
- Timeline shows the four recorded timestamps. Identity & signing trust exposes
  repository, source revision, signer, issuer, trust digest and receipt ID.
- Verify this receipt calls the actual verification endpoint. The API checks the
  genuine signature and all signed artifact references. Stored ingestion
  verification is explicitly distinguished from a fresh check.
- Search is debounced and uses the API's text search. Press `/` to focus it.
  Filter by cleanup verdict, repository, incident state, and completion dates.
  Date ranges include both selected local calendar days. Invalid URL dates are
  rejected before requesting records.
- Filters and receipt selection live in the URL; browser Back/Forward and reload
  preserve them. Tab reaches controls; arrow keys/Home/End move within inspector
  and evidence-journey tabs. Escape closes the filter dialog and restores focus.
- Refresh is explicit. Load more follows the server cursor. Summary figures count
  only loaded records in the current filtered view, with “more available” when
  another page exists. They are not lifetime/global analytics.

“Clean” maps to cleanup `pass`, “Needs attention” to `fail`, and “Incomplete” to
`partial`. A failed job command can still have clean cleanup. Failed or missing
coverage never becomes a pass because a signature verifies. Open incidents remain
visible; incident editing is outside this release.

## Implementation and boundaries

React 19, Vite 8, Tailwind 4, TanStack Query 5 and Table 9 are pinned in the local
workspace lockfile. Inter, Space Grotesk and IBM Plex Mono font assets are bundled.
The custom coverage graphic is SVG plus keyboard-accessible HTML buttons.

The production Go gateway serves the built UI and proxies fixed API routes.
HTML is not cached; hashed assets are immutable. Its content security policy
permits same-origin scripts, styles, fonts and API requests, denies framing and
does not permit arbitrary remote connections. Files outside the build asset
allowlist are not served. No SPA fallback masks API errors.

The browser receives no database, object-store, signing or API delivery credentials.
It talks only to the loopback gateway. The API retains its internal networks and
restricted database role. Receipt data is held in memory; only the theme preference
is persisted in browser local storage. Fetches use no-store, no credentials and
bounded timeouts. There is no service worker, automatic polling or remote telemetry.

Unavailable, rejected and busy verification are separate from successful checks.
API errors are visible; previously loaded records are labeled as stale if refresh
fails. Empty, loading, invalid-link and no-matching-results states are explicit.
Reduced-motion preferences disable the decorative motion.

For development only, `npm run dev --workspaces=false` inside dashboard starts Vite
on loopback port 5173 with a fixed local API proxy. Production uses the stricter
gateway and is the tested demo path.

## Repeat the live demonstration

On the prepared laptop, with Docker running:

```bash
bash dev ci-up
bash dev finalizer-up
bash dev dashboard-up
python3 scripts/demo-finalizer.py pass
bash dev deliver
```

The demo runs a real disposable Kubernetes job, observes its cleanup, archives and
signs a receipt, and verifies it offline with the CLI. Delivery submits the signed
output through API re-verification. Refresh the dashboard, select the latest
Successful command receipt and click Verify this receipt.

## Validation and remaining scope

`bash dev dashboard-check` runs Chromium/Playwright against the running local
gateway. The suite expects the genuine lifecycle examples installed in earlier
milestones, including the interrupted-run incident. Error, empty and pagination UI
cases use browser request interception; they never insert fabricated production
records. Screenshots and JSON results go under ignored `.build/`.

Read [dashboard-validation.md](dashboard-validation.md) for the executed checks.
Watchdog scheduling/leases and the complete fault-injection matrix are later
milestones. Local backup/rehearsal and a full internet-blocked end-to-end acceptance
run remain pending. Hosted GitHub CI, public Sigstore and AWS deployment remain
excluded by the approved local scope.
