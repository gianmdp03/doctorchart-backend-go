# Doctor Chart — M4 motor de reservas
30/10/2026. M1/M2 aprobados e integrados en ensayo. M3 aceptado por el propietario: backend ff5053b, Angular d0b7a45; incluyen M1/M2 completos. M4 autorizado para backend/PostgreSQL y pruebas, un único agente. M5 NO autorizado.

Trabajo en el clon normal C:/Users/gcast/Documents/GitHub/doctorchart-m3/doctorchart-backend-go, rama codex/m4-bookings desde ff5053b. Angular no se modifica. Las ramas M3/turnos y los checkouts habituales main se conservan. Sin worktrees, GitHub remoto, merges, pushes ni despliegues.

Leer [handoff actual](05_HANDOFF.md), [contrato M4-BOOKINGS-v1](contracts/M4_BOOKINGS_API.md) y [entrega M4](M4_DELIVERY.md). M3_DELIVERY.md conserva la evidencia anterior; no repetir auditorías históricas.

Reservas confirmadas, estados administrativos, cancelación, reprogramación, eventos e idempotencia durable. M3 es el único cálculo de horarios; PostgreSQL impide solapamientos del médico entre sedes. Migración incremental 0006; baseline y 0002..0005 inmutables.

Pacientes e historias pertenecen al médico. La reserva puede vincular un paciente propio, nunca crearlo ni modificar su historia; sin patients.clinic_id. Solo BASIC/PRO/UNLIMITED y derechos efectivos M2; sin nuevas cuotas ni cantidades comerciales reales.

BOOKINGS_ENABLED=false y SCHEDULES_ENABLED=false por defecto en Go. CLINICS_ENABLED/SCHEDULES_ENABLED Angular siguen desactivados. Activación, bases reales y publicación requieren autorización independiente. Portal /appointments, secretarias, pagos, notificaciones y calendario Angular M5 quedan pendientes.
