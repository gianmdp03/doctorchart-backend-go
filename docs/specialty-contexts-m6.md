# DoctorChart — M6: integración, estabilización y cierre

Validación: 10/11/2026. Alcance exclusivo M6. Fuentes: misión del propietario,
AGENTS.md de ambos repositorios y los diez handoffs M1–M5 leídos antes de editar.
Las auditorías antiguas de consultorios no sustituyen las decisiones definitivas
de especialidades. No se reabrieron alternativas.

## A. Implementación funcionalmente cerrada

### 1. Resumen ejecutivo y 2. estado M1–M6

La arquitectura queda validada en laboratorio. Se corrigieron cuatro defectos
pequeños del frontend y las expectativas antiguas de flags, sin cambios de
esquema en M6. Pasaron Angular completo (851/851), Go completo y detector de
carreras sobre PostgreSQL 17 y 18, y el recorrido con Angular/Go/PostgreSQL reales.
Esto no acredita despliegue, Supabase/Billing reales ni operación productiva.

| Etapa | Resultado conservado y verificado |
| --- | --- |
| M1 / versión 11 | Workspace administrativo único y contexto clínico independiente |
| M2 / versión 12 | Pacientes y recursos clínicos asociados al contexto |
| M3 / versión 13 | Bloques por consultorio/contexto, identidad y duración |
| M4 / versión 14 | Reservas contextualizadas, snapshots y agenda global |
| M5 / versión 15 | Varias especialidades, selector, preferencias y publicación por contexto |
| M6 / versión 15 | Integración real, regresiones corregidas y gates finales |

### 3. Arquitectura definitiva

Una cuenta y un doctor_workspaces por médico; varios doctor_specialty_contexts,
únicos por workspace/especialidad. Cada contexto usa el motor de wizard existente
y conserva definición efectiva, borrador, versión y preferencias propios. Los
pacientes son identidades clínicas independientes, incluso con el mismo DNI.
Consultorios y agenda pertenecen al workspace administrativo; cada bloque ofrece
un único contexto y duración. Sólo están habilitadas Pediatría, Medicina General
y Dermatología.

Las escrituras capturan su contexto. La URL explícita identifica la pantalla;
el selector y la última selección local no son autorización. El servidor exige
specialtyContextId y verifica propietario, contexto y recurso. La administración
sigue siendo global aunque la selección clínica esté pendiente.

### 4. PostgreSQL y 5. migraciones verificadas

production=true se conserva. Baseline, catálogo, checksum, seed y SQL 0002–0015
mantienen sus hashes previos a M6. No hay 0016 ni cambios retrospectivos. Versión
final 15; se conserva el runner uniforme y la validación de identidad/catálogo.

Se ejecutaron en PostgreSQL 17/18 las suites db/bootstrap/cmd/api y funcionales:
vacío, reinicios, secuencias, upgrades históricos representativos/intermedios
hasta 15, conservación de pacientes/consultas/documentos/turnos/preferencias,
FK/constraints/triggers, inmutabilidad, checksum alterado, drift, identidad
ajena, rollback SQL/seed/catálogo, reintento e inicialización concurrente.
Ejemplos: TestMultipleContextUpgradeFromFourteenRollbackRetryPreservation,
TestPatientContextUpgradePreservesAndRejectsInconsistentData,
TestScheduleBlocksRelationalIntegrity y
TestBookingContextsUpgradePreservesUnknownHistory.

Se mantienen drivers, READ COMMITTED, contexto 30 s, lock_timeout 10 s,
statement_timeout 20 s, advisory lock transaccional namespace 17209/key 3 y
confirmación atómica de esquema/seeds/marcador antes de HTTP/jobs. Sólo se
usaron bases descartables propias. No se aplicaron migraciones a bases reales.

### 6. Contextos/wizard y 7. pacientes independientes

El navegador real inicia sesión, crea Pediatría, termina su wizard, incorpora
General y Dermatología desde Mis especialidades y termina cada wizard. Un
contexto pendiente rechaza atención con ONBOARDING_REQUIRED y no permite
publicarlo. Otro contexto listo mantiene atención y administración. Un borrador
de personalización conserva definición efectiva, readiness y otra especialidad.

Los formularios dinámicos reales crean tres Juan Pérez, DNI 12345678, IDs 1/2/3,
contextos 1/2/3. Cada uno guarda primera consulta y seguimiento, búsqueda propia,
resumen, receta/orden, impresión y PDF. Tres imágenes multipart pasan por el
conversor real y persisten como estudios independientes. Paciente/documento/
archivo desde otro contexto devuelve 404. No se vinculan pacientes por DNI ni
se crean desde reservas.

### 8. Consultorios y Mis horarios

El navegador real configura Centro, Norte y Sur y las tres especialidades:

| Día y sede | Contexto y bloque |
| --- | --- |
| Lunes, Centro | General 08–10 / 20 min; Dermatología 10–12 / 30 min |
| Lunes y miércoles, Norte | Pediatría 14–18 / 20 min |
| Martes, Sur | Dermatología 09–13 / 30 min |
| Martes, Norte | Dermatología 14–16 / 30 min |
| Viernes, Centro | Pediatría 08–12 / 20 min |

Se comprueban varios días, edición, eliminación de bloque de ensayo, identidad
de los demás, cierre con date-picker compartido, preview, adyacencia válida y
conflicto entre sedes/contextos rechazado sin cambiar la versión guardada.
Escritorio 1440 px y móvil 390 px sin overflow; se inspeccionaron capturas.
No se rediseñó la pantalla ni el motor temporal.

| Regresión | Evidencia |
| --- | --- |
| 1 especialidad × 1 consultorio | D: autoselección y primer horario real |
| 1 especialidad × 3 consultorios | B: TestScheduleBlocksDurationsIdentityAndThreeClinics, HTTP/SQL real |
| 3 especialidades × 1 consultorio | D: Centro contiene bloques de las tres |
| 3 especialidades × 3 consultorios | D: escenario completo anterior |

B también rechaza superposición dentro de sede, entre especialidades/sedes,
bloque ajeno, versiones concurrentes y reemplazos incompatibles con reservas.
Cubre excepciones especiales/cierres, zonas horarias, DST y concurrencia con
creación/reprogramación. No se modificó esa lógica.

### 9. Agenda global y 10. reservas públicas/administrativas

D crea turnos General/Dermatología, verifica duración/contexto/bloque/snapshots,
rechaza reprogramación a otra especialidad, reprograma dentro de la correcta y
rechaza ficha incompatible. La agenda muestra ambos, conserva el filtro propio
al cambiar el selector y abre ficha con el contexto persistido. Se verifican
vinculación, CHECKED_IN y COMPLETED. B amplía cancelación, estados, filtros por
sede/estado, versiones, eventos, replay y exclusión global concurrente.

Un visitante móvil abre enlace directo Dermatología, cambia oferta con una
respuesta real demorada, elige horario, completa datos administrativos, resuelve
el proof real y recibe constancia 201. SQL verifica contexto, procedencia y
30 min, sin paciente clínico. Igual body/key repite la constancia. Una sola
oferta se autoselecciona; despublicar cierra acceso nuevo y conserva el recibo.
B cubre sedes inactivas, ocupación, publicación concurrente, dos visitantes,
recibo concurrente y rollback de detalles. Esa cobertura B no se presenta como E.

### 11. Secretaría

D realiza activación por invitación, código de recuperación, cookie de sesión y
CSRF. Recibe sólo Centro, consulta turnos/disponibilidad, crea, reprograma y
cancela. Un consultorio no asignado devuelve 404 sin enumerarlo; las rutas
clínicas rechazan su sesión. El DTO no contiene patientId, provenance ni
dirección clínica privada. No aparece vinculación/buscador clínico. B cubre
permisos por acción, estados, varios propietarios, expiración/revocación,
rate limit y conflictos globales. No se amplían permisos ni se mezclan identidades.

### 12. Aislamiento clínico y 13. concurrencia

Se revisaron clinical/context.go, ownedPatient, ownedEncounter, directorio/
dashboard, resumen, estudios/archivos, impresión, crecimiento, vacunación y
cálculos. Las queries hijas por patient_id están precedidas por control del
paciente/contexto; las preferencias de cálculo resuelven workspace/especialidad.
Los fallbacks internos con ID 0 exigen exactamente un contexto y no están
expuestos por el binder HTTP, que exige ID explícito. Multipart vuelve a verificar
dueño/contexto antes de conversión y commit. No se halló otro defecto concreto.

TestSameDoctorMultipleContextClinicalHTTPIsolation y las suites de integridad,
medios e impresión niegan workspace/contexto/paciente/consulta/documento ajenos,
mutaciones cruzadas y recursos pediátricos desde otro contexto. Calculadoras
siguen deshabilitadas en la interfaz; se verifican sus límites/backend sin
habilitarlas artificialmente.

Full/race en ambos PostgreSQL ejercitan reservas simultáneas, mismo recibo,
agenda contra reserva/reprogramación, altas duplicadas de contexto, versiones
clínicas y locks de pacientes. D mantiene dos consultas en edición en pestañas
distintas: un POST real se demora sin sustituir su respuesta, cambiar contexto
queda bloqueado mientras guarda, cada escritura llega al origen y el otro
borrador sigue intacto. Cancelar, guardar antes de cambiar y descartar se prueban
también sobre el formulario real de paciente.

### 14. Resultados y 15–16. navegador/integración real

| Gate / categoría de prueba | Resultado |
| --- | --- |
| A: Angular completo | PASS: 103 archivos, 851 tests, 0 fallos |
| TypeScript app/spec | PASS ambos |
| Angular desarrollo y build opt-in M6 | PASS ambos |
| Configuración pública | PASS: 7 tests; fixtures, no credenciales de despliegue |
| Gofmt de cmd/db/internal/ops, build y vet | PASS Linux Go 1.27.3 |
| B: Go completo PostgreSQL 17/18 | PASS ambos, con opt-in SKIP detallados debajo |
| B: Go completo -race PostgreSQL 17/18 | PASS ambos, mismos límites opt-in |
| C: navegador API simulada | PASS: runner M5 reejecutado después de fixes M6 |
| D: navegador Go/PostgreSQL reales | PASS: 17 flujos; 87 llamadas auxiliares HTTP más las de Angular |
| E: Supabase/Billing reales | BLOCKED: no configurados/autorizados |
| Build productivo / smoke de su artefacto | BLOCKED: ver categoría C de cierre |

En D son reales Angular, DoctorChart Go, Gateway Go, PostgreSQL 18, conversor
compartido y SMTP local Mailpit. Sólo Auth/Billing usan el adaptador externo de
desarrollo existente. Login emite JWT RS256; el Gateway real valida JWKS/issuer/
audience. No hay bypass de producto, inyección de identidad ni endpoints
DoctorChart simulados. HTTPS valida CA propia; Chrome sólo confía en el SPKI
del certificado efímero. D no es E.

SQL final D: versión 15, 1 workspace, 3 contextos, 3 pacientes, 4 reservas,
3 estudios y 3 outbox. Después se observaron dos SENT y un recordatorio futuro
PENDING: SMTP local, sin acreditar correo externo. Capturas: horarios desktop/
móvil, público y secretaría móvil.

Seis opt-in siguen SKIP, no PASS: TestActualBillingEffectiveRights,
TestLimitsSyncDoctorChartFixture, TestLiveStudyHTTPConverterAndPersistence,
TestLiveConverterLocalStorage, TestLiveConverterOrientationAndSpecialPreservation,
TestLiveConverterS3AndApplicationIsolation. Los opt-in de conversor requieren
sus propias variables/DSN; D prueba carga/conversión normal, sin acreditar S3 ni
toda su matriz especializada. No se ejecutó govulncheck ni se afirma auditoría
nueva de dependencias.

### 17. Tests corregidos y 18. defectos solucionados

1. clinicsGuard bloqueaba administración al seleccionar contexto pendiente
   aunque otro estuviera listo. Ahora autoriza por existencia de contexto listo,
   preserva onboarding inicial si ninguno está listo y corrige ruta inexistente
   /onboarding/specialty por /onboarding/select-specialty. Errores de carga
   cancelan navegación. Dos regresiones fallaron antes/pasan después.
2. El detector de cambios no reconocía app-patient-form-page, sin ancestro form
   nativo. Ahora protege ese editor real.
3. Guardar consulta limpiaba la marca global de otro borrador de ficha. Se
   identifican editores independientes: un éxito sólo libera el suyo y conserva
   cambios posteriores al inicio del request. Tres regresiones cubren formulario
   real, borrador ajeno y escritura mientras se sigue editando.
4. Los tests calendar/schedules/public-appointments asumían flags desactivados
   aunque la configuración aprobada los activa. Se corrigen expectativas,
   fixtures y restauración afterEach, conservando casos desactivados explícitos.
   No se cambió ningún flag ni se eliminó/omitió test.

5. La inspección móvil detectó nombre y especialidad concatenados en tarjetas de
   secretaría. Un ajuste CSS los separa en líneas; el navegador verifica sus
   posiciones reales y ausencia de overflow. No cambia DTO ni permisos.

Antes: flags 3 FAIL, clínicas 2 FAIL, cambios sin guardar 2 FAIL. Después:
guards 14 PASS, navegación 8 PASS, suite completa 851 PASS. Se aclaró también
que fixtures ficticios de configuración no certifican build/Supabase reales.

En Node 26 se ejecuta la suite con NODE_OPTIONS=--no-experimental-webstorage,
heredado también por los workers. Sin ese entorno, el WebStorage nativo de Node
sombrea el almacenamiento de jsdom y 46 tests fallan antes de sus assertions
por localStorage undefined. Pasar sólo la opción al CLI no llega a esos workers.
Se conservaron ambos logs fallidos de diagnóstico; la ejecución final completa
con el entorno correcto pasa 851/851. No se cambian flags de DoctorChart ni se
omiten pruebas. Para PowerShell, definir esa variable sólo durante el comando
ng test --watch=false y restaurarla al terminar.

### Evidencia y reproducción segura

Resultados y hashes de logs: [specialty-contexts-m6-results.json](specialty-contexts-m6-results.json).
Logs locales: %TEMP%/doctorchart-m6-{go-gates17,go-gates18,race17,race18,angular,
ts-app,ts-spec,dev-build,lab-frontend-build,config,production,mocked,real,opt-in-skips}.log.
Capturas/JSON D: %TEMP%/doctorchart-specialty-m6-20261110/browser/.

Runner backend: ops/testing/specialty-m6-lab.mjs. Navegador frontend:
scripts/specialty-contexts-m6-real.cjs. Requieren Docker Linux, hermanos Gateway/
media, imagen bycode/image-email:local, dependencias frontend/Chrome y configuración
del adaptador local existente. No leen DSN reales ni modifican local.public.json.

Desde backend ejecutar node ops/testing/specialty-m6-lab.mjs con argumentos
prepare, build y up en ese orden. Luego go-gates 17, go-gates 18, go-gates 17 race
y go-gates 18 race. Los gates de un mismo major son secuenciales: comparten sus
siete bases de ensayo. Desde frontend ejecutar:

```
node node_modules/@angular/cli/bin/ng.js build --configuration m6-lab
node scripts/specialty-contexts-m6-real.cjs
```

Para repetir D, reset-e2e sólo recrea la base literal doctorchart_m6_e2e, tras
verificar identidad/label/puerto propios. Al terminar, down verifica ownership
y retira sólo contenedores/red/caches propios; conserva evidencia temporal.
Se elimina environment.m6.generated.ts verificando su marcador. Go/tests corren
en Linux; no se modificó firewall ni se inició un servidor Go nativo en Windows.

## B. Validación de integración pendiente

### 19. Pendientes de servicios externos

Faltan Supabase/Billing reales de un entorno autorizado. Gateway real en D no
convierte el adaptador Auth en Supabase. Quedan alta/confirmación/refresh/
recuperación reales, integración comercial entre repositorios y correo externo.
R2/S3 y opt-in especializados requieren sus fixtures propios. No se usaron
usuarios ni servicios productivos. No hay defectos funcionales M1–M6 conocidos
sin resolver en los recorridos ejecutados; siguen los límites de cobertura citados.

## C. Preparación para producción pendiente

### 20. Configuración, despliegue y recuperación

npm run build ejecutado falla con «supabase.publishableKey: falta la clave pública.»
No hay PUBLIC_SUPABASE_* en el proceso ni clave pública legítima de ensayo
accesible. El identificador del Auth local no reemplaza ese requisito. Se necesita
URL pública HTTPS y publishable/anon de un proyecto autorizado por site.config
o PUBLIC_SUPABASE_URL/PUBLIC_SUPABASE_PUBLISHABLE_KEY; nunca claves privadas.
No se inventó clave ni se relajó el validador.

El smoke productivo espera correctamente bundles con hash, gzip e immutable de
build-deploy/generate-hosting. main.js de desarrollo no es ese artefacto. No se
declara smoke PASS ni se relajan sus controles. Con configuración legítima:
compilar, ejecutar test:static sobre esa salida y verificar CORS/callbacks,
dominios y certificados autorizados. Ver frontend docs/conexion-entornos.md.

Antes del VPS leer docs/vps-operations.md, verificar marcador real, respaldar base
y medios y probar restauración aislada. Migraciones/despliegue reales requieren
autorización independiente. El binario actual lleva a 15; los antiguos rechazan
ese marcador. No hay DOWN automático: después del commit del upgrade se requiere
binario compatible o restauración autorizada conservando escrituras posteriores.
M6 no ejecutó despliegue, wipe, restauración ni comandos contra el VPS.

### 21. Git y preservación

Backend main HEAD 6869461; frontend main HEAD ff14f0c, HEAD y staged intactos.
Preflight guarda copias/hashes/diffs de los 108 archivos backend y 112 frontend
previamente modificados/untracked. Los 108 backend siguen idénticos; sólo
clinical-navigation.service.ts de los 112 frontend recibe el ajuste M6 descrito.
El resto de M6 modifica archivos antes limpios o agrega nuevos. Baseline, SQL,
catálogo, seed, flags y ambos AGENTS mantienen hashes. El diff total contiene
trabajo previo/M1–M5 y no debe atribuirse entero a M6.

M6 añade informe/resultados/laboratorio en backend; guards/regresiones,
protección de editores, configuración/runner opt-in y documentación en frontend.
Sin cambios Go funcionales, commits, pushes, worktrees ni resets. Estado final:
111 archivos backend y 124 frontend modificados/untracked en total, incluyendo
el trabajo anterior. Cleanup verificado: cero contenedores/redes/volúmenes con
el label exclusivo M6 y archivo de entorno generado retirado. Evidencia temporal
conservada.

### 22. Ampliación futura del catálogo

Una cuarta especialidad futura puede partir del catálogo y wizard existentes,
sin otro workspace ni reforma de identidad de pacientes/bloques/reservas. Requiere
decisión de producto, definición/experiencia publicada y pruebas de capacidades,
formularios, documentos y aislamiento; módulos clínicos avanzados pueden requerir
desarrollo propio. Ese trabajo no se implementa ni inicia en M6. Sólo las tres
actuales permanecen habilitadas.

M6 termina aquí. No iniciar otra etapa ni desplegar automáticamente.
