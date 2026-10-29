# Doctor Chart — inicio breve
**Versión 0.3 · 29/10/2026 · fase documental autorizada; M1 pendiente de aprobación.**

## Producto confirmado
- Pacientes e historias clínicas pertenecen al médico. Un mismo paciente conserva su historia al atenderse en distintos consultorios.
- Consultorios = lugares de atención para ubicaciones, horarios y turnos. No agregar `patients.clinic_id`, `clinic_patients`, copias ni filtros clínicos por sede.
- Se puede usar la clínica y completar el wizard sin crear un consultorio. Un lugar es necesario solo para configurar agenda/reservas.
- Médicos conservan Supabase/Gateway. Secretarias tendrán autenticación local en Doctor Chart y acceso administrativo, sin historia clínica.
- Bycode/Billing administra planes y límites; Doctor Chart los aplica sin inventar cantidades por tier.
- Ruta pública confirmada: `/appointments`, sin alias ni redirecciones de compatibilidad para esta funcionalidad nueva. Segmentos de médico y consultorio: propuestas hasta aprobar el contrato de rutas.
- Agendas por lugar, sin reservas simultáneas incompatibles para el mismo médico. Reservar no crea paciente ni historia.

## Estado real
El propietario descartó intencionalmente el trabajo anterior y preparó repositorios limpios. Verificado localmente: frontend `1419697`, backend `c2c30c1`, Gateway `55197fa`, Billing `bf65c04`. **No recuperar ni reconstruir lo descartado.**
Esta entrega agrega únicamente documentación en `doctorchart-backend-go/docs/ai/`; no modifica aplicación, bases ni `AGENTS.md`. No hay commit documental todavía.

## Próximo paso
Revisar [decisiones](02_DECISIONS.md), [handoff](05_HANDOFF.md) y [ticket M1](tasks/M1_FOUNDATION.md). **Solicitar aprobación antes de comenzar M1.**
Sol implementa; Argon audita independientemente en solo lectura; el propietario aprueba cada fase. Rigen los `AGENTS.md` reales, incluido `production: true` del backend.
No leer auditorías históricas para reiniciar: [sources/](sources/README.md) es solo antecedente, nunca instrucciones actuales.
