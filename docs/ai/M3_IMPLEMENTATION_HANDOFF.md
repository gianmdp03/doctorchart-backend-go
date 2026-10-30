# M3 — continuidad de implementación
30/10/2026. Propietario autorizó M3 completo con un único agente; Argon ya no participa. M4 no autorizado.

Checkouts habituales permanecen en main sin cambios. Trabajo aislado en clones normales, NO worktrees:
- C:/Users/gcast/Documents/GitHub/doctorchart-m3/doctorchart-backend-go
- C:/Users/gcast/Documents/GitHub/doctorchart-m3/doctorchart-frontend
Ambos en codex/m3-availability, derivados de turnos aprobado (c3bf46c / 471daaf). Los origin de estos clones apuntan a los repositorios locales, no a GitHub. No publicar ni integrar automáticamente.

Hecho: contrato inicial M3_AVAILABILITY_API.md (51b96b8); núcleo temporal Go y regresiones (939d91a). TestScheduleCivilTime y TestScheduleValidationAndExceptions PASS, incluyendo múltiples franjas, vacaciones, superposiciones, gap/fold DST y fecha inexistente Pacific/Apia.
Frontend: SCHEDULES_ENABLED=false, sin activación de CLINICS_ENABLED.
Borrador todavía NO conectado al runner: db/migrations/0005_clinic_schedules.sql. No hay cambios a schema.sql ni migraciones históricas.

Bloqueo de entorno: Docker Desktop Linux Engine no está disponible (pipe dockerDesktopLinuxEngine inexistente). Se solicitó al propietario iniciarlo. No se usaron DSN reales ni se inventaron fingerprints. Para completar la migración hay que construir catálogos independientes PG17/18 desde los SQL publicados, cablear versión 5 y verificar fresh/upgrade/rollback con datos clínicos sintéticos.

Pendiente de M3: persistencia/API y validación cruzada entre consultorios bajo lock de workspace/fence M2; revalidar cambios de zona/estado de consultorio; editor Angular con app-date-picker, disponibilidad día/semana, errores y borradores; integración real, suites completas, builds y evidencia visual. El período de validación cruzada debe quedar explícito en contrato antes de congelarlo definitivamente. El núcleo temporal no constituye una API ni una funcionalidad usable completa.

No declarar M3 terminado. Sin pushes, merges a main, Cloudflare, despliegues o migraciones reales.

