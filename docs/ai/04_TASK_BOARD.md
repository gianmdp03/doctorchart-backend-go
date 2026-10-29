# Tablero — M2-SOL entregado para revisión
**29/10/2026. M1 cerrado/APROBADO; M3 no autorizado.**

| Tarea | Estado | Responsable / siguiente paso |
|---|---|---|
| M1 | CLOSED / ARGON_APPROVED, 6152867 | Rama pendiente de integración controlada |
| Documentación M1/M2 | COMMITTED, 6d7b0d6 | Commit independiente previo a M2 |
| M2-SOL | FROZEN / READY_FOR_ARGON_REVIEW | DoctorChart 1dd09bc; Billing 80c171c; [entrega y hashes completos](reviews/M2_SOL_DELIVERY.md); propietario acepta después de revisión |
| M2-ARGON | CROSS_REVIEW_REQUIRES_CORRECTIONS | ea4f4d7, limpio; [tres hallazgos P2 y una observación P3](reviews/M2_ARGON_CROSS_REVIEW.md); sin cambios de Sol en frontend |
| max_clinics real | OWNER_DECISION_REQUIRED | Solo BASIC/PRO/UNLIMITED; API administrativa verificada; sin clave mutaciones afectadas bloqueadas |
| P2 referido FREE_TRIAL/ENTERPRISE | NON_BLOCKING / OUT_OF_COMMERCIAL_SCOPE | No ampliar Billing; compatibilidad heredada preservada; [pruebas](reviews/M2_SOL_DELIVERY.md) |
| UTC / zonas IANA | VERIFIED / FIX_REQUIRED | UTC funciona; P2 Go/SQL permiten offsets Etc/GMT±N; desajustes Angular documentados |
| Hash contrato M2 | VERIFIED / CANONICAL_RECORDED | v0.1 aa7ff31… frente a v1 70e0961…; DTO/rutas/errores iguales, política snapshot actualizada |
| Listado con timeZone global | P1 / INTEGRATION_BLOCKED | Angular lo agrega; Go responde 400. Propuesto aceptar contexto sin cambiar DTO/rutas |
| Frontend comercial Bycode | BACKLOG / NOT_IMPLEMENTED | Agregar editor max_clinics para BASIC/PRO/UNLIMITED con API existente; sin cantidades ni autorización implícita |
| M3 / staff / horarios / reservas | NO AUTORIZADOS | Sin implementar |

CLAIM por instrucción expresa del propietario, tras verificar estado limpio:
- DoctorChart: Sol, codex/m2-clinics-backend, base 6d7b0d6.
- Billing: Sol, codex/m2-doctorchart-entitlements, base bf65c04.
- Gateway solo leído, main 55197fa; Angular no escrito por Sol. Frontend avanzó externamente a codex/m2-clinics-frontend/ea4f4d7; se preservó sin intervención.

RELEASE de escritura de aplicación backend y Billing tras commits expresamente autorizados 1dd09bc / 80c171c. Implementación congelada para auditoría independiente; documentación de cierre separada. Frontend solo leído en ea4f4d7. Pruebas que escriban en checkout requieren CLAIM temporal y coordinación. No cambiar ramas ni descartar trabajo pendiente. Nunca worktrees, merge/push/despliegue o bases reales.
