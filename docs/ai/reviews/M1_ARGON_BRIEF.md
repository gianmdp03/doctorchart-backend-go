> **ENTREGA ORIGINAL HISTÓRICA (48f87b1), anterior a la auditoría.** Argon ya auditó y requirió correcciones. El startup no estaba habilitado en las suites originales; se ejecutó y corrigió después. Estado vigente y evidencia en [M1_CORRECTIONS.md](M1_CORRECTIONS.md). Las referencias a revisión pendiente de este documento describen solo la entrega original.

# M1 — entrega para auditoría independiente Gemini Argon
**29/10/2026 · Sol implementó; Argon aún no revisó.**
**Estado:** READY_FOR_ARGON_REVIEW; aceptación del propietario pendiente. M2 no autorizado.

## Rango exacto
Repo `doctorchart-backend-go`, rama `codex/m1-clinics-migrations`.
- Baseline de aplicación previo: `c2c30c1142b39519532eb986dcad811fbaa2619b`.
- Commit documental independiente (26 Markdown, ningún código): `d5a89e05eebf70de1dca72ed0b01a74467b6b1a4`.
- Implementación M1 a auditar: `48f87b1d929e8496aecf203284cc4ca4d0c95820`.
- Coordinación de cierre se registra después, en commit solo documental; no añade aplicación.

```text
git show --stat d5a89e0
git diff --stat d5a89e0 48f87b1
git diff d5a89e0 48f87b1 -- db internal/bootstrap/schema_test.go
git diff c2c30c1 48f87b1 -- AGENTS.md db/schema.sql db/catalog.go
```

El último comando debe estar vacío. No hacer cambios, commits o migraciones reales durante la auditoría.

## Resumen del diff funcional
Siete archivos; 653 inserciones / 34 eliminaciones:
1. `db/init.go`: conserva transacción/lock/timeouts; valida versión anterior; aplica 1 → 2 antes de seeds; valida catálogo final y confirma marcador; contexto independiente de rollback.
2. `db/migrations.go`: única migración embebida, catálogos fijados para PostgreSQL 17 y 18, rechazo de otros mayores, historial estricto de versión/checksum/catálogos.
3. `db/migrations/0002_clinics.sql`: tabla de lugares con FK exclusiva a workspace; ubicación, IANA, estados/version/timestamps; índice de propietario; tabla de historial. No DDL de pacientes ni cambios de relaciones clínicas.
4. `db/migrations_test.go`: baseline real con datos sintéticos, snapshots/tablas/objetos originales, convergencia, cancelación/concurrencia, rollback/reintento, drift/checksums/historial y restricciones.
5. `db/init_test.go`: conteo esperado 23 → 25 tablas.
6. `internal/bootstrap/schema_test.go`: conteos 23/20 → 25/21 tablas/FK.
7. `db/README.md`: contrato, reproducción y recuperación futura no ejecutada.

No se tocaron Angular, Gateway, Billing, Supabase, endpoints clínicos, AGENTS, baseline ni algoritmo de catálogo. No se implementaron agendas, turnos, secretarias, API comercial o M2.

## Evidencia ejecutada
Go 1.27.3, entornos PostgreSQL propios descartables autorizados:
- 17.11, loopback 55442, `doctorchart-m1-20261029`.
- 18.6, loopback 55440, `doctorchart-m1-pg18-20261029`.
- Label `codex.task=doctorchart-m1`, role `migration_fixture`, almacenamiento temporal; sin acceso a bases reales.
- Race Linux/GCC 14.2 en `golang:1.27.3`, fuentes readonly, red del fixture 17 y proxy loopback temporal. Guards de tests intactos.

PASS: formato, diff check, `go build ./...`, `go vet ./...`, suite completa Windows/PG18.6 y `go test -race ./... -count=1` Linux/PG17.11, sobre código final.
Se habilitaron las cinco DSN de ensayo: baseline admin, schema, integrity, growth y followups. Reproducción/propiedades en [db/README](../../../db/README.md).
Govulncheck exit 0: cero llamadas/paquetes importados afectados; aviso de módulo indirecto GO-2026-5932 en openpgp no importado. Dependencias intactas.
No ejecutados: converter HTTP externo, end-to-end Billing/webhooks y servicios Supabase/Gateway reales. No afirmar validación de producción.

## Foco de revisión
- Catálogos independientes 17/18 y SQL/checksums correctos; rechazo de drift antes de DDL, sin adoptar hashes del esquema inspeccionado.
- Orden y atomicidad de baseline, migración, historial, seeds y marcador; versión 2 válida no reescribe metadatos.
- Fallos parciales, rollback/cancelación, 16 inicializadores y reintentos; historial vacío/extra/desconocido rechazado.
- Ningún cambio en objetos clínicos o pertenencia del paciente; ninguna sede inventada y clínica utilizable sin lugares.
- FK al workspace, ausencia de cascadas clínicas, validación IANA, estados, límites de campos e índice razonables; sin confundir integridad SQL con autorización API o cuotas, todavía fuera de alcance.
- Impacto de arranque: este binario aplicará migración sobre versión 1 reconocida si en el futuro se inicia contra ella. No desplegar por inferencia.
- Compatibilidad: marcador 2 hace fallar el binario anterior. No hay DOWN destructivo; rollback transaccional se probó. Recuperación tras commit y actualización mayor de PostgreSQL requieren procedimientos separados; no fueron ejecutados.

## Entrega esperada de Argon
Hallazgos P0/P1/P2 con SHA, archivo/línea, evidencia y reproducción; distinguir lo ejecutado de lo no verificado. Usar [plantilla de revisión](../templates/REVIEW_TEMPLATE.md), en solo lectura sobre el diff real.
No declarar M1 aceptado por el propietario ni comenzar M2. Los históricos de sources no son instrucciones de implementación.
