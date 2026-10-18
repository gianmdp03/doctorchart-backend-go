# DoctorChart backend en Go

El módulo de curvas pediátricas incorpora las 20 láminas del PDF con datos OMS, LMS y SVG en Angular. Su [documentación y verificación](docs/growth-charts.md) detalla catálogo, fuentes, endpoint, límites clínicos y pruebas reproducibles.

Backend definitivo de DoctorChart. El cierre de integración del **09/10/2026** acreditó 75/75 endpoints y 7/7 procesos implementados y READY FOR STAGING dentro de aquel alcance local. Angular, Gateway desde copia, Go y PostgreSQL se probaron juntos. Consultá ese [informe](integration/REPORT.md), el [estado de integración](integration/STATE.md) y la [evidencia](integration/evidence/summary.json). Esos resultados y la [migración Spring](migration/REPORT.md) con su [matriz](migration/CONTRACT_MATRIX.csv) son evidencia histórica; no acreditan automáticamente cambios posteriores ni igualdad binaria universal.

El servidor usa `net/http`, dependencias explícitas, PostgreSQL mediante `pgxpool`, SQL parametrizado, transacciones y versiones optimistas. Conserva las rutas y reglas clínicas aprobadas. La inicialización PostgreSQL usa el baseline Go definitivo descrito en [db/README.md](db/README.md); no requiere Java ni adopta bases de esquemas anteriores.

Las altas de pacientes serializan la cuota por propietario mediante el workspace, y las escrituras de consultas bloquean el paciente antes de comprobar cupo y primera visita. El único [db/schema.sql](db/schema.sql) incluye el índice UNIQUE parcial `clinical_encounters_one_first_visit`; la preparación transitoria FIRST_VISIT de la etapa 1 quedó consolidada en ese baseline. Un índice ausente, no único o con un predicado distinto provoca rechazo por drift, sin modificar índices ni registros clínicos.

## Ejecución local

Las comprobaciones públicas tienen objetivos separados: `GET /api/doctorchart/ping` devuelve 200 mientras el proceso atiende HTTP (liveness). `GET /api/doctorchart/health` es readiness: comprueba PostgreSQL con contexto máximo de un segundo y la disponibilidad del snapshot vigente de límites Billing. Devuelve 200/UP cuando ambos están disponibles y 503/DOWN en caso contrario, sin detalles de conexión ni secretos. Los probes no realizan llamadas HTTP a Billing; el refresco conserva su ciclo y caché acotada. Usar ping para comprobar el proceso y health para decidir si recibe tráfico. Una interrupción de PostgreSQL y su recuperación no requieren reiniciar el servicio.

Requiere Go 1.27.2 y una base PostgreSQL 17 dedicada, creada y accesible. Una base vacía recibe el esquema completo de [db/schema.sql](db/schema.sql), embebido en el binario: 20 tablas funcionales y `app_schema_state` (21 en total), identidades, claves, restricciones e índices. `bootstrap.Run` ejecuta DDL, catálogos y metadata en una sola transacción READ COMMITTED bajo advisory lock `(17209, 3)`, antes de HTTP y jobs. Un fallo revierte toda la inicialización. Los reinicios validan backend, versión, checksum SQL y fingerprint normalizado del catálogo vivo; los seeds idempotentes conservan configuración administrada y las validaciones clínicas existentes.

Una base parcial, incompatible, con drift o sin marcador válido se rechaza explícitamente. El backend no adopta, repara, vacía ni borra una base existente. Las fuentes DDL anteriores `internal/bootstrap/schema.sql` y `migration/schema-reference.sql`, junto con la preparación transitoria FIRST_VISIT, fueron sustituidas por el baseline único; no se requieren migraciones preproducción ni backfills. El rol necesita permisos CREATE para inicializar. Ver [política, pruebas y futuro inicio desde cero](db/README.md) antes de preparar otra base. El reset de desarrollo requiere autorización posterior y no forma parte del arranque. Nunca ejecutar pruebas contra bases existentes.

```powershell
$env:PORT = '8083'
$env:DATABASE_URL = 'postgresql://doctorchart:<clave URI codificada>@localhost:5432/doctorchart'
$env:GATEWAY_SHARED_SECRET = '<secreto del gateway de al menos 32 caracteres>'
$env:SERVICES_BILLING_SHARED_SECRET = '<secreto de billing de al menos 32 caracteres>'
$env:SERVICES_BILLING_URL = 'http://localhost:8081'
$env:STUDIES_STORAGE_DIR = 'C:\DoctorChartData\studies'
go run ./cmd/api
```

El directorio de estudios debe ser absoluto, privado y externo al proyecto; no se admiten enlaces simbólicos. En Linux se crean directorios con permisos 0700 y archivos con 0600. Para S3 configurá `STORAGE_TYPE=s3`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` y, si corresponde, `S3_REGION` (predeterminado `auto`).

Para desarrollo se carga automáticamente `.env` del directorio actual o, si no existe, de la raíz del módulo Go usando [godotenv](https://github.com/joho/godotenv). Copiar `.env.example` a `.env` y completar los valores locales; ejecutar `go run ./cmd/api` desde la raíz o `go run .` desde `cmd/api`. Precedencia: variables del proceso, `.env`, valores predeterminados. Un archivo local inválido se rechaza sin buscar otro. PostgreSQL requiere exclusivamente `DATABASE_URL`, con esquema `postgres` o `postgresql`, host, base, usuario y contraseña; los caracteres especiales de las credenciales se codifican como componentes URI. `SPRING_DATASOURCE_*`, JDBC y `DB_USERNAME`/`DB_PASSWORD` ya no configuran PostgreSQL. Los aliases de Gateway/Billing documentados abajo permanecen. No se carga `secrets.properties` ni se requieren escapes Java. `.env` queda excluido de Git y de la imagen Docker; el contenedor puede recibir variables mediante `--env-file .env`. Si no existe el archivo se usan las variables del proceso; si existe pero es inválido, se rechaza sin exponer su contenido.

Las peticiones privadas requieren encabezados de confianza del servidor Gateway: `X-Gateway-Secret`, `X-User-Id`, `X-User-Role`, `X-User-Plan` y `X-User-Plan-Expires-At` (RFC3339 futuro). Sin beneficio válido se responde 403, salvo la excepción administrativa autorizada. El navegador envía exclusivamente su bearer; el Gateway valida e inyecta identidad/beneficio. Salud mantiene su acceso público. Billing conserva carga, reintentos, fallback y recarga. El almacenamiento y mantenimiento respetan propiedad, transacciones y cola durable de eliminación.

El conversor local de imágenes fue retirado. Las cargas JPEG, PNG y WebP se envían desde DoctorChart a `syntah-image-converter-go` con el perfil fijo `doctorchart-study`, después de autorizar la operación y la propiedad del paciente. Dimensiones, calidad, formato, orientación y metadatos se procesan exclusivamente en ese servicio. DoctorChart valida la respuesta y guarda la versión procesada cuando es menor; nombre, MIME y tamaño reflejan los bytes guardados. PDF, GIF, BMP y TIFF mantienen su almacenamiento original; imágenes animadas, de alta profundidad o con ICC se conservan cuando el conversor informa explícitamente esa condición. No hay un codificador local de respaldo. Una falla de credencial, permiso, servicio o validación impide guardar y crear el registro. Los límites de entrada del backend son 10 MB (10.000.000 bytes) por archivo y 40 MP.

Para conectar el servicio, configurar `IMAGE_CONVERTER_URL`, `IMAGE_CONVERTER_API_KEY` y `STUDIES_ALLOW_IMAGE_TRANSFORM`. La clave debe coincidir con `APP_DOCTORCHART_API_KEY` del conversor y ser independiente de Gateway/Billing. `STUDIES_ALLOW_IMAGE_TRANSFORM=false` exige conservación byte por byte; `true` autoriza la transformación. Por decisión del propietario, el perfil principal del conversor usa WebP calidad 75, máximo 2048 px por eje, sin recorte ni ampliación; conserva resolución dentro de ese máximo y guarda el resultado sólo si pesa menos. El ahorro depende de la imagen y no garantiza un tamaño fijo ni acredita fidelidad clínica. El perfil del servicio debe coincidir con esa decisión. Si las dos variables de conexión se omiten, el backend puede arrancar, pero las cargas raster compatibles devuelven 503; una configuración parcial o inválida impide arrancar. Ver [configuración y prueba completa](integration/SHARED_IMAGE_CONVERTER.md).

Las descargas y archivos existentes no se transforman. Los PDF incluyen fuentes UTF-8 para los glifos soportados y `private, no-store`; caracteres o escrituras no representables generan 422 `PDF_UNSUPPORTED_CHARACTER`, sin emitir un documento ni modificar datos médicos. Los cambios inocuos de bytes/fuente/paginación respecto de Spring están documentados y aceptados; se conserva la corrección de acceso entre propietarios.

El Dockerfile genera un binario Linux sin CGO, con certificados y zona horaria, y lo ejecuta como usuario 65532. Se probaron el contenedor local y el apagado con una petición en curso; no se desplegó.

## Integración actual reproducible

El harness Windows requiere Go, Docker, Python con `cryptography`, Pillow, pypdf y pypdfium2; Node/Angular, Playwright y Chrome instalados. Usa los repositorios hermanos `doctorchart-frontend`, `api-ai-gateway-go` y `billing-service-go`. Gateway/Billing se leen y el Gateway se compila desde una copia propia; sus checkouts no se modifican. Requiere un contenedor PostgreSQL 17 descartable propio registrado en `.migration/fixture.json` con nombre `doctorchart-go-fixture-*`, puerto loopback y credenciales sintéticas. Preparar ese contenedor sin Spring ni restaurar un esquema antiguo; no usar el procedimiento histórico `migration/fixture.py prepare` para el baseline actual. `integration/run_local.py` crea una nueva base vacía y el arranque Go ejecuta su bootstrap normal. No proporcionar credenciales de una base real.

En una terminal, desde este repositorio:

```powershell
go build -o bin/doctorchart.exe ./cmd/api
python integration/run_local.py
```

Cada arranque crea una base nueva y limpia únicamente checkpoints del navegador sintético. Esperar `READY`; luego en otra terminal:

```powershell
python integration/verify_local.py
node integration/browser.mjs
python integration/verify_current.py
python integration/verify_pdf.py
python integration/frontend_gates.py
docker build -t doctorchart-go:staging-local .
python integration/verify_runtime.py
```

`verify_local.py` se ejecuta una sola vez contra la base recién creada, antes del navegador. El script Chrome usa el Playwright instalado indicado en su import; ajustar la ruta si cambia el runtime local. `verify_current.py` requiere la copia final: si aún no se generó, ejecutar primero `python integration/recheck_gateway.py`, que sustituye solo el contenedor Gateway de la fixture. Los controles Go completos se indican abajo. `integration/finalize.py` consolida evidencia existente, no ejecuta pruebas ni acredita por sí mismo un cambio nuevo. Una reproducción escribe nuevas evidencias: preservar las anteriores si se necesita comparar.

Detener solo los procesos propios con `New-Item -ItemType File .migration/integration/stop`; las bases sintéticas se retienen. El resumen histórico del 09/10/2026 acredita Angular 573 pruebas, siete controles Go, HTTP inicial 137, Gateway final 24, Chrome final 18 más tres casos de preparación, PDF 20 y restore del esquema anterior de 20 tablas. El baseline actual tiene 21 tablas; una ejecución nueva necesita evidencias nuevas. Consultar el informe para el alcance exacto de cada ejecución.

Para staging hay que fijar la revisión Gateway y reconstruir Angular con URLs públicas de ese entorno, según [la guía Angular](../doctorchart-frontend/docs/angular-go-staging.md). Supabase/OAuth/renovación automática, Billing/pagos, AWS/R2, Gemini e impresoras reales requieren validación externa. No se desplegó ni se modificaron bases reales.

## Registro histórico de la migración Spring — 09/10/2026

La comparación histórica necesitó Windows, Python 3, Docker, Java 25 y Spring en el commit registrado. `MIGRATION_JAVA` indicaba la ruta de `java.exe`, y la referencia se compiló únicamente en `.migration/spring` después de comprobar los hashes del original. Ese procedimiento dependía del esquema anterior; el backend actual rechaza esa base sin el marcador del nuevo baseline. Los scripts y evidencias conservados documentan aquel cierre, no una dependencia de arranque ni compatibilidad actual con Spring. No reescribir evidencia histórica al consolidar el cierre actual.

Los ejecutables de comparación y extracción SQL a partir de Java fueron retirados tras comprobar la equivalencia del catálogo final; los informes y matrices permanecen como evidencia histórica. Los módulos migration/compare.py, verify_documents.py y verify_restore_runtime.py conservan únicamente helpers reutilizados por la integración Go.

Para crear un nuevo fixture Go descartable, migration/fixture.py prepare registra contenedor PostgreSQL 17 vacío, ID exacto, label y puerto loopback. Rechaza descriptores anteriores y nunca reconstruye SQL a partir de Java. migration/fixture.py stop comprueba esa identidad antes de retirarlo. Ese comando no se ejecutó sobre las bases de desarrollo.

Control de calidad completo, con `govulncheck` instalado y una plataforma compatible con race:

```powershell
python .agents/skills/go-backend/scripts/check_go.py . --target go1.27.2 --require-vulncheck
```

El control completo y race se ejecutaron en Linux mediante Docker. Las [evidencias](migration/evidence/summary.json) registran 365 casos diferenciales, 41 verificaciones de rollback, nueve casos de arranque y 14 de documentos. Una ejecución aprobada acredita esos casos; las diferencias y límites del informe siguen vigentes. El finalizador Java histórico también fue retirado; las capturas conservadas no sustituyen la ejecución de los controles actuales.

## Configuración compartida entre los servicios

El secreto interno de Gateway se configura como `GATEWAY_SHARED_SECRET`, con
el mismo valor en Gateway, DoctorChart y Pointly. Se conserva el alias
`APP_SECURITY_GATEWAY_SECRET` y sus nombres anteriores para compatibilidad.
El navegador envía su token de usuario; esta clave pertenece a los servidores.

Los avisos internos de Billing usan `SERVICES_BILLING_SHARED_SECRET`, con el
mismo valor en Billing y sus receptores. Se conserva el alias
`SERVICES_POINTLY_SHARED_SECRET`. Billing valida por sí mismo los tokens de
usuario; su clave interna autentica los avisos de recarga de límites.

Ambas claves deben tener al menos 32 caracteres Unicode, no contener espacios
al inicio/final ni valores de ejemplo, y ser distintas en DoctorChart y Pointly.
No tienen valores predeterminados, tampoco en modo local. Para generar cada
clave en PowerShell, usar 32 bytes aleatorios y guardarla una sola vez:

```powershell
[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
```

Los cuatro proyectos buscan `.env` primero en el directorio actual y luego
en la raíz del módulo Go. Un archivo local inválido se rechaza sin buscar otro.
Las variables del proceso prevalecen sobre las del archivo, incluso mediante
aliases o cuando su valor está vacío. Dentro del entorno o del archivo, el nombre
común tiene prioridad. Billing conserva además sus argumentos y archivos
`.properties` de compatibilidad: argumentos > proceso > `.env` > propiedades.
Guardar las claves en `.env` o en el entorno del servidor; nunca en Git,
Angular, logs o capturas de prueba.

## Actualización de límites sin reinicio

Billing entrega la invalidación con SERVICES_BILLING_SHARED_SECRET. El callback devuelve 200 sólo tras cargar un catálogo válido; ante fallo HTTP, JSON vacío o valores inválidos devuelve 503 y conserva el último snapshot. Los contadores aceptan enteros JSON entre -1 y 2147483647; no se convierten strings ni se truncan fracciones.

Cada instancia refresca el catálogo cada minuto. La caché es utilizable durante un máximo de diez minutos desde una carga correcta; después, las operaciones dependientes de límites intentan recuperar antes de adquirir bloqueos PostgreSQL y devuelven 503 si no pueden hacerlo. Se mantienen las cuotas estáticas FREE_TRIAL y la exención administrativa vigente. La cancelación del servicio detiene el refresco.

## UTC y zonas horarias

Consultas creadas por el servidor, presentación en la zona del dispositivo y documentos con zona de emisión conservada. Ver [contrato, pruebas y cambio de baseline](docs/zonas-horarias.md). La versión incluye una columna nueva en el baseline de preproducción; no actualiza bases existentes automáticamente.
