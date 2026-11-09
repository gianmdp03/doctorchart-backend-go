# Handoff — M8 listo para aprobación

31/10/2026. Propietario aprobó M7; M8 completado en ensayo, pendiente de aceptación. M9 no iniciado.

Ramas codex/m8-notifications en C:/Users/gcast/Documents/GitHub/doctorchart-m3. Backend aplicación68a4993+pruebase716623; Angulardeee309; Gateway6586a72. Billing69febba intacto, en rama anterior. Cierres documentales posteriores enHEAD. Contrato previo0dc3710.

[Informe M8](M8_DELIVERY.md) y [contrato](contracts/M8_NOTIFICATIONS_API.md): endpoints, config, seguridad, pruebas y límites. DoctorChart575PASS +2regresiones finales-race; Gateway358; Notifications-race17; PG18migration5; Angular773; hosting8; navegador/serviciosreales9+3dependencias.21pruebasexternas omitidas, noPASS.

SMTPSTARTTLS real a Mailpit aislado: confirmación/cancelación/reprogramación/recordatorio e invitación/recuperaciónstaff. Outbox cifrado durable atómico conM4, lease/SKIPLOCKED,2workers,reintentos/reinicio; proveedor caído no revierte reserva. Consulta pública POSTconfirmation por código256bits, bodyonly, limitado/expirable/revocable. Sin clínica ni búsquedas porPII. M7personalrecovery/cookies/CSRF/médicoSupabase intactos.

Límites: SMTP no garantiza exactly-once físico ante ACKperdido; Message-IDestable. No retira correo ya aceptado o cambiado después del chequeo final. Sin backfillmasivo/emailde pacienteclínico. Sin cancelaciónpública. ClaveNOTIFICATIONS_KEY estable necesaria para reinicio/restauración; no rotaciónautomática. Editorcomercial pendiente M9.

NOTIFICATIONS_ENABLED=false y flags anteriores sin activar normalmente. Entornoensayo HTTPSloopback y Mailpit con certificadoverificado; credencialessintéticas. Recursos propios retirados al cierre con ownership, evidencia fuera deGit en .fixtures/m8/verification-summary.json y integration/capturas.

Mainhabituales limpias/sin cambios M8 realizados aquí: DoctorChartGo d5a89e0; Angular1419697; Billingbf65c04; Bycodefrontend8ac5b14; Gateway55197fa. Bycodefrontend recibió otro commitajeno8ac5b14 (31/10 12:15-03:00), descendientede89f032c; ambos preservados sin incorporar. NuevasrefsM8 respaldadas mediante fetchLOCAL sin cambiarcheckout. Sin GitHubremoto, push, merge, worktrees, despliegue, migracionesreales o M9.
