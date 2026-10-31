# Doctor Chart — M6 reservas públicas
31/10/2026. M5 aceptado (backend 89a2564 / Angular a3dfb0d); M6 implementado y validado en ensayo, pendiente aceptación del propietario. Un único agente; M7 NO autorizado.

Leer [entrega M6](M6_DELIVERY.md), [contrato público](contracts/M6_PUBLIC_APPOINTMENTS_API.md) y [handoff](05_HANDOFF.md). M1–M5 completos y preservados; informes anteriores son antecedentes, no instrucciones actuales de detener M6 o repartir trabajo con Argon.

Clones normales en C:/Users/gcast/Documents/GitHub/doctorchart-m3/{doctorchart-backend-go,doctorchart-frontend,bycode-backend-go,api-ai-gateway-go}, ramas codex/m6-public-booking. Desde las bases exactas M5 y sus dependencias congeladas. Las cinco main habituales, turnos y ramas previas permanecen intactas. Sin worktrees, GitHub remoto, merges, pushes o despliegues.

/appointments permite buscar profesionales publicados, elegir sede/slot M3 y confirmar mediante el mismo motor M4, sin cuenta ni historia clínica nueva. Publicación privada y formularios administrativos versionados; IDs públicos opacos, confirmación sin documento/email/teléfono. Capacidad comercial independiente online_booking_enabled, ausente=false, solo BASIC/PRO/UNLIMITED, sin cuotas/precios nuevos. Contrato firmado v2 compatible con v1; una única lease v2 para toda mutación pública.

Migración 0008 incremental, esquema 8, baseline/0002..0007 inmutables. PUBLIC_BOOKINGS_ENABLED y PUBLIC_APPOINTMENTS_ENABLED siguen false en código normal. Sin pacientes por sede, cambios clínicos automáticos, producción, datos reales, staff, notificaciones/M8, pagos o M7. La aceptación no autoriza activar/publicar ni integrar a main.
