# Roadmap vigente —31/10/2026

M1–M6 aprobados. M7 secretarias y administración delegada completado en ensayo, pendiente de aprobación. Siguiente fase M8 (notificaciones) no autorizada ni iniciada. Sin activación comercial ni producción; referencias técnicas en M7_DELIVERY.md.

---

Antecedentes anteriores al cierre M7:

# Roadmap — lugares administrativos, clínica por médico
**M1 CERRADO / ARGON APROBADO en 6152867. M2-SOL implementado para revisión, Angular reservado a Argon; M3 no autorizado.**

# Estado actual — M6
M5 aceptado. M6 portal público y reservas sin cuenta completado y validado de punta a punta en ensayo; [entrega](M6_DELIVERY.md). Capacidad online independiente, sin defaults por plan ni configuración real. Integración/activación/despliegue requieren autorización separada; M7 no autorizado, avisos pertenecen a M8.

## Antecedente M5
# Estado actual — M5
M4 aceptado por el propietario. M5 calendario Angular conectado al motor real, validado con servicios aislados y listo para aceptación; [entrega](M5_DELIVERY.md). Integración a main/activación/publicación requieren autorización separada. M6 y portal/staff/email/pagos/marketing no autorizados.

## Antecedente M4
# Estado actual — M4
M1/M2 aprobados; M3 aceptado (ff5053b backend, d0b7a45 Angular). M4 motor de reservas implementado sobre M3, con pruebas PostgreSQL y autenticación real de ensayo; [entrega y límites](M4_DELIVERY.md). Sin integración a main/producción. M5 calendario/UI, portal público, staff y notificaciones requieren autorizaciones posteriores.

## Antecedente M2 — estados históricos
| Fase | Resultado / estado | Dependencia para avanzar |
|---|---|---|
| R0 | Documentación base en d5a89e0 | Completada |
| M1 | Esquema 2 + clinics, commit final 6152867, APROBADO | Integración controlada pendiente; no merge/despliegue |
| M2-SOL | Corrección congelada: DoctorChart 594402e / Billing 80c171c | Revalidar esquema 4; Argon quita contexto timeZone de consultorios; cuotas reales por configurar |
| M2-ARGON | ea4f4d7 auditado por Sol en solo lectura | Corregir tres P2; observación P3 en informe cruzado; autorización e integración pendientes |
| Integración M2 | Validar API real/UI/derechos y revisión cruzada | Ambos entregan evidencia; propietario autoriza integración/activación |
| T0 | Auth local staff | Contrato propio y permiso; fuera de M2 |
| T1 | Horarios/excepciones por lugar | Sin doble ocupación del médico; fuera de M2 |
| T2 | Reservas/formularios versionados | Motor común; reserva no crea clínica |
| T3–T4 | Panel staff y portal /appointments | Contratos y fases posteriores aprobados |

Ramas backend M2 pueden partir del M1 aprobado sin integrarlo a main; frontend usa su base independiente. Un escritor por Git; revisión cruzada y sin worktrees.
El [contrato único](contracts/M2_CLINICS_API.md) está congelado; M2-SOL autorizado e implementado con snapshot apto durante caída Billing. No extender a M3.
Sin cantidades por tier inventadas, escrituras por derechos desconocidos ni exposición operativa de mocks. Sin cambios clínicos por sede, pacientes duplicados o consultorio obligatorio.
