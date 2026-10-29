# Tablero — M2-SOL entregado para revisión
**29/10/2026. M1 cerrado/APROBADO; M3 no autorizado.**

| Tarea | Estado | Responsable / siguiente paso |
|---|---|---|
| M1 | CLOSED / ARGON_APPROVED, 6152867 | Rama pendiente de integración controlada |
| Documentación M1/M2 | COMMITTED, 6d7b0d6 | Commit independiente previo a M2 |
| M2-SOL | CORRECTIONS_FROZEN / READY_FOR_REVIEW | DoctorChart 594402e; Billing 80c171c intacto; [correcciones](reviews/M2_SOL_CORRECTIONS.md); propietario acepta después de revisión |
| M2-ARGON | EXTERNAL_CHANGES_PENDING_REVIEW | HEAD ea4f4d7 revisado; cambios locales externos de interceptor/formulario/modelos/mocks observados al cierre, preservados sin revalidar; sin escrituras de Sol |
| max_clinics real | OWNER_DECISION_REQUIRED | Solo BASIC/PRO/UNLIMITED; API administrativa verificada; sin clave mutaciones afectadas bloqueadas |
| P2 referido FREE_TRIAL/ENTERPRISE | NON_BLOCKING / OUT_OF_COMMERCIAL_SCOPE | No ampliar Billing; compatibilidad heredada preservada; [pruebas](reviews/M2_SOL_DELIVERY.md) |
| UTC / zonas IANA | FIX_IMPLEMENTED / REVIEW_PENDING | Go + migración 4; UTC/regiones válidos; Etc/ rechazado sin alterar filas existentes; pruebas de conservación/rollback |
| Hash contrato M2 | VERIFIED / CANONICAL_RECORDED | v0.1 aa7ff31… frente a v1 70e0961…; DTO/rutas/errores iguales, política snapshot actualizada |
| Listado con timeZone global | P1 / ARGON_CHANGE_REQUIRED | NO ampliar Go: consultorios no usa ese contexto. Argon excluye /clinics y rutas hijas del interceptor; conservarlo para clínica |
| Frontend comercial Bycode | BACKLOG / NOT_IMPLEMENTED | Agregar editor max_clinics para BASIC/PRO/UNLIMITED con API existente; sin cantidades ni autorización implícita |
| M3 / staff / horarios / reservas | NO AUTORIZADOS | Sin implementar |

CLAIM por instrucción expresa del propietario, tras verificar estado limpio:
- DoctorChart: Sol, codex/m2-clinics-backend, base 6d7b0d6.
- Billing: Sol, codex/m2-doctorchart-entitlements, base bf65c04.
- Gateway solo leído, main 55197fa; Angular no escrito por Sol. Frontend avanzó externamente a codex/m2-clinics-frontend/ea4f4d7; se preservó sin intervención.

CLAIM exclusivo de corrección backend: Sol, codex/m2-clinics-backend, base ea1c89d, autorización expresa del propietario tras verificar árbol limpio. Billing/Angular/Gateway sin escrituras. RELEASE de aplicación en 594402e para revalidación independiente; cierre posterior solo documental. Pruebas que escriban en checkout ajeno requieren CLAIM temporal y coordinación. No cambiar ramas ni descartar trabajo pendiente. Nunca worktrees, merge/push/despliegue o bases reales.
