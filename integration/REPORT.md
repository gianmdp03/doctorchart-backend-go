# DoctorChart Angular + Go — cierre local y preparación de staging

**Actualización de configuración (2026-10-09):** por solicitud posterior se reemplazó el soporte Java properties por `.env` y variables del proceso, con prioridad del proceso. Se copiaron los valores locales sin publicarlos; no se inició contra esa base. Se agregó `.env.example` y se retiró el parser anterior. La nueva verificación Go está en [evidence/dotenv](evidence/dotenv/); el resumen y los hashes originales de este informe describen la integración previa, no una repetición de Chrome después de este cambio de configuración. El resto del informe conserva el alcance de esa ejecución.

**Estado: READY FOR STAGING para los artefactos locales de DoctorChart y la copia del Gateway identificada por hashes.** No significa que producción esté validada. Antes de usar otra revisión del Gateway hay que repetir las regresiones de su frontera de seguridad. No se desplegó ni se modificaron servicios, credenciales, bases o pacientes reales.

Go queda como backend definitivo. Se continuó sobre la migración existente, sin rehacer auditoría/diseño ni repetir los 365 casos Spring↔Go. La evidencia histórica permanece en `migration/evidence`; la evidencia actual está en [evidence/summary.json](evidence/summary.json) y sus hashes en [evidence/artifact-manifest.json](evidence/artifact-manifest.json).

## 1. Archivos del frontend modificados

En `doctorchart-frontend`:

- `src/app/app.ts`, `app.spec.ts`: recuperar el enlace solicitado tras inicializar sesión, sin cancelar una navegación en curso; conservar aislamiento de pantallas por cuenta.
- `src/app/app.routes.ts`: reevaluar guards al recargar la misma ruta después de autenticarse.
- `src/app/features/auth/login-page.component.ts`, `login-dialog.component.ts`, `login-dialog.component.spec.ts`: reevaluar la ruta actual tras login/registro con sesión.
- `src/app/core/services/auth.service.ts`, `auth.service.spec.ts`: beneficio exclusivamente de `app_metadata.app_modules.doctorchart`, campos válidos, vencimiento reactivo, roles normalizados y eventos de sesión sin sobrescrituras tardías.
- `src/app/core/interceptors/auth.interceptor.ts`, `auth.interceptor.spec.ts`: quitar autorización vieja y encabezados de confianza introducidos por el cliente; usar el bearer de la sesión actual.
- `src/app/core/guards/doctorchart-plan.guard.spec.ts`: demostrar que la misma URL vuelve a comprobar el beneficio.
- `src/app/core/services/clinical-document-print.service.ts`, `clinical-document-print.service.spec.ts`: permisos 401/403, error de representación 422, descarte de respuestas de otra cuenta, cierre/revocación de vistas previas y ventanas sin opener.
- `src/app/core/date/argentina-date-adapter.ts`, `src/app/app.config.ts`, `src/app/shared/date-picker/date-picker.component.ts`, `date-picker.component.spec.ts`: compartir el formato argentino y evitar que el proveedor nativo cambie la interpretación del cumpleaños.
- `README.md`, `docs/angular-go-staging.md`: configuración del backend definitivo y comprobaciones pendientes.

No se modificaron estilos, componentes de calculadoras, versiones Angular ni dependencias del frontend. El listado exacto y sus hashes se registra también en la evidencia final.

## 2. Archivos del backend Go modificados

- `internal/api/http.go`, `http_test.go`: rechazar beneficios ausentes, inválidos o vencidos en la frontera de confianza del Gateway; conservar la excepción legítima de SUPER_ADMIN.
- `internal/storage/store.go`, `validation.go`, `store_test.go`, `s3_test.go`: preservar originales, límites previos a la decodificación, distinguir indisponibilidad S3 de ausencia y recorrer todas las páginas de objetos para mantenimiento.
- `internal/clinical/pdf.go`, `printing.go`, `printing_test.go`: fuente UTF-8 embebida, medidas y cortes de texto compatibles, errores explícitos para caracteres no representables y PDF `private, no-store`.
- `internal/clinical/fonts/DejaVuSansCondensed.ttf`, `DejaVuSansCondensed-Bold.ttf`, `DejaVuSansCondensed-Oblique.ttf`, `LICENSE.txt`: archivos de fuente sin modificar del módulo fpdf ya utilizado. La licencia también está incluida en el binario. [Licencia oficial de DejaVu](https://dejavu-fonts.github.io/License.html).
- `go.mod`, `go.sum`: eliminar el codificador nativewebp que dejó de utilizarse al conservar originales; no se actualizaron dependencias por iniciativa propia.
- `README.md`, `MIGRATION_STATE.md`, `migration/REPORT.md`: distinguir resultados históricos de aceptación e integración actuales.
- `integration/`: harness de procesos locales, recorridos Chrome, regresiones, renders, estado, informes y evidencias. No agrega paquetes a la arquitectura del servidor.

## 3. Incompatibilidades y defectos corregidos

| Problema confirmado | Resultado actual |
| --- | --- |
| Claims heredados/plan de Pointly aceptados como beneficio DoctorChart | Solo el módulo DoctorChart válido concede acceso. |
| Beneficio vencido permanecía en un computed mientras el JWT seguía igual | El reloj reactivo invalida el beneficio; Go exige vencimiento futuro. |
| Go trataba un beneficio vacío como BASIC | HTTP 403 `DOCTORCHART_PLAN_REQUIRED`. |
| Login sobre la misma URL podía omitir guards | Se recarga y se reevalúan permisos. Un vencido redirige a planes. |
| Recuperación inicial sustituía un enlace a paciente o cancelaba una navegación activa | Se conserva el enlace y se respeta una navegación ya iniciada. |
| Datepicker nativo reemplazaba el adaptador argentino | `29/02/2020` y fechas ambiguas se interpretan correctamente; se guarda ISO. |
| Resampling podía alterar material diagnóstico sin clasificación | Todo estudio aceptado conserva exactamente sus bytes, dimensiones y EXIF. |
| Fallo S3 de lectura se presentaba como 404; listado de limpieza incompleto | 503 en indisponibilidad; mantenimiento paginado. |
| PDF convertía `μg`, `Ω` y cirílico en puntos | Se conserva el texto soportado con fuente embebida; no se emite PDF con sustitución silenciosa. |
| Endpoint PDF retiraba `no-store` | PDF privado sin almacenamiento HTTP; se verifica por Gateway y Chrome. |
| PDF tardío o ventana previa sobrevivía al cambio de propietario | Se descarta/cierra/revoca; 403 informa falta de permiso. |

No se cambiaron fórmulas clínicas ni el esquema PostgreSQL.

## 4. Flujos completos verificados y límites de la evidencia

Procesos reales: Angular, Gateway Go desde copia de código, DoctorChart Go y PostgreSQL 17.11 con las 20 tablas originales. Identidades/JWT/JWKS, Billing y S3 fueron servicios HTTP sintéticos. El Gateway verificó TLS de la fixture con su CA; Chrome usó una huella de certificado limitada a esa fixture y un perfil aislado.

En Chrome se ejecutaron login inválido/válido, selección de Pediatría, wizard y guardado, creación de paciente con fecha bisiesta, historia clínica con peso/talla/perímetro, curva renderizada, aplicación de vacuna, receta persistida, impresión autenticada, carga/descarga/visualización/eliminación de estudio, edición de paciente conservando historia, recarga de enlace protegido, renovación explícita mediante el SDK Supabase real, logout, cambio de cuenta y redirección de beneficio vencido. Descarga JPEG idéntica al original; EXIF 6 produce dimensiones visuales 1000×3000 desde el original 3000×1000.

Por HTTP real se probaron también aislamiento de workspace/pacientes/encuentros/calculadoras/archivos/PDF, encabezados falsificados, JWT inválido/vencido/issuer/audience incorrectos, permisos administrativos, concurrencia 200/409, validaciones, listas/filtros/paginación, cuotas BASIC/PRO, borrado en cascada, siete formatos de archivos, rangos 206, límites 25 MiB/40 MP, PDF cifrado y archivos inválidos, fallos S3, cola durable tras reinicio y recuperación del backend después de un 502 del Gateway.

Las ocho calculadoras se compararon contra los resultados clínicos históricos, excluyendo solamente reloj/fecha generada. Favoritos, ejecución, historial, autofill y propiedad también se comprobaron. **La UI de calculadoras sigue desactivada por el feature flag existente**; no se afirmó haberla usado en Chrome. La eliminación de pacientes se comprobó en la API y el contrato Angular existente; el frontend no exponía un botón para ese flujo y no se añadió una función visual nueva.

## 5. Controles ejecutados

| Control | Resultado final | Evidencia |
| --- | --- | --- |
| Angular, suite completa | 59 archivos, 573/573 pruebas PASS | [frontend-tests.log](evidence/frontend-tests.log) |
| Build producción, typecheck | PASS; aviso existente de bundle inicial ≈1,22 MB sobre presupuesto de aviso de 1 MB | [build](evidence/frontend-production-build.log), [typecheck](evidence/frontend-typecheck.log) |
| Configuración pública / SSR real | 3/3 y 3/3 PASS | [config](evidence/frontend-public-config.log), [SSR](evidence/frontend-production-ssr.log) |
| Go 1.27.2, formato/build/vet/tests/race/govulncheck | Siete controles PASS en Linux | [go-quality.log](evidence/go-quality.log) |
| Go nativo Windows | 80 PASS, 0 FAIL, 1 SKIP por symlink; symlink ejecutado en Linux | [go-tests.jsonl](evidence/go-tests.jsonl) |
| Integración HTTP inicial | 137/137 PASS; snapshot inicial, casos BASIC directos identificados explícitamente | [local-http.json](evidence/local-http.json) |
| Regresiones sobre snapshot final Gateway y código Go actual | 24/24 PASS; cuotas reales a través del Gateway incluidas | [current-gateway.json](evidence/current-gateway.json) |
| Chrome final / preparación previa persistida | 18/18 y 3/3 PASS | [browser.json](evidence/browser.json), [browser-setup.json](evidence/browser-setup.json) |
| PDF actual, contenido/paginación/Unicode/no-store/propiedad | 20/20 PASS y revisión visual | [pdf-current.json](evidence/pdf-current.json) |
| Gateway, tests desde copia final | PASS en sus tres paquetes | [gateway-current-tests.log](evidence/gateway-current-tests.log) |
| Docker actual, restore y apagado | PASS: 20 tablas, dump/esquema iguales, usuario 65532, petición en curso 200, salida 0, worker finalizado | [restore-runtime.json](evidence/restore-runtime.json) |
| Spring original | 292 hashes iguales y Git limpio | [source-preserved.json](evidence/source-preserved.json) |

Los fallos encontrados no se cuentan como éxitos iniciales: quedaron las reproducciones de [paginación S3](evidence/s3-pagination-before.log), [pérdida de caracteres PDF](evidence/unicode-before.json), [caché HTTP PDF](evidence/pdf-cache-before.json) y [suite tras retirar el proveedor nativo](evidence/frontend-tests-before-date-provider.log). Los primeros intentos del harness HTTP/Chrome también están conservados y explicados en [verification-notes.json](evidence/verification-notes.json). Se corrigieron sus expectativas/esperas antes de acreditar los resultados finales. Los avisos `window.open` de jsdom son limitaciones del entorno unitario; Chrome ejecutó ventanas reales.

Govulncheck: cero vulnerabilidades alcanzables y cero en paquetes importados; sigue apareciendo una advertencia solo a nivel de módulo `GO-2026-5932` en `x/crypto/openpgp`, no importado. No se afirma cero advertencias de dependencias.

## 6. Diferencias respecto de Spring aceptadas deliberadamente

- **PDF:** cambian bytes, metadatos, fuente, anchuras y cortes de página. Se conserva contenido clínico en los casos probados, A4, orden, dosis, profesional, fecha y legibilidad. Se revisaron [referencia y PDF desde Angular](evidence/pdf-reference-current.png), [receta con Unicode](evidence/clinical-unicode-contact.png) y [100 órdenes completas](evidence/clinical-orders-current-contact.png). La comparación visual lado a lado contiene pacientes sintéticos distintos; no pretende demostrar igualdad semántica por sí sola.
- **Caracteres:** latín, griego, cirílico y símbolos presentes en la fuente se conservan. Escrituras que exigen shaping no soportado, glifos ausentes y caracteres fuera del plano básico generan HTTP 422 `PDF_UNSUPPORTED_CHARACTER`; el documento no se emite y el dato guardado permanece intacto. Es una limitación explícita, no soporte universal Unicode.
- **Estudios:** no hay clasificación confiable entre imagen administrativa y material diagnóstico; se preservan todos los originales aceptados. No hay diferencias introducidas de píxeles ni recodificación JPEG. Archivos nuevos de más de 40 MP o 25 MiB se rechazan, sin reducirlos silenciosamente. Los originales existentes se siguen descargando; no se transformó almacenamiento ni datos previos. La prueba sintética no certifica aptitud diagnóstica.
- **Seguridad:** se conserva el 403 entre propietarios y se prohíbe almacenamiento HTTP del PDF. No se reproduce la filtración ni el intervalo de contenido obsoleto de la caché Spring. Se exige beneficio futuro del Gateway, conforme a su contrato actual.

## 7. Filtración PDF

**Sigue corregida.** El propietario recibe 200; el otro profesional recibe 403 después de acceder el propietario y después de cambios clínicos. La autorización precede a la generación y no hay caché caliente que evite ese control. Chrome y las regresiones actuales comprueban además `private, no-store`; Angular descarta respuestas tardías y revoca sus previews cuando cambia la cuenta. No se incluyeron secretos internos en el navegador.

## 8. Gateway y Billing

No se editaron esos repositorios. El Gateway cambió durante su refactor por actividad externa: 15 archivos de la primera copia cambiaron. Se creó una nueva copia, se compiló, se ejecutaron sus tests y se repitieron las regresiones relevantes y Chrome. La copia final Gateway/Billing tiene 72 hashes en [reference-manifest-current.json](evidence/reference-manifest-current.json); la comparación final se registra en [references-current.json](evidence/references-current.json).

No se reprodujo un defecto bloqueante del Gateway en esos flujos. Su cliente de herramientas AI también incluye la identidad y expiración en el contrato inspeccionado; la llamada real a Gemini no se ejecutó. El catálogo Billing simulado conserva `/api/billing/limits/DOCTORCHART`; se comprobó una cuota BASIC agotada y el acceso PRO mediante JWT firmados de fixture. El servicio Billing real y su catálogo visual de planes no se ejecutaron. La URL terminal de planes de la fixture devuelve 404 intencionalmente: prueba la redirección, no la interfaz de pagos.

## 9. Verificaciones externas pendientes

En staging, con datos sintéticos y autorización del entorno: fijar una revisión Gateway; configurar URLs/issuer/audience/CORS y secretos exclusivamente en servidor; probar Supabase real, OAuth y recuperación/rotación JWKS; sesiones prolongadas y renovación automática; Billing real, cambios de beneficio/plan y pagos; AWS/R2 real con permisos, ranges, fallos y cola durable; impresión en impresoras/navegadores objetivo; Gemini real y herramientas con identidad; características y tamaños de archivos representativos bajo la memoria disponible.

Nada de eso figura como PASS local. No se hicieron DNS, Cloudflare, despliegues, ni operaciones sobre bases reales. La configuración pública generada para el build de prueba apunta a fixtures locales: debe reconstruirse con los valores públicos de staging antes de publicarse.

## 10. Entrega y continuación

**READY FOR STAGING dentro del alcance indicado.** No quedan fallos de los controles locales finales ni operaciones pendientes de la migración funcional. La aceptación nueva permite diferencias inocuas de PDF y prioriza originales; el `NOT READY FOR EXACT DROP-IN` histórico no se utiliza como requisito de igualdad binaria actual.

La guía de reproducción está en [README.md](../README.md) y en [la guía Angular](../../doctorchart-frontend/docs/angular-go-staging.md). Las fixtures y bases descartables quedan para revisión; los procesos propios están detenidos, sin puertos de integración abiertos, según [cleanup.json](evidence/cleanup.json). La [imagen local](evidence/docker-image.json) queda identificada para revisión. La evidencia original Spring no se reescribió ni se elevaron contratos a `VERIFIED` sin pruebas.
# Cambio posterior de almacenamiento, 09/10/2026 (histórico)

Por solicitud del usuario para ahorrar almacenamiento en Cloudflare R2, las cargas nuevas se optimizan antes de guardarse: máximo 2048 px y JPEG calidad 85 para imágenes opacas, PNG para transparencia; solo se reemplazan los bytes si el resultado es menor. Se aplica orientación EXIF de JPEG/WebP/TIFF. PDF, GIF, imágenes compactas y archivos existentes se conservan. Las evidencias anteriores de conservación byte a byte describen la ejecución anterior, no este cambio; no se volvió a ejecutar el harness de navegador ni se probó una cuenta R2 real para este cambio.

## Sustitución por el conversor compartido, 09/10/2026

La implementación local anterior se retiró por solicitud del usuario. DoctorChart ahora envía JPEG/PNG/WebP al conversor compartido con el perfil `doctorchart-study` seleccionado por el backend y una credencial independiente. Las reglas de procesamiento pertenecen al conversor; los archivos especiales tienen una política explícita de conservación. La autorización precede a la llamada y el registro se inserta después del guardado, con limpieza ante rollback. Pasaron las pruebas Go, build, vet, race en Linux y la prueba con API del conversor y PostgreSQL reales; almacenamiento S3/R2 mediante fixture de protocolo. Configuración, resultados y reproducción: [SHARED_IMAGE_CONVERTER.md](SHARED_IMAGE_CONVERTER.md). No se reejecutó el navegador ni se utilizó un bucket real.
