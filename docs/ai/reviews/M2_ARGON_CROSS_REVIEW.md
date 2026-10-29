# M2-ARGON — auditoría cruzada de Sol

29/10/2026. **REQUIERE CORRECCIONES: 0 P0/P1, 3 P2, 1 observación P3.**
Frontend examinado: `codex/m2-clinics-frontend`, commit exacto **ea4f4d7b217134cfb7e3f9ccd8902dcd4beae898**, base 14196974125e5769c5a95e309896397acad96e63. Solo lectura; árbol limpio antes y después. Las líneas siguientes pertenecen a ese commit.

Contrato contrastado: **M2-CLINICS-v1**, `docs/ai/contracts/M2_CLINICS_API.md`, SHA256 `70e096102182fb5d0880997b6ff428e058d756b987c3d8c2238c8743f780e2fd`. La entrega de Argon también cita v0.1 y otros hashes: esta revisión usa el contrato vigente y comprueba el código, sin inferir conformidad del informe previo.

## Hallazgos

1. **P2 — Una respuesta tardía puede sustituir el consultorio de otra ruta.** `src/app/features/clinics/clinic-form-page.component.ts:407–410`. Al guardar 101 y navegar a `/clinics/102/edit` antes de recibir el PATCH, Angular puede reutilizar el componente. `loadForEdit(102)` carga 102, pero el PATCH anterior ejecuta `applyServerClinicToForm(101)` sin comprobar ruta, propietario ni destrucción. La URL/clinicId queda en 102 y el formulario vuelve a 101; el siguiente guardado usa `current.id` y modifica 101. El mismo patrón aparece en cambios de estado y recarga de conflictos (447–453, 485–490). **Corregir:** asociar cada continuación a identidad, recurso y generación de navegación, descartar respuestas obsoletas y cancelar lecturas al destruir/cambiar recurso. No inferir que cancelar la suscripción revierte una escritura ya enviada. Regresión con respuesta diferida de 101 después de cargar 102.

2. **P2 — Un fallo de actualización conserva permisos comerciales anteriores.** `src/app/features/clinics/clinic-form-page.component.ts:517–523`; también `clinics-page.component.ts:174–182` y `clinic-form-page.component.ts:284–286`. Tras un 403/503 de mutación se solicita capabilities; si esa consulta falla, se conserva el objeto VERIFIED anterior y los botones siguen habilitados. Tampoco se controla su `validUntil` al decidir acciones (`clinics.models.ts:275–325`). **Corregir:** ante denegación/invalidez conocida o falta de verificación apta, invalidar acciones locales y permitir revalidación; si se retiene una respuesta anterior durante una falla de transporte, debe seguir vigente y no estar invalidada. Deshabilitar al vencer su vigencia. Un snapshot apto del backend sigue llegando como VERIFIED y debe funcionar durante una caída de Billing; no bloquear por inferir una caída remota. El backend revalida cada mutación: este hallazgo afecta la interfaz, no demuestra un bypass comercial en Go. Regresión 503 de escritura + fallo de refresh, y expiración con reloj controlado.

3. **P2 — Se agregan aliases de compatibilidad para una función inédita.** `src/app/app.routes.ts:32–34`: `/consultorios`, `/consultorios/nuevo`, `/consultorios/:id/editar`. También `hosting/nginx.conf:23–24`, `src/app/core/config/site.config.json:7–9,18–19` y `scripts/static-smoke.test.mjs:13`. Contradice la decisión expresa de conservar únicamente rutas nuevas en inglés. **Corregir:** retirar estos redirects y patrones españoles nuevos; conservar `/clinics`, sus rutas hijas y headers noindex. Ajustar smoke tests e informe de entrega; preservar redirects históricos de funciones anteriores. No crear aliases `/turnos`; `/appointments` continúa reservado.

4. **P3 — El formulario no ofrece reintentar la verificación comercial.** `src/app/features/clinics/clinic-form-page.component.html:134–146` ignora `banner.showRetryButton`, que el modelo activa para UNAVAILABLE. Si Billing no tiene snapshot apto al entrar en alta/edición y luego se recupera, el formulario permanece en lectura sin una acción de revalidación; recargar toda la página puede perder los datos locales. **Corregir:** agregar reintento que refresque capabilities sin reinicializar campos, también ante error de carga de permisos; respetar estados de carga/envío. El listado sí ofrece ese botón.

## Comprobaciones sin hallazgos

| Área | Evidencia / resultado |
|---|---|
| DTO y endpoints | Clinic, requests, capabilities y códigos HTTP coinciden; seis operaciones HTTP correctas sobre `${environment.apiUrl}/clinics`; no ownerId/workspaceId en cuerpos |
| BIGINT | id/version/expectedVersion permanecen strings; BigInt canónico y límite int64, sin conversión a Number en transporte |
| Auth | Interceptor existente envía Bearer al API configurado, retira headers de identidad/secreto y desactiva transferCache; guards existentes de plan/onboarding preservados |
| Flag / mocks | CLINICS_ENABLED=false; rutas y sidebar condicionados; transporte HTTP por defecto; token mock solo inyectado en tests, sin catch/fallback ni reintento POST automático |
| Cuotas / estados | Acciones del servidor y estado del recurso gobiernan UI; ACTIVE+INACTIVE cuentan, ARCHIVED lectura/restauración a INACTIVE; no cantidades por plan inferidas por UI |
| Conflictos | 409 VERSION_CONFLICT conserva entradas y ofrece resolución explícita; falla de contexto asíncrono indicada en hallazgo 1 |
| Separación clínica | Sin cambios a pacientes, relaciones, wizard, historias, calculadoras ni obligación de crear un consultorio; diff de WorkspaceStore solo agrega flag |
| Datos / seguridad | Render mediante interpolación Angular, sin innerHTML ni secretos añadidos; no se acreditó exposición entre propietarios. App recrea la vista por identidad y AccountReadScope protege lecturas iniciales; no confundir esas defensas con protección de todas las continuaciones asíncronas |

## Validación y límites

- Inspección del diff completo y dependencias de auth, guards, configuración, hosting, servicio, componentes, mocks y pruebas; `git diff --exit-code ea4f4d7 --` y status limpios.
- **Reproducciones en memoria PASS:** Node24.20.0 + TypeScript del repo, `transpileModule`/VM, métodos reales del componente con dobles mínimos de Angular/formulario y RxJS real. (a) refresh 503 conserva create=true; (b) PATCH diferido de 101 tras cargar 102 deja clinicId=102 y clinic.id=101. Son pruebas focales de lógica, no pruebas Angular/browser.
- **BIGINT focal PASS:** acepta `9007199254740993`, rechaza `9223372036854775808`.
- No se ejecutaron build ni suite Angular para evitar escribir cachés/artefactos en el repositorio reservado a Argon. No se validó E2E contra Gateway/Billing reales ni se activó el flag. Las pruebas reportadas por Argon no se presentan como reejecutadas por Sol.
- Ningún archivo frontend modificado. Sin merge, push, worktrees, migraciones, despliegue o M3. Correcciones de Argon y revalidación pendientes de autorización; este informe no autoriza integración.
