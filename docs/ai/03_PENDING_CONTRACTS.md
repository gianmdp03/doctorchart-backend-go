# Pendientes tras M2-SOL
M1 cerrado/APROBADO en 6152867. M2-SOL implementado; revisión independiente y aceptación pendientes. M3 no autorizado.

- **Bloqueo comercial acotado:** propietario debe configurar max_clinics explícitamente en Billing para cada tier real. No se inventan cantidades ni se copian cupos clínicos. Sin clave, trial sin catálogo apto o cobertura no verificable: capabilities UNAVAILABLE y mutaciones 503; lecturas locales seguras.
- **Resuelto técnicamente:** [API M2-CLINICS-v1](contracts/M2_CLINICS_API.md) congelada, endpoint interno firmado de Billing y [snapshots](contracts/M2_EFFECTIVE_ENTITLEMENTS.md). Snapshot apto permite operar con Billing caído solo hasta su vencimiento original. Sin copia apta, bloqueo.
- Catálogo legacy conserva sus dos claves; nuevos derechos llegan por propietario, con cuota opcional administrada. Cambios de cobertura/catalogo notifican por outbox/callback existente.
- No se promete revocación instantánea: callback recibido invalida entre réplicas; evento todavía no recibido o modificación manual sin callback queda acotado a lease máximo de 60s, jamás más allá de cobertura.
- Pendientes de autorización independiente: M2-ARGON/Angular, commits de aplicación, integración, merge/push/despliegue/configuración real. Revisar [entrega](reviews/M2_SOL_DELIVERY.md).
- Staff, horarios, reservas y portal público requieren fases futuras; /appointments sin alias y sin segmentos adicionales definidos aquí. Pacientes/historias permanecen por médico.
