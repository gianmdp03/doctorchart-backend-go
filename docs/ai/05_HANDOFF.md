# Handoff — M2-SOL implementado
**29/10/2026 (Argentina). Revisión independiente y aceptación pendientes; M3 no autorizado.**

## Estado y autorización
Propietario autorizó M2-SOL, rama específica y cambios acotados necesarios Billing/Gateway. Angular queda reservado a Argon. No worktrees, merge/push/despliegue ni migraciones reales.
- M1 aprobado: 615286720f98768dfbc85bcbe35d315e41405e87, sin integrar.
- Commit documental independiente previo a M2: **6d7b0d60b946b9451eea294b98c0724530edb792**, 19 archivos.
- DoctorChart: codex/m2-clinics-backend, aplicación congelada en **1dd09bc2f5a94128150440c7c5524a44594f1358** (17 archivos). Base documental 6d7b0d6, descendiente del M1 aprobado.
- Billing: codex/m2-doctorchart-entitlements, aplicación congelada en **80c171c86fcae15a7d52868c5960da340bb9d179** (11 archivos), base bf65c042cc7480cbf4603447becfeb14002dbde1; árbol limpio.
- Gateway main 55197fa intacto. Frontend comenzó en main/1419697 y avanzó externamente durante esta tarea a codex/m2-clinics-frontend/ea4f4d7b217134cfb7e3f9ccd8902dcd4beae898, árbol limpio al cierre; Sol no escribió ni cambió ramas allí. Main backend d5a89e0, sin merge.
El propietario autorizó expresamente consolidar ambos commits. Se verificaron los 26 hashes de código/tests de la entrega probada, scope staged explícito y diff --check. RELEASE de escritura de aplicación: Argon puede auditar esos commits exactos; cualquier corrección posterior requiere coordinación y nuevo hash. El cierre posterior es exclusivamente documental. Sin otro escritor simultáneo.

## Contrato congelado
[M2-CLINICS-v1](contracts/M2_CLINICS_API.md), SHA256:
`70e096102182fb5d0880997b6ff428e058d756b987c3d8c2238c8743f780e2fd`.
DTO/endpoints sin modificaciones posteriores. Disponible a Argon por documentación/esta conversación; no se atribuye envío a una sesión externa.
[Derechos efectivos](contracts/M2_EFFECTIVE_ENTITLEMENTS.md): cobertura administrada existente + catálogo Billing; firma HMAC, propietario/revisión/vigencia y lease máximo 60s limitado por cobertura. Snapshot apto permite operar durante caída remota; no defaults ni renovación desde caché.
Callbacks/outbox existentes e invalidación compartida entre réplicas; no se promete revocación instantánea antes de entrega del callback. Evidencia, archivos, reproducción y hashes en [entrega auditable](reviews/M2_SOL_DELIVERY.md).

## Resultado
Listado/detalle/alta/edición/estados de consultorios, IANA Go, propiedad médica, versiones y cuotas transaccionales. No cambios clínicos ni pacientes por sede. Archivados conservan datos, sin cancelar turnos (todavía no implementados).
Migración **3** agrega solo epoch de invalidación entre réplicas; baseline, migración 0002, catálogo clínico y AGENTS intactos. Vacío/1→3/2→3 convergen con conservación/rollback. Próximo arranque real aplicaría upgrade; **no autorizado ni ejecutado**.
Billing agrega proveedor interno, max_clinics opcional y compatibilidad legacy; pagos/concesiones generan notificación durable. Gateway no necesitó cambios; Supabase auth intacta.

## Pruebas
DoctorChart suite Windows/PG18.6 PASS y race completa Linux/PG17.11 PASS (404 casos/subcasos). Billing race completa PASS (495), más focal race tras ajuste final de compatibilidad. Baseline/shared Billing race y startup Windows/PG18.6 ejecutados explícitamente PASS sin SKIP. Corregidos tres conteos heredados 17→19 sin modificar esquema Billing.
Interoperabilidad handlers reales Billing/DoctorChart PASS contra PostgreSQL/Supabase sintéticos locales. Build/vet/formato/diff check y govulncheck PASS (cero vulnerabilidades alcanzables; avisos de módulos no importados sin alterar dependencias).
Omisiones externas y comandos exactos en entrega. Startup DoctorChart, bootstrap y nuevas suites de cuota/snapshot sí ejecutados. Fixtures propios PG17/18 y Linux race, label codex.task=doctorchart-m2; retirados al cerrar. Sin datos reales.

## Límite y próximo paso
No se asignaron cantidades reales max_clinics. Propietario debe configurarlas por tier: sin clave/cobertura apta, mutaciones afectadas 503; no bloqueó implementar partes independientes.
Argon debe auditar M2-SOL sobre los dos commits congelados; aceptación del propietario pendiente. Frontend M2-ARGON entregado en ea4f4d7b217134cfb7e3f9ccd8902dcd4beae898 y auditado en solo lectura: [informe cruzado](reviews/M2_ARGON_CROSS_REVIEW.md), tres hallazgos P2 y una observación P3; requiere correcciones antes de integración. No se modificó Angular. Configuración real, correcciones, integración/publicación o migraciones reales requieren su autorización. **No comenzar M3.**
