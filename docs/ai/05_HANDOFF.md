# Handoff M5 — calendario médico validado
30/10/2026. M4 aceptado (7fe9bd6); M5 completado para aceptación. M6 NO autorizado. Ver [entrega](M5_DELIVERY.md) y [contrato](contracts/M5_CALENDAR_API.md); no repetir auditorías históricas.

Ramas codex/m5-calendar en los clones normales de doctorchart-m3. Backend base M4 7fe9bd6: contrato 9bd148f, aplicación c63f5f3, corrección de aserción de test 25a8c6f. Angular base M3 d0b7a45: aplicación 3f09d8d, controles móviles y hosting 0de8490. Cierres documentales posteriores visibles en HEAD. Main/turnos/M3/M4 anteriores preservados; refs M5 respaldadas localmente en checkouts habituales sin cambiar main.

Angular 747 PASS, build development/production-deployment PASS, hosting/config 8 PASS. Go 516 PASS + 6 externos omitidos documentados; reservas -race 18 PASS, upgrade M4→7 PG18 3 PASS, vet/build PASS. Navegador real con app/guards/auth/interceptores y Gateway/Billing/PostgreSQL descartables: 11 recorridos PASS. Capturas y evidencia en entrega. No JWT fabricados ni credenciales reales.

Migración 0007 añade identidad administrativa opcional de la persona atendida; eventos/recibos previos y datos clínicos intactos. Sin endpoints nuevos o lógica duplicada de horarios. Sin DOWN destructivo; binario M4 rechaza esquema 7 y recuperación real requiere permiso/respaldo/binario compatible.

Flags normales siguen false; no producción, Cloudflare, GitHub remoto, push, merge o despliegue. Fixtures propios retirados, evidencia preservada. Próximo paso: aceptación del propietario; no iniciar M6 ni activar/publicar módulos sin autorización independiente.
