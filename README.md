# DoctorChart backend en Go

El módulo de curvas pediátricas incorpora las 20 láminas del PDF con datos OMS, LMS y SVG en Angular. Su [documentación y verificación](docs/growth-charts.md) detalla catálogo, fuentes, endpoint, límites clínicos y pruebas reproducibles.

Backend definitivo de DoctorChart: **75/75 endpoints y 7/7 procesos implementados; READY FOR STAGING dentro del alcance local verificado**. Angular, Gateway desde copia, Go y PostgreSQL se probaron juntos. Consultá el [informe actual](integration/REPORT.md), el [estado de integración](integration/STATE.md) y la [evidencia](integration/evidence/summary.json). La [migración Spring](migration/REPORT.md) y su [matriz](migration/CONTRACT_MATRIX.csv) son evidencia histórica; no se afirma igualdad binaria universal.

El servidor usa `net/http`, dependencias explícitas, PostgreSQL mediante `pgx`, SQL parametrizado, transacciones y versiones optimistas. Conserva las rutas y la configuración original, crea el esquema cuando no existen tablas de DoctorChart, inicializa catálogos vacíos y preserva los existentes. No requiere Java para funcionar.

## Ejecución local

Requiere Go 1.27.2 y una base PostgreSQL creada y accesible. Al arrancar sin tablas de DoctorChart, crea automáticamente sus 20 tablas, identidades, índices y restricciones usando [schema.sql](internal/bootstrap/schema.sql), incluido en el binario y derivado de [schema-reference.sql](migration/schema-reference.sql). Luego inicializa los catálogos. La creación es transaccional y se coordina entre réplicas mediante un bloqueo; si falla, revierte todo el esquema creado. Una base completa conserva sus tablas y datos; una base parcialmente eliminada se rechaza y requiere restaurar las tablas faltantes. El rol necesita permisos CREATE en la base o en `public` para la creación inicial. Se verificó el arranque en PostgreSQL 18. No ejecutar los comandos de prueba contra bases existentes.

```powershell
$env:PORT = '8083'
$env:SPRING_DATASOURCE_URL = 'jdbc:postgresql://localhost:5432/doctorchart'
$env:DB_USERNAME = 'doctorchart'
$env:DB_PASSWORD = '<clave local>'
$env:GATEWAY_SHARED_SECRET = '<secreto del gateway de al menos 32 caracteres>'
$env:SERVICES_BILLING_SHARED_SECRET = '<secreto de billing de al menos 32 caracteres>'
$env:SERVICES_BILLING_URL = 'http://localhost:8081'
$env:STUDIES_STORAGE_DIR = 'C:\DoctorChartData\studies'
go run ./cmd/api
```

El directorio de estudios debe ser absoluto, privado y externo al proyecto; no se admiten enlaces simbólicos. En Linux se crean directorios con permisos 0700 y archivos con 0600. Para S3 configurá `STORAGE_TYPE=s3`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` y, si corresponde, `S3_REGION` (predeterminado `auto`).

Para desarrollo se carga automáticamente `.env` del directorio actual o, si no existe, de la raíz del módulo Go usando [godotenv](https://github.com/joho/godotenv). Copiar `.env.example` a `.env` y completar los valores locales; ejecutar `go run ./cmd/api` desde la raíz o `go run .` desde `cmd/api`. Precedencia: variables del proceso, `.env`, valores predeterminados. Un archivo local inválido se rechaza sin buscar otro. Se preservan los aliases de entorno existentes para compatibilidad con Gateway y herramientas de prueba. No se carga `secrets.properties` ni se requieren escapes Java. `.env` queda excluido de Git y de la imagen Docker; el contenedor puede recibir variables mediante `--env-file .env`. Si no existe el archivo se usan las variables del proceso; si existe pero es inválido, se rechaza sin exponer su contenido.

Las peticiones privadas requieren encabezados de confianza del servidor Gateway: `X-Gateway-Secret`, `X-User-Id`, `X-User-Role`, `X-User-Plan` y `X-User-Plan-Expires-At` (RFC3339 futuro). Sin beneficio válido se responde 403, salvo la excepción administrativa autorizada. El navegador envía exclusivamente su bearer; el Gateway valida e inyecta identidad/beneficio. Salud mantiene su acceso público. Billing conserva carga, reintentos, fallback y recarga. El almacenamiento y mantenimiento respetan propiedad, transacciones y cola durable de eliminación.

El conversor local de imágenes fue retirado. Las cargas JPEG, PNG y WebP se envían desde DoctorChart a `syntah-image-converter-go` con el perfil fijo `doctorchart-study`, después de autorizar la operación y la propiedad del paciente. Dimensiones, calidad, formato, orientación y metadatos se procesan exclusivamente en ese servicio. DoctorChart valida la respuesta y guarda la versión procesada cuando es menor; nombre, MIME y tamaño reflejan los bytes guardados. PDF, GIF, BMP y TIFF mantienen su almacenamiento original; imágenes animadas, de alta profundidad o con ICC se conservan cuando el conversor informa explícitamente esa condición. No hay un codificador local de respaldo. Una falla de credencial, permiso, servicio o validación impide guardar y crear el registro. Los límites de entrada del backend son 10 MB (10.000.000 bytes) por archivo y 40 MP.

Para conectar el servicio, configurar `IMAGE_CONVERTER_URL`, `IMAGE_CONVERTER_API_KEY` y `STUDIES_ALLOW_IMAGE_TRANSFORM`. La clave debe coincidir con `APP_DOCTORCHART_API_KEY` del conversor y ser independiente de Gateway/Billing. `STUDIES_ALLOW_IMAGE_TRANSFORM=false` exige conservación byte por byte; `true` autoriza el perfil transformador de las pruebas sintéticas. El perfil del servicio debe coincidir con esa decisión. Si las dos variables de conexión se omiten, el backend puede arrancar, pero las cargas raster compatibles devuelven 503; una configuración parcial o inválida impide arrancar. Ver [configuración y prueba completa](integration/SHARED_IMAGE_CONVERTER.md).

Las descargas y archivos existentes no se transforman. Los PDF incluyen fuentes UTF-8 para los glifos soportados y `private, no-store`; caracteres o escrituras no representables generan 422 `PDF_UNSUPPORTED_CHARACTER`, sin emitir un documento ni modificar datos médicos. Los cambios inocuos de bytes/fuente/paginación respecto de Spring están documentados y aceptados; se conserva la corrección de acceso entre propietarios.

El Dockerfile genera un binario Linux sin CGO, con certificados y zona horaria, y lo ejecuta como usuario 65532. Se probaron el contenedor local y el apagado con una petición en curso; no se desplegó.

## Integración actual reproducible

El harness Windows requiere Go, Docker, Python con `cryptography`, Pillow, pypdf y pypdfium2; Node/Angular, Playwright y Chrome instalados. Usa los repositorios hermanos `doctorchart-frontend`, `api-gateway` y `billing-service-go`. Gateway/Billing se leen y el Gateway se compila desde una copia propia; el refactor original no se modifica. Se necesita el contenedor PostgreSQL descartable registrado en `.migration/fixture.json`. Si aún no existe, la preparación histórica `python migration/fixture.py prepare` requiere también Java y la copia Spring previamente compilada; crea la fixture propia y genera el esquema de referencia. No es necesario repetir esa preparación cuando la fixture ya existe. No proporcionar credenciales de una base real.

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

Detener solo los procesos propios con `New-Item -ItemType File .migration/integration/stop`; las bases sintéticas se retienen. El resumen actual acredita Angular 573 pruebas, siete controles Go, HTTP inicial 137, Gateway final 24, Chrome final 18 más tres casos de preparación, PDF 20 y restore de 20 tablas. Consultar el informe para el alcance exacto de cada ejecución.

Para staging hay que fijar la revisión Gateway y reconstruir Angular con URLs públicas de ese entorno, según [la guía Angular](../doctorchart-frontend/docs/angular-go-staging.md). Supabase/OAuth/renovación automática, Billing/pagos, AWS/R2, Gemini e impresoras reales requieren validación externa. No se desplegó ni se modificaron bases reales.

## Reproducción histórica de la migración Spring

Este harness histórico no es un paso necesario para continuar la integración actual. Necesita Windows, Python 3, Docker, Java 25 y Spring en el commit registrado. `MIGRATION_JAVA` indica la ruta de `java.exe`. La referencia se compila únicamente en `.migration/spring`, después de comprobar los hashes del original. No reescribir evidencia histórica al consolidar el cierre actual.

Ejecutá secuencialmente desde este proyecto:

```powershell
python -m pip install -r migration/requirements.txt
go test ./...
go build -o bin/doctorchart.exe ./cmd/api
python migration/build_reference.py
python migration/fixture.py prepare
python migration/compare.py
python migration/verify_lifecycle.py
python migration/verify_documents.py
python migration/performance.py
docker build -t doctorchart-go:migration-local .
python migration/verify_restore_runtime.py
python migration/verify_source.py
python migration/update_matrix.py
```

El fixture genera credenciales desechables en `.migration/fixture.json`, crea sus propios clones PostgreSQL y usa servicios locales de Billing/S3. Los reportes públicos contienen datos sintéticos. `python migration/fixture.py stop` elimina únicamente el contenedor de pruebas identificado en esa configuración.

Control de calidad completo, con `govulncheck` instalado y una plataforma compatible con race:

```powershell
python .agents/skills/go-backend/scripts/check_go.py . --target go1.27.2 --require-vulncheck
```

El control completo y race se ejecutaron en Linux mediante Docker. Las [evidencias](migration/evidence/summary.json) registran 365 casos diferenciales, 41 verificaciones de rollback, nueve casos de arranque y 14 de documentos. Una ejecución aprobada acredita esos casos; las diferencias y límites del informe siguen vigentes. `finalize_evidence.py` consolida los resultados ya generados y requiere también las capturas de pruebas Go y de revisión visual; no sustituye la ejecución de los controles.

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
