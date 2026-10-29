# Roadmap revisado — clínica global por médico, ubicaciones solo para turnos
**R0 commiteado por autorización expresa. M1 implementado, pruebas PASS, pendiente de auditoría Argon y aceptación. M2 y demás fases no autorizadas.**

| Fase | Resultado | Criterio clave |
|---|---|---|
| R0 | Documentación instalada desde estado limpio intencional | 26 documentos en d5a89e0; cero código/SQL en R0 |
| M1 | Implementado en 48f87b1; pendiente de review | Vacío/upgrade convergen en PG17/18; clínica intacta; aprobación final pendiente |
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
