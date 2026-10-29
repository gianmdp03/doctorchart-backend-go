# M2 — contrato compartido de consultorios
**M2-CLINICS-v0.1 · PROPUESTO / pendiente de aprobación.** Única fuente de DTO, rutas, permisos y mocks para [Sol](../tasks/M2_SOL.md) y [Argon](../tasks/M2_ARGON.md). No autoriza implementación ni publicación.

## Identidad y alcance
API de gestión autenticada del médico, prefijo `/api/doctorchart`. Angular usa su interceptor/login existente; Go conserva middleware Gateway y la identidad verificada `api.User(ctx).UserID`, roles existentes y vigencia de acceso. No agregar un login ni confiar en identidad/plan enviados por el formulario.
Resolver `doctor_workspaces.user_id` desde esa identidad. Todas las consultas/mutaciones se limitan a su workspace; ID ajeno responde igual que inexistente. No aceptar `ownerId`, `userId` o `workspaceId` en requests. Sin suplantación ni bypass comercial implícito para administradores.
Consultorios son lugares; pacientes e historias siguen por médico. Fuera: horarios, reservas, staff, portal público y cambios clínicos. `/appointments` permanece reservado a la fase pública futura.

## Modelo expuesto: Clinic
```json
{
  "id": "101",
  "name": "Consultorio Centro",
  "country": "Argentina",
  "province": "Buenos Aires",
  "city": "Mar del Plata",
  "address": "Dirección de ejemplo 123",
  "timezone": "America/Argentina/Buenos_Aires",
  "status": "ACTIVE",
  "version": "0",
  "createdAt": "2026-10-29T15:00:00Z",
  "updatedAt": "2026-10-29T15:00:00Z"
}
```
IDs y versiones: strings decimales canónicos no negativos dentro de BIGINT; ID > 0. Evita pérdida de precisión en JavaScript. `expectedVersion` usa el mismo tipo, solo para este contrato nuevo; no cambiar contratos clínicos existentes.
Timestamps de respuesta ISO/RFC3339; UI en español y formato argentino. Campos requeridos, sin null, trim y no vacíos: name ≤160, country ≤100, province/city ≤160, address ≤255, timezone ≤100. No agregar unicidad de nombre ni otros requisitos fuera del esquema M1.
Zona: validar en Go con `time.LoadLocation`, coherente con SQL: UTC o zona IANA nombrada con región; rechazar Local, offsets, abreviaturas y prefijos posix/right. Reutilizar tzdata ya incorporado sin alterar el significado del parámetro global de presentación `timeZone`. Error de campo; CHECK SQL continúa como defensa.

## Rutas y mensajes propuestos
`{id}` es Clinic.id; todos los DTO son JSON.

| Método y ruta relativa al prefijo | Request | Response |
|---|---|---|
| GET /clinics?page=0&size=20&status=INACTIVE | page ≥0, size 1..100, status opcional enum | 200 `{items: Clinic[], page, size, totalElements}`; sin status incluye todos, orden id ascendente |
| GET /clinics/capabilities | Sin body | 200 Capabilities, incluso si Billing no está disponible |
| GET /clinics/{id} | Sin body | 200 Clinic |
| POST /clinics | Todos los campos de ubicación de Clinic, sin id/status/version/timestamps | 201 Clinic y Location; estado inicial ACTIVE, versión 0 |
| PATCH /clinics/{id} | `expectedVersion` + al menos un campo de ubicación; parcial | 200 Clinic actualizado; versión +1 |
| PATCH /clinics/{id}/status | `expectedVersion`, `status` | 200 Clinic; versión +1 si cambia |

Ejemplos de cuerpos (la respuesta mutante es siempre Clinic completo):
```json
{"name":"Consultorio Centro","country":"Argentina","province":"Buenos Aires","city":"Mar del Plata","address":"Dirección de ejemplo 123","timezone":"America/Argentina/Buenos_Aires"}
```
```json
{"expectedVersion":"0","name":"Consultorio Norte"}
```
```json
{"expectedVersion":"1","status":"INACTIVE"}
```
Rechazar campos desconocidos/null y evitar asignación masiva. No hay DELETE. Transiciones propuestas: ACTIVE → INACTIVE/ARCHIVED; INACTIVE → ACTIVE/ARCHIVED; ARCHIVED → INACTIVE (restaurar). Archivado es de solo lectura hasta restaurarlo. Estado idéntico con versión vigente: 200 sin incrementar. Versión desactualizada: 409; conservar el formulario para recarga/resolución, sin sobrescribir.

## Capabilities y derechos efectivos
Ejemplo sin derechos comerciales verificables; counted=0 describe un workspace vacío, no una cuota por plan:
```json
{
  "entitlements": {
    "status": "UNAVAILABLE",
    "revision": null,
    "validUntil": null,
    "clinicManagement": null,
    "max_clinics": null
  },
  "usage": {"counted": 0},
  "actions": {
    "create": false, "edit": false, "activate": false,
    "deactivate": false, "archive": false, "restore": false
  },
  "reason": "ENTITLEMENTS_UNAVAILABLE"
}
```
Tipos: status = VERIFIED / DENIED / UNAVAILABLE; revision = string o null; validUntil = RFC3339 o null; clinicManagement = boolean o null; max_clinics = entero ≥0, -1 explícito o null desconocido. Actions siempre boolean; reason = null, ENTITLEMENTS_UNAVAILABLE, CLINIC_MANAGEMENT_DISABLED o CLINIC_LIMIT_REACHED. VERIFIED exige revisión no vacía, vigencia futura, clinicManagement=true y max_clinics válido. DENIED es una denegación comercial vigente verificada; datos incompletos son UNAVAILABLE. Son permisos máximos globales; edición/estado además dependen del estado/version del recurso.
Fuente: Bycode/Billing debe proveer un derecho efectivo autenticado para propietario + módulo DoctorChart, con revisión, vigencia, habilitación de gestión y `max_clinics`. La ruta interna y autenticación de ese proveedor aún deben acordarse; no se inventan aquí ni se presume que ya existen. Catálogo por tier/JWT de plan solos no sustituyen ese derecho.
No cantidades por tier, defaults comerciales ni privilegio ilimitado por error. null/clave ausente/valor inválido no equivale a ilimitado; -1 solo es admisible si Billing lo concede expresamente en un derecho verificado. Angular decide disponibilidad por actions del servidor, no infiriéndola del nombre del plan o del máximo.

## Propuesta de cuota y degradación — aprobar antes de congelar
- Consumen cupo ACTIVE e INACTIVE; ARCHIVED no. Crear o restaurar exige cupo libre. Activar exige feature vigente y no estar sobre el límite, aunque no aumente el conteo.
- Downgrade conserva todo. Si counted > max_clinics: bloquear crear/restaurar/activar/editar; permitir inactivar/archivar con derecho de gestión vigente para que el médico elija qué conservar. No archivar automáticamente.
- Listado y detalle siguen disponibles sin consultar Billing si la autenticación/plan vigente existentes permiten el acceso; no eludir el middleware de planes vencidos.
- Hasta disponer de proveedor efectivo integrado/verificado: TODAS las mutaciones deshabilitadas, incluidas edición, inactivación y archivo. Capabilities devuelve UNAVAILABLE/actions false; mutaciones responden 503 sin escribir.
- Propuesta conservadora inicial ante fallo/timeout de Billing: misma regla, aun si hay datos cacheados anteriores. No usar caché antigua para autorizar escrituras; no borrar ni alterar registros.
- Derecho verificado que deniega gestión: DENIED, actions false, mutaciones 403. Derecho vigente que alcanza cupo: 409 para alta/restauración; no 503.
- Cada mutación revalida derechos en backend. No basta un GET capabilities previo. Obtener el derecho fuera de la transacción, luego lock estable del workspace antes de registro/conteo y escritura atómica; todas las mutaciones siguen ese orden. Validar revisión/vigencia según contrato Billing; concurrencia por último cupo admite una sola alta.
- Consistencia de revisión/revocación entre Billing y DoctorChart debe quedar acordada antes de habilitar escrituras reales; los mocks no acreditan esa integración.

## Errores
Conservar `api.Error`: `{code, message, errors:[{path,code}]}`; mensajes en español.
```json
{"code":"INVALID_REQUEST","message":"La zona horaria no es válida.","errors":[{"path":"timezone","code":"INVALID_TIMEZONE"}]}
```

| HTTP | code | Uso |
|---|---|---|
| 400 | INVALID_REQUEST | Campos, enum, ID, versión o paginación inválidos; errors por campo |
| 401 | UNAUTHENTICATED | Identidad ausente según autenticación existente |
| 403 | FORBIDDEN / DOCTORCHART_PLAN_REQUIRED | Rol o beneficio inválido según middleware existente |
| 403 | CLINIC_MANAGEMENT_DISABLED | Derecho verificado o habilitación operativa deniega esta gestión |
| 404 | CLINIC_NOT_FOUND | Inexistente o de otro médico |
| 409 | ONBOARDING_REQUIRED | No existe workspace del médico |
| 409 | VERSION_CONFLICT | Edición concurrente; recargar antes de reintentar |
| 409 | CLINIC_LIMIT_REACHED | Alta/restauración/activación bloqueada por cuota |
| 409 | CLINIC_STATE_CONFLICT | Transición o edición de archivado no permitida |
| 503 | ENTITLEMENTS_UNAVAILABLE | Proveedor no integrado, inaccesible, vencido o inválido; cero escrituras |
| 500 | INTERNAL_ERROR | Fallo interno sin revelar SQL ni secretos |

Errores heredados de autenticación conservan sus códigos, headers y precedencia. Si un recurso no pertenece al médico, responder 404 antes de evaluar sus permisos comerciales. Un cambio entre capabilities y mutación puede devolver 403/409/503: el frontend refresca permisos sin inventar éxito. No reintentar POST automáticamente tras respuesta ambigua.

## Condición para trabajo paralelo
Tras aprobación del propietario, congelar versión y hash/SHA de este único documento. Argon genera tipos/mocks y pruebas con estos casos: lista vacía/paginada, permisos verificados, cuota cero/alcanzada/-1 explícito, denegación, proveedor caído/no integrado, conflicto de versión y recurso ajeno.
Mocks solo tests o desarrollo explícito; nunca fallback productivo ante error real. Rutas/nav nuevas detrás de flag inicialmente apagado; habilitar operación exige aprobación de activación, integración real y actions efectivos, con control también en backend.
El consumidor actual `internal/billing/rules.go` rechaza claves distintas de las dos cuotas clínicas. Sol debe preparar compatibilidad de `max_clinics` sin romperlas; no publicar esa clave desde Billing hacia clientes antiguos ni modificar Bycode/Gateway automáticamente. Cambios necesarios del proveedor externo son dependencia a autorizar, no trabajo implícito de estos tickets.
