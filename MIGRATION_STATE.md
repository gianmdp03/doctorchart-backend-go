# DoctorChart migration state

## PostgreSQL baseline follow-up — 2026-10-12, stage 3

DoctorChart now uses the same PostgreSQL initialization procedure as Billing and Pointly, with its own dedicated database and clinical tables. [db/schema.sql](db/schema.sql) is the single embedded structural baseline: 20 functional tables plus `app_schema_state`, 21 tables total. The previous bootstrap SQL, duplicated reference SQL and temporary FIRST_VISIT preparation have been consolidated; no preproduction migration chain or backfill is needed for an empty installation.

Normal startup calls `bootstrap.Run` once. `db.Ensure` owns one READ COMMITTED transaction and advisory lock `(17209, 3)` for DDL, clinical catalogs and baseline metadata; HTTP and jobs start after commit. Seeds preserve administered catalog values on restart. Existing FIRST_VISIT, patient/record quota transactions, immutable vaccination catalogs, identity and clinical rules remain in place.

A genuinely empty database initializes. A valid initialized baseline is accepted after identity/version/SQL checksum and normalized live-catalog fingerprint checks. An old, partial, unknown or drifting schema fails explicitly without adoption, automatic repair or deletion. PostgreSQL configuration is now exclusively `DATABASE_URL` with PostgreSQL URI credentials; JDBC, `SPRING_DATASOURCE_*` and separate DB username/password settings are no longer inputs. Drivers remain `pgxpool`; no Java runtime is required.

The [database guide](db/README.md) documents the common mechanism, validation, future versioned migrations and the future development reset procedure. No development reset has been executed. Stage 3 verification and its final result are recorded separately from the October 9 evidence below; those older PASS counts do not certify this baseline.

## Historical Go integration handoff — 2026-10-09

**Shared converter follow-up (2026-10-09):** the local optimizer has been removed at the user's request. Compatible uploads now use `syntah-image-converter-go` with the backend-selected `doctorchart-study` profile and an independent credential. Dimensions, format and quality belong to that service; explicit backend configuration controls whether transformation is allowed. Live HTTP/PostgreSQL tests and local/S3 protocol tests passed, alongside all Go tests, vet, build and Linux race tests. See [configuration, policy and reproducible proof](integration/SHARED_IMAGE_CONVERTER.md). Earlier storage/browser evidence below describes previous implementations.

**Configuration follow-up (2026-10-09):** user requested `.env` exclusively. Go now reads optional `.env` with process variables taking priority; Java properties support and its local file were removed. Original local values were transferred privately and validated without connecting to their database. `.env.example` documents the variables. New Go checks are recorded separately in `integration/evidence/dotenv`; previous integration hashes represent the earlier verified snapshot, not this configuration follow-up. HTTP/clinical contracts were not changed.

**Current handoff (2026-10-09): READY FOR STAGING within the locally verified Angular + Go scope.** The new acceptance criteria allow harmless PDF differences and prioritize preserved medical originals/ownership. Go is the definitive backend; no local migration functionality remains pending. Current [report](integration/REPORT.md), [resumable state](integration/STATE.md), [evidence](integration/evidence/summary.json).

Current controls: Angular 573 tests, production build/typecheck/configuration/SSR PASS; seven Go Linux quality gates PASS, native Windows 80 PASS / 1 symlink SKIP; real HTTP initial snapshot 137/137, final Gateway regressions 24/24, Chrome 18/18 plus prior setup 3/3, PDF 20/20, Docker restore/nonroot/graceful shutdown PASS. PostgreSQL retains the original 20-table schema. Spring source remains unchanged.

Current changes: strict future entitlement; identity-safe previews and private/no-store PDFs; UTF-8 supported clinical text with explicit 422 for unsupported scripts; original study bytes preserved, new 40 MP / 25 MiB limits; paginated S3 cleanup; Argentine dates and protected login/deep-link navigation. Calculators retain the existing disabled Angular flag; their APIs and clinical results were tested. Gateway/Billing were read-only, with a final tested snapshot recorded by hashes.

Next: freeze Gateway revision and validate real providers in staging (Supabase, automatic renewal, Billing, AWS/R2, Gemini and printers), rebuilding public Angular configuration. No deployment or real-database mutation. Owned integration processes are stopped; synthetic databases retained. Historical matrices and evidence below are not rewritten or elevated to VERIFIED.

## Historical migration handoff

The following results and unresolved exact-equivalence statements describe the earlier Spring differential run, before the new acceptance criteria and integration fixes. The old JPEG optimizer/128 MP limit is superseded by preservation of originals and the 40 MP input limit above.

**Status:** FUNCTIONAL IMPLEMENTATION COMPLETE / NOT READY FOR EXACT DROP-IN
**Phase:** 4 VERIFY AND HANDOFF — local verification executed; no deployment
**Source commit:** 828691433ac1057d4e7f132685884b5c9cb58e39
**Source:** ../doctorchart-backend, read only; Java builds/tests from target-owned copies
**Source runtime:** Spring Boot 4.1.1 / Java 25
**Go:** 1.27.2; documented security patch deviation from Skills' 1.27.1
**Skills:** .agents/skills/spring-to-go/SKILL.md; .agents/skills/go-backend/SKILL.md
**Fixture:** doctorchart-go-fixture-acd41e09; disposable PostgreSQL 17.11; 20 tables; existing databases untouched
**Matrix:** migration/CONTRACT_MATRIX.csv — 75 HTTP + 7 lifecycle operations
**Implemented:** 75/75 HTTP; 7/7 lifecycle; 0 known missing operations; 0 fully VERIFIED contracts
**Final report:** migration/REPORT.md; machine summary: migration/evidence/summary.json

| Batch | Scope | Status |
| --- | --- | --- |
| B01 | Runtime, gateway, health, billing | COMPLETE — executed local evidence |
| B02 | Specialties, experience, workspace/wizard | COMPLETE — executed local evidence |
| B03 | Patients, profiles and cascade deletion | COMPLETE — executed local evidence |
| B04 | Encounters and prescriptions | COMPLETE — executed local evidence |
| B05 | Growth, vaccination, charts and calculators | COMPLETE — executed local evidence; 31 exact pinned-JVM renal regressions |
| B06 | Studies, storage, printing/PDF and maintenance | IMPLEMENTED WITH DEVIATIONS — PDF/JPEG/cache differences documented |

| Gate | Result | Evidence in migration/evidence/ |
| --- | --- | --- |
| Go format/build/vet/test/race/security | PASS, seven controls | go-quality.log |
| Go native tests | 58 PASS, 0 FAIL, 1 Windows symlink SKIP; Linux executed | go-tests.jsonl |
| HTTP/auth/20-table state/sequences | 365/365 PASS | compare.json |
| Rollback and bidirectional Spring ↔ Go writes | 41/41 PASS; Spring Hibernate validate | compare.json |
| Startup/configuration | 9/9 PASS | lifecycle.json |
| Documents/local storage/S3 protocol | 14/14 functional PASS; explicit deviations | documents.json |
| Existing Spring tests | 663 PASS, 0 failures/errors/skips | spring-tests.json |
| Schema preservation and full backup/restore | PASS on owned clones | compare.json; restore-runtime.json |
| Linux nonroot, clinical preview and in-flight graceful shutdown | PASS | restore-runtime.json |
| Local workload | 1024 requests/runtime, concurrency 8, 0 errors | performance.json |
| Original source preservation | 292 hashes unchanged; Git clean | source-preserved.json |
| Exact complete equivalence | FAIL — known differences | ../REPORT.md |
| Real database/AWS/R2/productive environment/deployment | NOT RUN — not authorized | ../REPORT.md |

**Unresolved equivalence:** native PDF bytes/metadata/minor layout and optimized JPEG bytes/fileSize/pixels differ. Spring's warm PDF cache leaks across owners (200); Go enforces ownership (403) and does not reproduce its stale 30s window. Go bounds optimizer originals to 128 MP; larger originals UNVERIFIED. Audit timestamps excluded only by the explicit tested clock policy. S3 proof is local protocol, not real AWS/R2. See report; no exact drop-in claim.

**Security:** 0 reachable and 0 imported-package vulnerabilities; module-only GO-2026-5932 affects unimported x/crypto/openpgp. No secret values in public evidence.

**Next concrete action:** no known inventory implementation remains. Resolve or explicitly accept documented equivalence exceptions before declaring READY; environment validation and any future deployment require separate authorization. No audit/design restart, source edits, real-database mutation or traffic change. Local fixture retained for review; owned cleanup is `python migration/fixture.py stop`.
