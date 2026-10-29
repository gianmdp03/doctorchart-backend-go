# M1 — Migración versionada y lugares de atención
**Estado:** READY_FOR_APPROVAL / implementación NO AUTORIZADA.
**Implementador principal:** Sol. **Auditor independiente:** Argon, solo lectura.
**Repo/base:** `doctorchart-backend-go`, `main`, `c2c30c1142b39519532eb986dcad811fbaa2619b`.
**Aprobación del propietario:** solo preparación documental; pendiente aprobación de M1.
**Decisiones:** DC-030 a DC-041, DC-051 a DC-053; DC-042/DC-043 son propuestas sometidas a aprobación con este ticket.

## Objetivo y alcance propuesto
Preparar la primera evolución incremental verificable desde baseline 1 y crear lugares de atención del médico para agenda futura.
- Conservar `db/schema.sql` y `production: true`; agregar migración versionada, checksum, estado previo esperado y catálogo final verificado.
- Ampliar bootstrap solo lo necesario para esta evolución real: vacío → baseline → versión nueva y baseline existente → versión nueva. Sin framework genérico ni cadenas preventivas.
- Propuesta de tabla `clinics`: `id BIGINT` identity; `workspace_id` obligatorio con FK a `doctor_workspaces(id)`; nombre, país, provincia, ciudad, dirección, zona IANA, estado, versión y timestamps.
- Propuesta de estados: activo, inactivo, archivado. No decidir en M1 cuáles consumen cupo comercial. Índice por workspace y restricciones de integridad; no cascadas de borrado de datos clínicos.
- Usar workspace como referencia al propietario, sin duplicar una segunda autoridad de pertenencia. Nunca enlazar pacientes a lugares.
- Sin API ni altas para usuarios en M1: la administración de lugares se habilita después del contrato de cuotas Billing (M2).

El nombre técnico, los campos, estados y estrategia concreta de versionado anteriores siguen siendo propuestas hasta aprobar M1. Los nombres/longitudes SQL definitivos se justifican en la implementación aprobada respetando convenciones reales.

## Política y compatibilidad
`AGENTS.md` del backend es obligatorio. La excepción histórica sobre nombres de estudios no autoriza reescribir baseline para este ticket.
Conservar pgx, READ COMMITTED, contexto 30s, lock_timeout 10s, statement_timeout 20s y advisory lock transaccional (17209, 3). Esquema, seeds necesarios y marcador quedan atómicos antes de HTTP/jobs.
No adoptar ni reparar estados desconocidos; validar catálogo anterior antes de migrar y catálogo esperado después, sin recalcular hashes para aceptar drift.
El binario actual rechaza versiones distintas de 1: documentar compatibilidad de despliegue/recuperación antes de cualquier aplicación real. No prometer rollback del binario antiguo sobre esquema nuevo.

## Archivos esperados
- `db/init.go`, `db/catalog.go` solo si hace falta y `db/init_test.go`.
- Nueva migración SQL versionada y metadatos/pruebas asociadas en ubicación a definir según patrón del ecosistema.
- `db/README.md`, este ticket, tablero y handoff.
- Pruebas clínicas existentes únicamente si requieren cobertura de preservación.
`db/schema.sql` y `AGENTS.md` se mantienen intactos. No modificar otros repositorios por conveniencia.

## Plan tras aprobación
1. Revalidar Git e identificar el contrato de transición y el patrón uniforme utilizado por los backends, sin modificar Gateway/Billing/Pointly.
2. Implementar la evolución mínima y la tabla propuesta, con validaciones previas y posteriores en la misma transacción.
3. Ejecutar pruebas en entorno autorizado, documentar resultados y recuperación/compatibilidad sin ejecutar cambios reales.
4. Entregar diff identificado, archivos, comandos/resultados y límites; Argon audita y Sol corrige; el propietario acepta.

## Entorno y pruebas propuestas
No se creó ni utilizó un entorno durante R0. Antes de ejecutar ensayos, obtener autorización del entorno PostgreSQL 17 descartable propio e identificar host/puerto/label; nunca inferir ownership desde una DSN existente.
- Base vacía y baseline con fixtures representativos convergen al mismo catálogo.
- Conservación exacta de pacientes, consultas, recetas, estudios, medios, IDs, relaciones y configuración administrada.
- Reinicios idempotentes y concurrencia; exclusión/cancelación/timeout; fallo SQL/seed revierte esquema, datos y marcador; reintento seguro.
- Identidad, checksum, versión desconocida, catálogo alterado o estado parcial fallan sin adopción/reparación.
- FK impide lugares huérfanos; cada lugar referencia un solo workspace propietario. No declarar verificado aislamiento de endpoints todavía inexistentes.
- Regresiones clínicas y auth pertinentes; acceso clínico/wizard sin lugares. La aceptación funcional del mismo paciente entre agendas se completa al implementar turnos, no se simula como funcionalidad de M1.

## Fuera de alcance
`patients.clinic_id`, `clinic_patients`, backfills, lugares inventados, duplicación o traslado clínico, filtros por sede, wizard obligatorio, frontend, rutas/portal, staff, agenda, reservas y contrato Billing completo.
Sin ejecución contra bases reales, wipes, commits, pushes o despliegues sin autorización específica. No crear worktrees.

## Criterios de aceptación y cierre
- [ ] Propietario aprobó M1 y propuestas de ingeniería incluidas.
- [ ] Tests anteriores con evidencia real, entorno identificado y omisiones documentadas.
- [ ] Baseline y AGENTS intactos; clínica sin nueva pertenencia por lugar.
- [ ] Diff congelado por SHA o contenido identificado; Argon revisó independientemente en solo lectura, con P0/P1/P2 y limitaciones.
- [ ] Sol resolvió hallazgos bloqueantes; propietario aceptó la fase.
- Commit/PR: inexistente; no se autoriza implícitamente su creación.
- Resultado de pruebas y review: pendientes; ningún código implementado.
