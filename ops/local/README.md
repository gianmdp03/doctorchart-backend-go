# DoctorChart local en Windows

Requisitos: Docker Desktop iniciado, Go 1.27.3, Node y dependencias Angular instaladas en el repositorio principal del frontend. La instancia compila los checkouts de `doctorchart-backend-go`, `doctorchart-frontend` y `api-ai-gateway-go`. Usa el adaptador de cuentas y permisos comerciales de desarrollo.

| Servicio | Dirección |
|---|---|
| DoctorChart | https://localhost:4302/ |
| PostgreSQL | 127.0.0.1:15434 |
| Mailpit | http://localhost:18026 |

Desde el repositorio principal del backend:

```powershell
node ops/local/manage.mjs prepare
node ops/local/manage.mjs start
node ops/local/manage.mjs verify
```

También podés usar `Iniciar DoctorChart.cmd` y `Detener DoctorChart.cmd` en `C:\Users\gcast\Documents\DoctorChartLocal`. El inicio abre la portada en Chrome. `ACCESOS.txt` contiene las contraseñas locales de `doctor@local.test` y `admin@local.test`. El administrador entra al panel de especialidades. Cerrá sesión antes de cambiar de cuenta en la misma ventana.

`prepare` conserva las credenciales y el certificado existente, y configura el frontend en `config/local.public.json` y `src/environments/environment.local.generated.ts`, ambos ignorados por Git. Si se genera un certificado nuevo, la instalación se limita al usuario actual de Windows. Las credenciales y los archivos clínicos se guardan fuera del repositorio.

El proyecto Docker Compose conserva el nombre `doctorchart-local-turnos` y el estado en `DoctorChartLocal\turnos` para reutilizar PostgreSQL, archivos, correo y los enlaces de prueba existentes. Esa carpeta es el directorio de datos del servicio. Los cambios de Angular se recargan desde el repositorio principal. Para recompilar después de un cambio en Go, ejecutá `node ops/local/manage.mjs stop` y luego `node ops/local/manage.mjs start`.

La base anterior en el volumen `doctorchart-local-actual_postgres-data` se conserva por separado. Para respaldar datos del entorno activo, guardá un `pg_dump` junto con `turnos/storage` y `turnos/settings.json`, que incluye la clave estable de notificaciones. Evitá `docker compose down -v`, que elimina volúmenes. La contraseña de PostgreSQL está en el campo `database` del `settings.json`.

Las invitaciones y los correos de turnos se capturan en Mailpit con SMTP STARTTLS. El adaptador es exclusivo de desarrollo. Usa cuentas locales, JWT firmado y permisos comerciales simulados. La validación de Google OAuth, cobros y servicios externos requiere sus entornos propios.

```powershell
node ops/local/manage.mjs status
node ops/local/manage.mjs stop
node ops/local/browser-smoke.mjs
docker exec doctorchart-local-turnos-local-support-1 node /local/dependencies-smoke.mjs --mail
```
