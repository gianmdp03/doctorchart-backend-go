# DoctorChart local en Windows

Para pruebas funcionales con datos ficticios y baseline 20, usá el [entorno manual SP-05A](../../docs/manual-testing-sp05a.md). La guía que sigue corresponde a la instancia local anterior de turnos.

Requisitos: Docker Desktop iniciado, Go 1.27.4, Node y dependencias Angular instaladas en el repositorio principal del frontend. La instancia compila los checkouts de `doctorchart-backend-go`, `doctorchart-frontend` y `api-ai-gateway-go`. Usa el adaptador de cuentas y permisos comerciales de desarrollo.

| Servicio | Dirección |
|---|---|
| DoctorChart | https://localhost:4302/ |
| PostgreSQL | 127.0.0.1:15434 |
| Mailpit | http://localhost:18026 |

Si la instancia ya está preparada, hacé doble clic en `Iniciar DoctorChart.cmd` en `C:\Users\gcast\Documents\DoctorChartLocal`. El lanzador compila los servicios Go, espera a que DoctorChart responda y abre Chrome. Para detener la instancia, usá `Detener DoctorChart.cmd` en la misma carpeta.

Desde el repositorio principal del backend podés iniciar y comprobar los servicios con estos comandos.

```powershell
node ops/local/manage.mjs start
node ops/local/manage.mjs verify
node ops/local/manage.mjs status
```

`ACCESOS.txt` contiene las contraseñas locales de `doctor@local.test` y `admin@local.test`. Las cuentas médicas adicionales se guardan en `turnos\settings.json`, en la lista `doctors`. El administrador entra al panel de especialidades. Cerrá sesión antes de cambiar de cuenta en la misma ventana. Para dar acceso a secretaría, enviá una invitación desde la cuenta médica y completá la activación con el correo capturado en Mailpit.

Usá `node ops/local/manage.mjs prepare` para preparar una instalación nueva o regenerar configuración faltante. Revisá y respaldá primero cualquier configuración local que quieras conservar. El comando vuelve a generar `config/local.public.json` y `src/environments/environment.local.generated.ts`, ambos ignorados por Git. Reutiliza las credenciales y el certificado existentes. Si Windows ya confía en ese certificado, podés configurar `$env:DOCTORCHART_SKIP_CERT_IMPORT = '1'` en PowerShell antes de ejecutar `prepare` para omitir la importación. Las credenciales y los archivos clínicos se guardan fuera del repositorio.

El proyecto Docker Compose conserva el nombre `doctorchart-local-turnos` y el estado en `DoctorChartLocal\turnos` para reutilizar PostgreSQL, archivos, correo y los enlaces de prueba existentes. Esa carpeta es el directorio de datos del servicio. Los cambios de Angular se recargan desde el repositorio principal. Para recompilar después de un cambio en Go, ejecutá `node ops/local/manage.mjs stop` y luego `node ops/local/manage.mjs start`.

La base anterior en el volumen `doctorchart-local-actual_postgres-data` se conserva por separado. El volumen activo es `doctorchart-local-turnos_postgres-data`. Para una instalación vacía, `prepare` genera credenciales separadas y el lanzador inicializa el esquema 16 antes de arrancar la API. Una instalación anterior requiere una actualización autorizada antes de recompilar y arrancar con RLS. El lanzador comprueba el marcador antes de reemplazar el binario.

Antes de reinicializar, respaldá PostgreSQL, `turnos/storage`, `turnos/settings.json`, las claves y la configuración local. Un respaldo físico con `pg_basebackup` conserva el fingerprint del catálogo. Verificalo con `pg_verifybackup` y ensayá la restauración en una copia aislada de la misma versión de PostgreSQL. La contraseña de PostgreSQL está en el campo `database` de `settings.json`.

Las invitaciones y los correos de turnos se capturan en Mailpit con SMTP STARTTLS. El adaptador es exclusivo de desarrollo. Usa cuentas locales, JWT firmado y permisos comerciales simulados. La validación de Google OAuth, cobros y servicios externos requiere sus entornos propios.

Las pruebas Go de integración se pueden ejecutar en contenedores Linux con PostgreSQL descartable para evitar avisos del firewall de Windows. Consultá las [pruebas del esquema inicial](../../db/README.md).

```powershell
node ops/local/manage.mjs status
node ops/local/manage.mjs stop
node ops/local/browser-smoke.mjs
docker exec doctorchart-local-turnos-local-support-1 node /local/dependencies-smoke.mjs --mail
```
