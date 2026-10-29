# Roadmap revisado — clínica global por médico, ubicaciones solo para turnos
**R0 documental autorizado y entregado, pendiente de aceptación. M1 y las demás fases de implementación no están autorizadas.**

| Fase | Resultado | Criterio clave |
|---|---|---|
| R0 | Documentación instalada desde estado limpio intencional | Entregado: Git registrado, docs persistidas sin commit, cero código/SQL; aceptación pendiente |
| M1 | Migraciones versionadas + `clinics` como **ubicaciones** | BD vacía y upgrade convergen; integridad/conservación; sin cambios clínicos |
| M2 | Integración comercial y administración de lugares | Cuotas de Billing respetadas y serializadas; alta/archivo sin pérdida |
| T0 | Auth local staff + contrato Gateway | Médico Supabase inalterado; secretaria aislada y revocable |
| T1 | Agendas y excepciones | Franja por lugar, sin doble ocupación del médico y sin anular turnos sin aviso |
| T2 | Motor de reservas + formularios versionados | Slot confirmado transaccionalmente, solicitante≠paciente posible, vínculo clínico opcional |
| T3 | Panel turnos médico/staff | Mismo motor y reglas, staff sin datos clínicos |
| T4 | Directorio y reservas públicas | `/appointments`, protección de abuso y pago presencial; segmentos de médico/consultorio pendientes |

Los límites comerciales reales se preparan en Bycode/Billing antes de habilitar M2 para usuarios. Se puede reordenar T0/T1 si los contratos lo requieren; nunca mezclar escrituras de ambos agentes.

## Trabajo expresamente eliminado respecto de versiones 0.1/0.2
- `patients.clinic_id`, backfill de pacientes, creación de sede inicial para migrar registros, aislamiento de clínica por consultorio.
- Revisión transversal de todos los endpoints clínicos para filtro por sede, selectores de contexto clínico, borradores por sede, match de DNI entre sedes.
- Wizard condicionado a crear sede, vistas longitudinales o duplicación de pacientes.

## Validación transversal
- Mantener regresiones clínicas actuales, sin afectar login médico.
- Registrar tests ejecutados y no ejecutados; revisión de Argon; aceptación humana antes de merge/despliegue.

No recuperar el trabajo anterior descartado intencionalmente. La confirmación de `/appointments` no aprueba segmentos anidados ni rutas administrativas.
