# Doctor Chart — inicio breve
**Versión 0.3 · 29/10/2026 · M1 implementado; pendiente de auditoría Argon y aceptación.**

## Producto confirmado
- Pacientes e historias clínicas pertenecen al médico. Un mismo paciente conserva su historia al atenderse en distintos consultorios.
- Consultorios = lugares de atención para ubicaciones, horarios y turnos. No agregar `patients.clinic_id`, `clinic_patients`, copias ni filtros clínicos por sede.
- Se puede usar la clínica y completar el wizard sin crear un consultorio. Un lugar es necesario solo para configurar agenda/reservas.
- Médicos conservan Supabase/Gateway. Secretarias tendrán autenticación local en Doctor Chart y acceso administrativo, sin historia clínica.
- Bycode/Billing administra planes y límites; Doctor Chart los aplica sin inventar cantidades por tier.
- Ruta pública confirmada: `/appointments`, sin alias ni redirecciones de compatibilidad para esta funcionalidad nueva. Segmentos de médico y consultorio: propuestas hasta aprobar el contrato de rutas.
- Agendas por lugar, sin reservas simultáneas incompatibles para el mismo médico. Reservar no crea paciente ni historia.

## Estado real
El propietario descartó intencionalmente el trabajo anterior y preparó repositorios limpios. Bases iniciales verificadas: frontend `1419697`, backend `c2c30c1`, Gateway `55197fa`, Billing `bf65c04`. **No recuperar ni reconstruir lo descartado.**
Documentación inicial: `d5a89e0` (26 archivos). M1: `48f87b1`, rama `codex/m1-clinics-migrations`: migración 1 → 2 y tabla `clinics`, sin cambiar clínica, baseline ni `AGENTS.md`. Pruebas en PostgreSQL 17/18 descartables; sin producción ni despliegue.

## Próximo paso
Revisar [handoff](05_HANDOFF.md) y [resumen para Argon](reviews/M1_ARGON_BRIEF.md). **M1 requiere revisión independiente y aceptación del propietario. No iniciar M2 sin aprobación.**
Sol implementa; Argon audita independientemente en solo lectura; el propietario aprueba cada fase. Rigen los `AGENTS.md` reales, incluido `production: true` del backend.
No leer auditorías históricas para reiniciar: [sources/](sources/README.md) es solo antecedente, nunca instrucciones actuales.
