# Requisitos funcionales vigentes — Doctor Chart
**Esta especificación reemplaza los requisitos de aislamiento clínico por consultorio de la versión 0.2.**

## Historia clínica (no cambiar el ámbito actual)
- Médico tiene una sola cartera clínica de pacientes, independiente del lugar de atención.
- Cada paciente conserva su `patient_id` y todas sus consultas, antecedentes, recetas, estudios, vacunas, cálculos, archivos y borradores aunque la atención sea en consultorios diferentes.
- El wizard y la configuración clínica son por médico. **No se crea consultorio obligatorio después del wizard** para usar historia clínica.
- No duplicar fichas por sede; no asociar forzosamente pacientes a consultorios; no implementar match DNI entre sedes. Un campo opcional de *lugar donde ocurrió una consulta* podría estudiarse como metadato futuro, NO como requisito V1 y sin alterar propiedad de la historia.

## Lugares de atención y agendas
- Médico puede crear múltiples lugares de atención: nombre, país, provincia, ciudad, dirección, zona horaria, estado; edición y archivo sin pérdida de datos.
- Cada lugar tiene sus días de atención, varias franjas por día, duración/intervalos y excepciones por fecha o franja.
- Las disponibilidades/turnos no deben superponerse para el mismo médico **aunque correspondan a diferentes lugares**. Respetar límites de plan.
- El primer lugar se crea cuando el médico decide configurar turnos, no como condición para entrar a funciones clínicas.

## Rutas públicas
- Ruta base confirmada: `/appointments`. No crear alias ni redirecciones para esta funcionalidad nueva.
- Los segmentos de médico y consultorio, sus identificadores públicos y las rutas administrativas son propuestas hasta aprobar el contrato de rutas.
- Interfaz en español; fechas visibles/ingresadas `dd/MM/aaaa` con `app-date-picker`; ISO solo en la frontera de datos.

## Reservas públicas y administración
- Directorio de profesionales por ubicación y especialidad; enlace público individual del médico; selección de lugar de atención.
- Formulario público: nombre completo, documento, teléfono y email del solicitante obligatorios. Extras configurables, obra social opcional por defecto. Diseñar versión inmutable de formulario para reservas históricas.
- Distinguir solicitante y persona atendida, especialmente pediatría. Vínculo clínico opcional por profesional autorizado; ningún paciente/consulta creado al reservar.
- Confirmación automática cuando la reserva es válida. Médico/secretaria autorizada pueden crear, ver, cancelar y marcar llegada/ausencia; un solo motor de turnos y un solo conjunto de reservas.
- El paciente paga presencialmente; V1 sin Mercado Pago ni procesamiento online.
- Resolver concurrencia, idempotencia, cancelaciones, excepciones contra reservas existentes, abusos públicos y trazabilidad.

## Médico y secretaria
- Médico mantiene su login Supabase, sistema de roles y acceso clínico actual; además opera turnos.
- Secretaria: email + activación/contraseña segura (diseño definitivo por aprobar), cuenta y sesión local **solo de Doctor Chart**, nunca Supabase. Panel exclusivo administrativo. Asignaciones por lugar y médico; sin endpoints o DTO clínicos.
- Médico configura/revoca accesos; Billing define capacidades de la cuenta propietaria, no una suscripción de la secretaria.

## Planes e infraestructura
- Billing/Bycode tiene autoridad sobre cuotas y features; Doctor Chart valida transaccionalmente uso local y muestra límites en Angular. No asumir tiers o cantidades.
- Basarse en callback/cola existentes según auditoría; verificar código actual antes de implementar contrato versionado por propietario.
- Migraciones incrementales con instalación desde cero y conservación de datos cuando corresponda. No vaciar bases por inferencia aunque hoy no haya clientes.

## Aún no aprobado
- Semántica exacta de cupos activo/inactivo/archivado, downgrade, snapshot comercial, verificación pública del profesional, detalle de inicio de sesión staff y políticas operativas de planes vencidos: ver `03_PENDING_CONTRACTS.md`.
