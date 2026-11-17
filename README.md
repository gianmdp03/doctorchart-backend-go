# DoctorChart backend en Go

El módulo de curvas pediátricas incorpora las 20 láminas del PDF con datos OMS, LMS y SVG en Angular. Su [documentación y verificación](docs/growth-charts.md) detalla catálogo, fuentes, endpoint, límites clínicos y pruebas reproducibles.

Backend definitivo de DoctorChart. El cierre de integración del **09/10/2026** acreditó 75/75 endpoints y 7/7 procesos implementados y READY FOR STAGING dentro de aquel alcance local. Angular, Gateway desde copia, Go y PostgreSQL se probaron juntos. Consultá ese [informe](integration/REPORT.md), el [estado de integración](integration/STATE.md) y la [evidencia](integration/evidence/summary.json). Esos resultados y la [migración Spring](migration/REPORT.md) con su [matriz](migration/CONTRACT_MATRIX.csv) son evidencia histórica; no acreditan automáticamente cambios posteriores ni igualdad binaria universal.

El servidor usa `net/http`, dependencias explícitas, PostgreSQL mediante `pgxpool`, SQL parametrizado, transacciones y versiones optimistas. Conserva las rutas y reglas clínicas aprobadas. La inicialización PostgreSQL usa el baseline Go definitivo descrito en [db/README.md](db/README.md); no requiere Java ni adopta bases de esquemas anteriores.

Las altas de pacientes serializan la cuota por propietario mediante el workspace, y las escrituras de consultas bloquean el paciente antes de comprobar cupo y primera visita. El único [db/schema.sql](db/schema.sql) incluye el índice UNIQUE parcial `clinical_encounters_one_first_visit`; la preparación transitoria FIRST_VISIT de la etapa 1 quedó consolidada en ese baseline. Un índice ausente, no único o con un predicado distinto provoca rechazo por drift, sin modificar índices ni registros clínicos.

## Ejecución local

Para ejecutar DoctorChart desde los repositorios principales, consultá la [guía del entorno local](ops/local/README.md). Incluye PostgreSQL persistente, acceso de prueba, conversor, correo capturado y lanzadores para Windows.

Las comprobaciones públicas tienen objetivos separados: `GET /api/doctorchart/ping` devuelve 200 mientras el proceso atiende HTTP (liveness). `GET /api/doctorchart/health` es readiness: comprueba PostgreSQL con contexto máximo de un segundo y la disponibilidad del snapshot vigente de límites Billing. Devuelve 200/UP cuando ambos están disponibles y 503/DOWN en caso contrario, sin detalles de conexión ni secretos. Los probes no realizan llamadas HTTP a Billing; el refresco conserva su ciclo y caché acotada. Usar ping para comprobar el proceso y health para decidir si recibe tráfico. Una interrupción de PostgreSQL y su recuperación no requieren reiniciar el servicio.

Requiere Go 1.27.4 y una base PostgreSQL 17 o 18 dedicada. El comando separado `init-db` crea el esquema completo de [db/schema.sql](db/schema.sql), embebido en el binario: versión 20, 55 tablas y 70 claves foráneas. Inicializa DDL, catálogos y metadata en una sola transacción READ COMMITTED bajo advisory lock `(17209, 3)`. Un fallo revierte toda la inicialización. La API usa logins restringidos y valida identidad, versión, checksum, fingerprint y permisos antes de iniciar HTTP o jobs. Las políticas RLS aíslan los registros clínicos por cuenta y contexto de especialidad, incluso si una consulta omite el filtro SQL.

Una base parcial, incompatible, con drift o sin marcador válido se rechaza explícitamente. El backend no adopta, repara, vacía ni borra una base existente. No se requieren migraciones preproducción ni backfills. El dueño del esquema inicializa mediante `init-db`; el administrador provisiona antes `doctorchart_app` y `doctorchart_media` con claves distintas y sin privilegios administrativos ni membresías. Ver [política, roles y pruebas](db/README.md). El reset de desarrollo requiere autorización posterior y no forma parte del arranque. Nunca ejecutar pruebas contra bases existentes.

```powershell
$env:PORT = '8083'
$env:DATABASE_URL = 'postgresql://doctorchart_owner:<clave URI codificada>@localhost:5432/doctorchart'
go run ./cmd/api init-db

$env:DATABASE_URL = 'postgresql://doctorchart_app:<clave URI codificada>@localhost:5432/doctorchart'
$env:MEDIA_DATABASE_URL = 'postgresql://doctorchart_media:<otra clave URI codificada>@localhost:5432/doctorchart'
$env:GATEWAY_SHARED_SECRET = '<secreto del gateway de al menos 32 caracteres>'
$env:SERVICES_BILLING_SHARED_SECRET = '<secreto de billing de al menos 32 caracteres>'
$env:SERVICES_BILLING_URL = 'http://localhost:8081'
$env:STUDIES_STORAGE_DIR = 'C:\DoctorChartData\studies'
go run ./cmd/api
```

El directorio de estudios debe ser absoluto, privado y externo al proyecto; no se admiten enlaces simbólicos. En Linux se crean directorios con permisos 0700 y archivos con 0600. Para S3 configurá `STORAGE_TYPE=s3`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` y, si corresponde, `S3_REGION` (predeterminado `auto`).

Para desarrollo, el servidor HTTP carga automáticamente `.env` del directorio actual o, si no existe, de la raíz del módulo Go usando [godotenv](https://github.com/joho/godotenv). Copiar `.env.example` a `.env` y completar los valores locales; ejecutar `go run ./cmd/api` desde la raíz o `go run .` desde `cmd/api`. Precedencia: variables del proceso, `.env`, valores predeterminados. Un archivo local inválido se rechaza sin buscar otro. PostgreSQL usa `DATABASE_URL` para la aplicación y `MEDIA_DATABASE_URL` para el catálogo de medios gestionados, con esquema `postgres` o `postgresql`, host, base, usuario y contraseña. Los caracteres especiales de las credenciales se codifican como componentes URI. `init-db` recibe la conexión del dueño exclusivamente desde el entorno del proceso. `SPRING_DATASOURCE_*`, JDBC y `DB_USERNAME`/`DB_PASSWORD` ya no configuran PostgreSQL. Los aliases de Gateway/Billing documentados abajo permanecen. `.env` queda excluido de Git y de la imagen Docker; el contenedor puede recibir variables mediante `--env-file .env`. Un archivo inválido se rechaza sin exponer su contenido.

Las peticiones privadas requieren encabezados de confianza del servidor Gateway: `X-Gateway-Secret`, `X-User-Id`, `X-User-Role`, `X-User-Plan` y `X-User-Plan-Expires-At` (RFC3339 futuro). Sin beneficio válido se responde 403, salvo la excepción administrativa autorizada. El navegador envía exclusivamente su bearer; el Gateway valida e inyecta identidad/beneficio. Salud mantiene su acceso público. Billing conserva carga, reintentos, fallback y recarga. El almacenamiento y mantenimiento respetan propiedad, transacciones y cola durable de eliminación.

El conversor local de imágenes fue retirado. Las cargas JPEG, PNG y WebP se envían desde DoctorChart a `syntah-image-converter-go` con el perfil fijo `doctorchart-study`, después de autorizar la operación y la propiedad del paciente. Dimensiones, calidad, formato, orientación y metadatos se procesan exclusivamente en ese servicio. DoctorChart valida la respuesta y guarda la versión procesada cuando es menor; nombre, MIME y tamaño reflejan los bytes guardados. PDF, GIF, BMP y TIFF mantienen su almacenamiento original; imágenes animadas, de alta profundidad o con ICC se conservan cuando el conversor informa explícitamente esa condición. No hay un codificador local de respaldo. Una falla de credencial, permiso, servicio o validación impide guardar y crear el registro. Los límites de entrada del backend son 10 MB (10.000.000 bytes) por archivo y 40 MP.

Para conectar el servicio, configurar `IMAGE_CONVERTER_URL`, `IMAGE_CONVERTER_API_KEY` y `STUDIES_ALLOW_IMAGE_TRANSFORM`. La clave debe coincidir con `APP_DOCTORCHART_API_KEY` del conversor y ser independiente de Gateway/Billing. `STUDIES_ALLOW_IMAGE_TRANSFORM=false` exige conservación byte por byte; `true` autoriza la transformación. Por decisión del propietario, el perfil principal del conversor usa WebP calidad 75, máximo 2048 px por eje, sin recorte ni ampliación; conserva resolución dentro de ese máximo y guarda el resultado sólo si pesa menos. El ahorro depende de la imagen y no garantiza un tamaño fijo ni acredita fidelidad clínica. El perfil del servicio debe coincidir con esa decisión. Si las dos variables de conexión se omiten, el backend puede arrancar, pero las cargas raster compatibles devuelven 503; una configuración parcial o inválida impide arrancar. Ver [configuración y prueba completa](integration/SHARED_IMAGE_CONVERTER.md).

Las descargas y archivos existentes no se transforman. Los PDF incluyen fuentes UTF-8 para los glifos soportados y `private, no-store`; caracteres o escrituras no representables generan 422 `PDF_UNSUPPORTED_CHARACTER`, sin emitir un documento ni modificar datos médicos. Los cambios inocuos de bytes/fuente/paginación respecto de Spring están documentados y aceptados; se conserva la corrección de acceso entre propietarios.

El Dockerfile genera un binario Linux sin CGO, con certificados y zona horaria, y lo ejecuta como usuario 65532. Se probaron el contenedor local y el apagado con una petición en curso; no se desplegó.

## Integración actual reproducible

El laboratorio `ops/testing/specialty-m6-lab.mjs` usa PostgreSQL 17 y 18 descartables en Docker Linux. Compila los repositorios principales de DoctorChart y Gateway sin modificarlos. Auth y Billing usan adaptadores locales; Angular, Gateway, API, PostgreSQL, conversor y Mailpit son reales. Requiere Docker, Node, dependencias Angular/Playwright y Chrome. No recibe conexiones de desarrollo ni producción. Sus recursos llevan una etiqueta propia que se comprueba antes de eliminarlos.

En una terminal, desde este repositorio:

```powershell
node ops/testing/specialty-m6-lab.mjs prepare
node ops/testing/specialty-m6-lab.mjs build
node ops/testing/specialty-m6-lab.mjs reset-e2e
node ops/testing/specialty-m6-lab.mjs go-gates 17
node ops/testing/specialty-m6-lab.mjs go-gates 18 race
```

`reset-e2e` vacía exclusivamente la base descartable del laboratorio. Para la prueba de navegador, desde el repositorio del frontend:

```powershell
node node_modules/@angular/cli/bin/ng.js build --configuration m6-lab
node scripts/specialty-contexts-m6-real.cjs
```

La prueba cubre tres especialidades, fichas independientes, consultas, recetas, estudios y descargas, turnos, reservas y secretaría. Para repetirla se requiere otro `reset-e2e` del laboratorio. Las evidencias quedan en `%TEMP%/doctorchart-specialty-m6-20261110`. Para retirar únicamente los contenedores y cachés del laboratorio, ejecutá desde el backend `node ops/testing/specialty-m6-lab.mjs down`.

Los scripts y resultados de `integration/` corresponden al cierre anterior a RLS: asumían inicialización dentro del proceso HTTP y credenciales del dueño. Se conservan como evidencia histórica; para el esquema 16 usá el laboratorio con roles separados.

Para staging hay que fijar la revisión Gateway y reconstruir Angular con URLs públicas de ese entorno, según [la guía Angular](../doctorchart-frontend/docs/angular-go-staging.md). Supabase/OAuth/renovación automática, Billing/pagos, AWS/R2, Gemini e impresoras reales requieren validación externa. No se desplegó ni se modificaron bases reales.

## Registro histórico de la migración Spring — 09/10/2026

La comparación histórica necesitó Windows, Python 3, Docker, Java 25 y Spring en el commit registrado. `MIGRATION_JAVA` indicaba la ruta de `java.exe`, y la referencia se compiló únicamente en `.migration/spring` después de comprobar los hashes del original. Ese procedimiento dependía del esquema anterior; el backend actual rechaza esa base sin el marcador del nuevo baseline. Los scripts y evidencias conservados documentan aquel cierre, no una dependencia de arranque ni compatibilidad actual con Spring. No reescribir evidencia histórica al consolidar el cierre actual.

Los ejecutables de comparación y extracción SQL a partir de Java fueron retirados tras comprobar la equivalencia del catálogo final; los informes y matrices permanecen como evidencia histórica. Los módulos migration/compare.py, verify_documents.py y verify_restore_runtime.py conservan únicamente helpers reutilizados por la integración Go.

Para crear un nuevo fixture Go descartable, migration/fixture.py prepare registra contenedor PostgreSQL 17 vacío, ID exacto, label y puerto loopback. Rechaza descriptores anteriores y nunca reconstruye SQL a partir de Java. migration/fixture.py stop comprueba esa identidad antes de retirarlo. Ese comando no se ejecutó sobre las bases de desarrollo.

Control de calidad completo, con `govulncheck` instalado y una plataforma compatible con race:

```powershell
python .agents/skills/go-backend/scripts/check_go.py . --target go1.27.4 --require-vulncheck
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
