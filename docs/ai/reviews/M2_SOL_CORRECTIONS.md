# M2-SOL — correcciones finales autorizadas
29/10/2026. Base ea1c89d; rama codex/m2-clinics-backend. Sol escribe solo DoctorChart backend, sin escrituras en Angular/Billing. Sin worktrees, merge, push, despliegue o migraciones reales. M3 no autorizado. Revisión cruzada y aceptación todavía pendientes.

**Commit de corrección congelado: 594402ed46536f145b6ebc29aa7badf8a87cc8bb**, nueve archivos (código, regresiones y db/README). Diff auditable: git diff ea1c89d 594402e --. Cierre de coordinación posterior exclusivamente documental; no cambia aplicación.

## Decisión P1: quitar timeZone únicamente de los requests de consultorios
La recomendación anterior de ampliar el listado Go se reemplaza tras rastrear el uso real:
- timeZone es contexto clínico existente: api.WithTimeZone lo valida; clinical.viewZone lo usa para límites de día/mes, fechas de controles, edad/cálculos y zona de emisión/reimpresión de documentos (internal/clinical/timezone.go, encounter.go:101–111, encounter_query.go:98–100, prescription*.go). Eliminarlo globalmente alteraría flujos clínicos.
- internal/clinics no consume api.TimeZone: lista sin cálculos de calendario y entrega instantes RFC3339 en UTC. La zona de la sede se obtiene del campo propio timezone, nunca del navegador.
- **Go conserva el contrato y el rechazo de timeZone en el listado.** Regresión TestClinicsListDoesNotNeedBrowserTimezone verifica GET con page/size →200 y contexto extra →400. El P1 sigue abierto hasta el cambio de Argon y prueba conjunta; no declararlo resuelto por estos tests.

### Instrucción concreta para Argon (sin editar Angular desde Sol)
1. En src/app/core/interceptors/time-zone.interceptor.ts, excluir la URL del API configurado cuyo path sea exactamente /clinics o comience por /clinics/. No excluir rutas clínicas ni destinos por mera coincidencia de substring.
2. Dejar de agregar timeZone en las seis operaciones de ClinicsService; enviar únicamente page/size/status cuando corresponda. Preservar Bearer, cuerpos, headers, transferCache=false y el comportamiento de otros interceptores.
3. Agregar pruebas con HttpClient e **interceptores reales**, no solo servicio sin interceptores: GET clinics con paginación/estado y capabilities/detalle/POST/PATCH no llevan timeZone; una ruta clínica que sí lo usa continúa enviándolo. Confirmar listado contra backend real antes de habilitar flag.
4. La validación de sede usa UTC o nombres regionales IANA exactos. Mantener rechazo de Etc/, Local, offsets, abreviaturas y posix/right. No admitir capitalización incorrecta solo porque Intl sea case-insensitive; manejar INVALID_TIMEZONE del servidor. No normalizar aliases o cambiar DTO/endpoint unilateralmente.
5. Actualizar identificación documental de la entrega a M2-CLINICS-v1, SHA256 70e096102182fb5d0880997b6ff428e058d756b987c3d8c2238c8743f780e2fd. Atender además hallazgos previos de [auditoría frontend](M2_ARGON_CROSS_REVIEW.md). Mocks solo pruebas/desarrollo explícito, sin fallback ni activación comercial implícita.

## P2 implementado: Go y esquema 4
- Go rechaza todo prefijo Etc/ en el campo timezone, incluida su familia de offsets/aliases. UTC explícito y regiones válidas siguen funcionando; no se modifica el contexto clínico global timeZone.
- Nueva 0004_clinic_timezone_policy.sql: lock contra escrituras → preflight de TODAS las filas, incluidas archivadas → actualización de función → recreación/validación del mismo CHECK. No crea tablas, índices, datos ni relaciones adicionales: siguen 26 tablas y 21 FK.
- Si existen filas Etc/, aborta con conteo y solicitud de conciliación expresa del propietario. No borra, convierte, archiva ni cambia ninguna fila; todo el upgrade revierte y permanece en su versión anterior. Incluye Etc/UTC y Etc/GMT porque son aliases no regionales: la forma admitida por la política es UTC explícito, sin convertir datos automáticamente.
- Runner reconoce versiones publicadas 1/2/3, valida catálogo/historial antes de DDL y registra versión 4. Baseline, SQL 0002/0003, algoritmo catalog.go, locks/contextos/aislamiento y AGENTS intactos.
- Catálogos independientes, ensamblados de cero desde SQL publicado + nueva migración: PG17 85cbcbb463c81876f40babf1d156444bb64793e41388c511bd5c661159b362d6; PG18 1eb0e7bb7b9e3f10aa39c0eaa0101727f304d834ecd5ce9fdbbf77764e73ae7f. No se adoptan hashes de bases existentes.
- Conservación de filas clínicas, sedes, epoch y secuencia; rollback ante SQL fallido, catálogo alterado o seed tardío, rechazo repetible de filas incompatibles y reintento exitoso. Instalación vacía y upgrade real 3→4 convergen; upgrade desde baseline con seeds administrados también probado.
- Rollback probado = transaccional ANTES de confirmar. No hay DOWN automático después de confirmar 4; imágenes anteriores rechazan esa versión. Recuperación futura requiere binario compatible o respaldo/restauración autorizada, conservando escrituras nuevas.

## Evidencia y límites
- Entorno propio: PostgreSQL17.11/18.6, containers doctorchart-m2-fix-pg17 (loopback55442) y doctorchart-m2-fix-pg18 (loopback55440), rol migration_fixture, tmpfs, label codex.task=doctorchart-m2-final-fix. Solo datos sintéticos, siete bases fijas y admin por motor. Retirados, incluidos helpers race, tras verificar ID/label/puerto.
- Go1.27.3, Windows/PG18: go test ./... -count=1 -json **PASS, 471 casos/subcasos**, startup y bootstrap con seeds reales ejecutados sin SKIP.
- Go1.27.3 Linux/PG17: go test -race ./... -count=1 -json **PASS, 471 casos/subcasos**, cero fallos; startup/bootstrap/nuevas regresiones sin SKIP. Fuentes y dependencia media local montadas readonly; proxy local solo al PostgreSQL propio del fixture.
- build / vet / gofmt / diff check: PASS. Pruebas no relajan guardas de DSN ni checksums.
- Fallos iniciales corregidos: test de inicializadores concurrentes aún esperaba dos migraciones; ahora exige tres. Primer helper Linux carecía del bind readonly del módulo media ya declarado por go.mod: fallo de entorno, no cambio de dependencia; repetido con ese bind, sin editar repositorio externo.
- Omisiones externas: TestActualBillingEffectiveRights (requiere proveedor fixture de Billing), TestLiveConverterLocalStorage, TestLiveConverterOrientationAndSpecialPreservation, TestLiveConverterS3AndApplicationIsolation, TestLiveStudyHTTPConverterAndPersistence y TestLimitsSyncDoctorChartFixture. La interoperabilidad Billing previa consta en entrega original; no se presenta como reejecutada aquí. No build/suite Angular, E2E de navegador, configuración comercial real ni producción.
- Artefactos locales: C:/Users/gcast/AppData/Local/Temp/doctorchart-m2-final-fix-1793309653734, con logs JSON de suites, script/helper race y overlay para catálogos independientes. Código fuente montado readonly en Linux. No son datos de usuarios.

## Alcance comercial y cierre
Solo BASIC/PRO/UNLIMITED; no nuevas cantidades/precios/seeds ni ampliación FREE_TRIAL/ENTERPRISE. Billing sigue congelado en 80c171c. Editor comercial max_clinics sigue pendiente en Bycode frontend, con API administrativa existente.
P2 listo para revisión independiente sobre el nuevo commit de corrección. Integración M2 requiere corrección frontend P1 y hallazgos propios de Argon, revalidación y aceptación del propietario. No habilitar flag ni comenzar M3.
Al verificar Git al cierre se observaron cambios locales externos del frontend sobre ea4f4d7 (interceptor/spec, formulario TS/HTML, mocks y modelos). Se preservaron; no se atribuye su ejecución a Sol ni se valida aquí esa versión todavía sin commit. Coordinar el SHA final de Argon para revisión conjunta.
