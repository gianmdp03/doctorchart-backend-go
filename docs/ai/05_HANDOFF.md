# Handoff M6 — portal público validado en ensayo
31/10/2026. M5 aceptado y M6 completado para aceptación técnica. Un único agente. [Entrega, pruebas y límites](M6_DELIVERY.md), [contrato](contracts/M6_PUBLIC_APPOINTMENTS_API.md). No repetir auditorías históricas. M7 no autorizado.

Ramas codex/m6-public-booking en los cuatro clones normales de doctorchart-m3. Aplicaciones finales: DoctorChart 8ccf7c2; Angular 5c45d4e; Billing 6bed61d; Gateway d23924f. Cierres documentales posteriores en HEAD. Bases M5 89a2564/a3dfb0d y dependencias M5 80c171c/55197fa; main/turnos/M3/M4/M5 preservados, refs M6 también conservadas mediante fetch exclusivamente local en checkouts habituales.

Pruebas: DoctorChart 536 PASS, Billing 529 PASS, Gateway 356 PASS, Angular 763 PASS, reservas -race 32 PASS, upgrade PG18 5 PASS, hosting/config 8 PASS, builds/vet PASS. Integración real de aplicación/guards/interceptores/Supabase Auth de ensayo/Gateway/Billing/PostgreSQL: 13 recorridos PASS. Reservas anónimas desktop/móvil, competencia 201/409, formulario versionado, aparición en calendario y aislamiento, Billing caído/revocado, 429 y datos clínicos idénticos. Los 7 usuarios de Auth iniciales no aumentaron. 27 fixtures externos opt-in omitidos, enumerados en evidencia, ninguno nuevo M6 y nunca contados PASS.

Seguridad: IDs públicos independientes; Gateway no acepta identidad administrativa pública. Una sola lease v2 para capacidad online y max_clinics, con fence M2 y motor M4 compartido. Catálogo online_booking_enabled ausente=false, sin cantidades predeterminadas ni nuevas funciones para tiers descartados; v1/legacy conservados. Proof de uso único/SQL atómico, límites compartidos, PII fuera de URLs/cache/logs (stdout y stderr verificados).

Esquema 8 por 0008, 34 tablas/32 FK; SQL previo, reservas y clínica intactos. Rollback transaccional ante fallo, sin DOWN destructivo; M5 no puede abrir marcador 8. Recuperación real requiere permiso, respaldo/restauración o binario compatible; no rebajar marcador.

Flags normales desactivados. Fixtures/servidor propios retirados, evidencia/capturas fuera de Git en .fixtures/m6, sin publicar credenciales sintéticas. Sin GitHub/Cloudflare remoto, merge/push/despliegue o bases reales. Próximo paso: aceptación del propietario; ninguna autorización implícita para publicación comercial, cantidades reales o M7.
