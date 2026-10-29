# M2-ARGON — frontend Angular de administración
**READY_FOR_APPROVAL / NO IMPLEMENTAR TODAVÍA.**
Repo: `doctorchart-frontend`. Base observada: `14196974125e5769c5a95e309896397acad96e63`, main limpio. Rama propuesta: `codex/m2-clinics-frontend`.
Responsable: Gemini Argon. Revisor cruzado: Sol. Contrato único: [M2-CLINICS-v0.1](../contracts/M2_CLINICS_API.md), aún propuesto.

## Dependencias y propiedad
Requiere aprobar contrato/ticket, no terminar el backend. Puede implementar contra mocks del contrato congelado. CLAIM exclusivo frontend según [protocolo](../06_PROTOCOL.md); sin worktrees ni escritura en backend.
Leer contrato real en el repo hermano `doctorchart-backend-go/docs/ai/` en solo lectura; no mantener una copia divergente.
Ámbito propuesto: `src/app/features/clinics/` (componentes, servicio/modelos y tests), integración mínima en `src/app/app.routes.ts`, navegación de `WorkspaceStore` y configuración de flag existente; hosting/noindex solo si la nueva ruta lo exige.
Entrega propia en `frontend/docs/ai/M2_ARGON_DELIVERY.md` o mensaje al coordinador: evidencia única de su tarea, sin duplicar el contrato ni editar handoff/backend mientras Sol escribe allí.

## Alcance
- Lista paginada/estados, formulario crear/editar y acciones inactivar/activar/archivar/restaurar según capabilities y versión.
- Rutas propuestas de gestión `/clinics`, `/clinics/new`, `/clinics/:id/edit`, UI en español y accesible. No son las rutas del portal `/appointments`.
- Tipos y mocks del contrato; estados vacío/carga/error/solo lectura, cuota alcanzada y Billing inaccesible.
- Reutilizar auth/interceptor, shell y formularios actuales. No exigir consultorio para historias o wizard.
- Flag de nuevas rutas/nav inicialmente apagado. Mocks solo tests/desarrollo explícito, sin fallback productivo. Botones de escritura exigen actions efectivos del servidor; no inferir permiso por plan, ausencia de límite o caché.
- Tratar 400 por campo, 401/403 existentes, 404, 409 de versión/cuota/estado y 503; no perder cambios ante conflicto ni mostrar éxito ficticio.

## Exclusiones
Backend/PostgreSQL/Billing/Gateway/Supabase; inventar cuotas; horarios/reservas/staff/portal público; crear selector de contexto clínico por sede. No reactivar calculadoras.
Fechas visibles argentinas y `app-date-picker` si aparece un selector; no agregar inputs date nativos. No introducir campos fuera del contrato.
Sin merge/push/despliegue ni operación comercial real por el solo hecho de aprobar desarrollo UI.

## Pruebas y aceptación
- [ ] Contrato/ticket aprobados y CLAIM frontend registrado.
- [ ] Servicio y mocks respetan shapes, strings BIGINT, rutas, expectedVersion, errores y paginación.
- [ ] Componentes validan campos y estados; bloqueo de todas las escrituras con rights desconocidos/no integrados/caídos; navegación operativa no expuesta con flag apagado.
- [ ] Conflictos y errores refrescan permisos de forma segura; mocks nunca sustituyen respuestas fallidas en producción.
- [ ] Pruebas frontend focales, build/SSR y regresiones de navegación pertinentes; sin afectar clínica ni autenticación.
- [ ] Entrega marca SIMULADO vs API REAL; no declarar aceptación E2E/comercial solo con mocks.
- [ ] Sol revisó diff congelado en solo lectura; integración futura valida ambos lados del contrato con permiso del propietario.

## Entrega
Base/rama/SHA o diff identificable, capturas si ayudan, pruebas/resultados, escenarios simulados y pendientes. Enviar evidencia al coordinador para actualizar tablero/handoff canónico; Argon no escribe el Git backend en paralelo. Si Sol requiere ejecutar pruebas frontend que escriban archivos, Argon primero entrega RELEASE.
