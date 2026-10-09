# DoctorChart Angular + Go integration

**Status: READY FOR STAGING — local DoctorChart artifacts and tested Gateway snapshot.** Closed 2026-10-09; no deployment. No pending local functionality implementation. Do not restart migration audit/design.

Scope: DoctorChart frontend and Go backend only. Spring is a historical reference. Gateway and Billing are read-only; Gateway is under active refactor. PostgreSQL and all identities/storage are disposable synthetic fixtures. Migration evidence remains historical; new evidence belongs to integration/evidence.

Baseline: frontend clean, 59 test files / 560 tests PASS. Backend migration inventory 75 HTTP + 7 lifecycle. Existing migration fixture doctorchart-go-fixture-acd41e09 retained. Go 1.27.2.

Confirmed defects corrected:
- Angular accepts legacy/invalid module claims; computed expiry remains cached while JWT stays unchanged.
- Go accepts an empty downstream benefit and applies BASIC quotas; current Gateway supplies X-User-Plan-Expires-At and suppresses invalid/expired plans.
- Automatic resampling of unclassified medical studies can alter diagnostic originals. Preserve accepted original bytes; reject originals above 40 MP / 25 MiB without resampling.
- PDF UI needs explicit 403 handling and must discard late responses after identity changes.
- S3 cleanup must traverse all listing pages and return 503 for unavailable reads.
- Clinical PDFs preserve supported UTF-8 text, reject unsupported scripts with 422, and use private/no-store.
- Argentine date provider, same-URL guards, protected deep links and navigation during identity initialization are corrected.

Accepted by the new user request: PDF/JPEG binary identity is not an acceptance gate; clinical content, legibility, originals, authorization and data integrity are. Preserve the Go PDF ownership fix. Calculators remain under the frontend's existing temporary feature flag unless explicitly reactivated; test preserved functionality and report route availability honestly.

Final controls: Angular 59 files / 573 tests, production build, typecheck, public configuration and SSR PASS; Go seven Linux quality controls PASS, native Windows 80 PASS / 1 symlink SKIP (executed on Linux); initial HTTP stack 137/137, final Gateway regressions 24/24, Chrome final 18/18 plus prior persisted setup 3/3, PDF 20/20 PASS. Docker restore preserves all 20 tables/schema; nonroot runtime and in-flight graceful shutdown PASS. Spring 292 hashes unchanged and clean Git.

Gateway changed externally during its refactor. The final tested copy has 72 Gateway/Billing hashes; those source repositories were not edited. Fix a revision before staging and repeat regressions if it changes. Fixtures retain their disposable data for review; owned integration processes are stopped at handoff.

Handoff: [REPORT.md](REPORT.md), [summary](evidence/summary.json), [reproduction](../README.md), [Angular configuration](../../doctorchart-frontend/docs/angular-go-staging.md). Unsupported PDF scripts/glyphs return 422 without issuing a document or modifying saved data. New studies above 40 MP / 25 MiB are rejected without resampling. These are explicit input limitations.

Next: staging with synthetic data and fixed Gateway revision; real Supabase/OAuth/JWKS and long-lived automatic renewal, Billing/payments, AWS/R2 recovery, Gemini and target printers/browsers. None is marked PASS. Rebuild public Angular configuration: the verified bundle points to local fixtures. No deployment, traffic changes or real-database operations authorized. Do not repeat historical Spring suites without a concrete regression reason.
