# Mapa de repositorios y alcance verificado
HEAD/ramas y estado Git local verificados el 29/10/2026: ver [handoff](05_HANDOFF.md). Se cotejaron esquema, bootstrap y política del backend de forma focalizada; no se reauditaron todas las integraciones ni se ejecutaron pruebas. Los puntos de entrada de esta tabla siguen siendo referencias históricas a verificar antes de cada fase.
| Repositorio | Ámbito técnico reportado | Puntos de entrada reportados |
|---|---|---|
| `doctorchart-backend-go` | Go, PostgreSQL, pacientes médicos, wizard, nuevos lugares/turnos/staff | `db/schema.sql`, `db/init.go`, `internal/clinical/patient.go`, `internal/clinical/encounter.go`, `internal/api/http.go`, `internal/workspace/` |
| `doctorchart-frontend` | Angular, shell médico, futuro portal/admin | `src/app/app.routes.ts`, `src/app/core/interceptors/auth.interceptor.ts`, wizard, servicios clínicos |
| `api-ai-gateway-go` | Login médico y rutas públicas/staff, CORS/headers | `main.go`, `auth.go`, `route_policy.go` |
| `bycode-backend-go` | Billing, cobertura y catálogo de límites, cola webhook | `db/schema.sql`, `internal/billing/catalog.go`, `internal/billing/payment_jobs.go`, `internal/billing/integrations.go` |

**No se cambia el ámbito clínico existente.** Una consulta o cita puede registrar lugar de realización opcional en futuro, pero ese dato no define autorización ni propiedad del paciente.

Ubicación canónica instalada: `doctorchart-backend-go/docs/ai/`. Una conversación o repositorio distinto necesita acceso real a esos bytes, no basta con mencionarlos. Comprobar commits reales al iniciar la sesión; los hashes históricos de auditorías pueden estar desactualizados.
