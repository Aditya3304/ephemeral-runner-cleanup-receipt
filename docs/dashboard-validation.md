# Milestone f validation — AFTER dashboard

Executed September 7, 2026 on the prepared Windows/WSL laptop. Milestones a–f are
complete; approval is required before g. Runtime is local and uses no paid service.

## Fresh end-to-end result

A new real passing command ran in a disposable Kubernetes runner. The coordinator
reported successful cleanup, runner absence and test-resource absence. The trusted
finalizer archived exact versions, produced a genuine private Sigstore signature,
and the network-disabled CLI verified the result. Durable delivery submitted it to
the independently verifying API. The dashboard loaded this same receipt and its
Verify button performed another successful signature and artifact check.

| Evidence | Value |
|---|---|
| Run ID | `90de78f6d0f6716364fac70128f43197` |
| Receipt ID | `6670207d-88f3-4d10-aa0a-717f31b6caee` |
| Cleanup verdict | `pass`, five verified trusted observations |
| Receipt SHA-256 | `e56def12d7ef0b048e0a0beb2555cc13c78a2bc6790dc7f9e1d9f3e30c964190` |
| Receipt version | `290353e0-0d51-48c5-9263-6fdcae5cafcb` |
| Signature bundle version | `a9fcc5bd-43f7-4976-8e82-5a9d7653e46c` |
| Source revision of the demo job | `875127535935ec96466f4e757232cad54e27b28d` |
| Independently installed finalizer revision | `b42fd6d4496336233d9d490531ca2a6265932505` |
| Fresh browser/API verification | Signature and all artifacts verified at `2026-09-07T01:45:13.733191216Z` |

The job source and trusted finalizer revisions deliberately differ. UI development
does not silently change the independently approved finalizer policy.

The production database now has **seven genuine receipts: six cleanup passes and
one failure, with one open incident**. The existing interrupted-run failure
remains a failure with three of five trusted checks passing. Browser interactions
do not alter receipts, resolve incidents, or fabricate observations.

## Browser checks

**14/14 passed in Chromium; final suite duration 44.9 seconds.** The suite uses the
built production UI at the loopback gateway, not Vite's development server.

1. Real API data, local-only requests, visual overview and genuine re-verification.
2. Incident selection, failed coverage and provenance remain honest.
3. Search, verdict filters, keyboard shortcut and browser history.
4. Repository/incident filters, inclusive local dates and accessible filter dialog.
5. Receipt deep links, exact versions/digests, metadata export and timeline.
6. Unavailable/rejected verification never displays fresh success.
7. API outage, explicit retry and genuinely empty UI.
8. Cursor pagination does not mislabel one page as the full ledger.
9. Dark theme persistence and interactive evidence-journey guide.
10. Desktop automated accessibility.
11. Mobile coverage interactions, reduced motion and automated accessibility.
12. 320-pixel viewport without page overflow, with filter controls reachable.
13. Arrow/Home/End tab behavior, malformed links and invalid calendar filters.
14. Newest genuine successful run visible and freshly verified.

Axe scans found **zero WCAG 2 A/AA and 2.1 AA rule violations** on the tested light
desktop, dark desktop, open filter dialog and mobile views. This is automated
coverage, not a claim of complete accessibility certification. Visual review
covered desktop light/dark, incident and narrow-screen layouts.

The first test intercepts and rejects any non-loopback browser URL and observed
**zero external requests and zero page JavaScript errors**. Failure, empty and
pagination states use browser-only interception; these are UI tests, not new
database/archive fault injections. Genuine positive verification uses the real API.

## Build and boundary checks

- TypeScript strict checking and Vite production build passed with cached assets.
- Production JavaScript: approximately 303 kB raw / 94 kB gzip-equivalent; CSS:
  approximately 45 kB raw / 10 kB gzip-equivalent. The gateway currently serves
  uncompressed assets; these gzip figures are Vite size estimates.
- All three font families are bundled locally.
- Gateway and API Go tests passed; gateway Go vet passed.
- Gateway tests cover allowed assets, missing files, traversal/private-file
  rejection, forbidden host/method, HEAD, API routing, CSP and immutable asset cache.
- API image was rebuilt and started locally without dependency downloads. Runtime
  API secrets and networks remain as validated in milestone e.
- Source formatting and the dashboard lockfile are maintained independently from
  the guard workspace.

Local evidence is retained in ignored `.build/dashboard-test-results.json`,
`.build/dashboard-browser.log`, `.build/dashboard-artifacts/`,
`.build/dashboard-fresh-demo.log` and the genuine finalized receipt directory.
A portable summary and screenshots are supplied in the outputs folder.

## Explicit deferrals

This milestone implements f and the static serving needed to view it locally.
No watchdog, scheduled leases, automatic missing-finalization recovery, analytics
database/charts, live polling, incident editing, multi-user account system or
external issue publication has been added. The coverage map is an interactive
view of each receipt's five observations, not aggregate analytics.

The complete fault matrix and a fresh start-to-finish demonstration with **all**
outbound internet blocked remain milestone h acceptance work. Browser outbound
blocking and network-disabled CLI verification were exercised here; these checks
alone do not establish that broader acceptance claim. Local backup, restart
rehearsal and packaging remain later work. Hosted GitHub, public Sigstore and AWS
are outside the approved offline scope.
