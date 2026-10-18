# UTC y zona horaria del usuario

La zona se detecta desde la configuración del dispositivo mediante Intl; no se infiere del país, idioma ni dirección IP. Se utiliza una zona IANA (por ejemplo Europe/Madrid), que incluye reglas de horario de verano. El formato sigue siendo dd/MM/aaaa y las horas se muestran en 24 horas. No se incorporó un selector manual: el comportamiento aprobado usa el horario del dispositivo.

Los instantes de consultas, recetas y estudios se reciben en UTC y se convierten al presentar. Las fechas de calendario, como nacimiento y vacunación, permanecen como YYYY-MM-DD y no se convierten entre zonas. Los filtros del historial comparan el día presentado; no recortan los primeros diez caracteres del timestamp UTC. Un reloj compartido actualiza el día local y el dashboard al cruzar medianoche, y se libera al destruir el inyector. No arranca timers durante el prerenderizado.

El interceptor añade timeZone a los parámetros de las peticiones dirigidas exclusivamente a la API clínica configurada. No añade cabeceras CORS ni envía esa información a terceros. El gateway actual conserva las queries, por lo que no requiere modificar el proxy. El backend valida la zona antes de usarla; no confía en ella para autorización ni para asignar timestamps.

## Creación y edición

El servidor asigna el instante de creación. Crear una consulta no admite encounterDate del cliente. Los formularios no permiten elegir fecha ni hora. La creación en línea puede mostrar una vista previa, claramente indicada: la fecha definitiva corresponde al instante del guardado en el servidor.

Editar una consulta conserva el instante original. El backend admite que un cliente anterior reenvíe ese mismo instante, pero rechaza cualquier cambio con IMMUTABLE_ENCOUNTER_DATE. También admite omitirlo. La versión, las reglas de consulta y la validación clínica se mantienen. La precisión de creación se ajusta a microsegundos, igual que PostgreSQL/pgx, para devolver exactamente lo persistido.

## Backend y documentos

El backend conserva timestamptz y devuelve ISO/RFC3339 en UTC. Para contar hoy o este mes construye los límites de calendario en la zona validada y los consulta como instantes. Un día puede durar 23 o 25 horas; se usa el siguiente día de calendario, no la suma fija de 24 horas.

Los documentos utilizan una zona de emisión persistida, indicada en el PDF. En consultas se conserva en el contexto guardado; en recetas/órdenes independientes se guarda en patient_prescription_records.issuance_zone. Una reimpresión utiliza esa zona aunque cambie la del dispositivo. Los snapshots de contexto clínico mantienen su referencia y zona originales al editar. Los clientes anteriores que no envían zona conservan el comportamiento argentino; no se desplazan instantes históricos.

## Cambio de esquema y despliegue futuro

El backend es preproducción según su AGENTS.md: la nueva columna está en db/schema.sql y se validó con el mecanismo normal de baseline/fingerprint. No se crearon migraciones incrementales, no se adoptó drift y no se modificó una base existente.

Una base de desarrollo inicializada con el baseline anterior será rechazada por el bootstrap actualizado. Para ejecutar esta versión completa, preparar una base PostgreSQL17 nueva y vacía y apuntar el backend a ella, conservando la anterior. Cualquier descarte/reinicialización de una base existente requiere autorización explícita; no fue ejecutado. No desplegar el nuevo frontend contra un backend antiguo esperando que reconozca las nuevas zonas o bloquee cambios de fechas.

## Verificación

Frontend: npm test -- --watch=false, npm run build, npm run test:seo, npm run test:static, npm run test:config. Para repetir con otra zona, configurar TZ en el proceso de pruebas (UTC, Europe/Madrid o America/Argentina/Buenos_Aires). Los casos que comprueban específicamente el formato argentino declaran esa zona en su fixture; los tests nuevos verifican varias zonas, medianoche y ambos cambios de horario de verano.

Browser QA: node scripts/time-zone-browser-check.mjs, con Edge/Playwright y un build disponible. Usa únicamente sesión ficticia y APIs interceptadas; verifica la misma consulta en Buenos Aires, Madrid y Los Ángeles, la zona enviada y la ausencia de controles para editar la fecha.

Backend: go test ./... -count=1. Las pruebas PostgreSQL son opt-in y deben usar exclusivamente un contenedor descartable propio. BASELINE_TEST_ADMIN_DSN requiere usuario migration_fixture, loopback y un puerto alto permitido; DOCTORCHART_INTEGRITY_TEST_DATABASE_URL requiere doctorchart_integrity_test. La suite de integridad vacía el esquema de esa base de ensayo: nunca apuntarla a una base real/de desarrollo. DOCTORCHART_GROWTH_TEST_DSN usa otra base vacía doctorchart_growth_test del contenedor propio.

Se comprobaron creación y rechazos, UTC de ida/vuelta, edición sin cambio de fecha, contexto de zona, conteos en el límite exacto del día, reimpresión desde otra zona, creación del baseline, reinicios, concurrencia, rollback y detección de drift. El PDF se renderizó y revisó con datos de demostración. Las pruebas contra Supabase/backend publicados y cuentas reales quedan pendientes de disponer de esos servicios.

## Evidencia de esta implementación (14/10/2026)

- Frontend: 649 pruebas pasan; suite completa comprobada en UTC, Madrid y la zona local argentina. Build sin warnings y verificaciones SEO, hosting estático y configuración correctas.
- Navegador Edge: Buenos Aires, Madrid y Los Ángeles pasan con la misma consulta UTC, requests en la zona detectada y fecha no editable. Evidencia: time-zone-browser-results.json. Solo sesión y API ficticias.
- Backend: go test ./... pasa, con pruebas de baseline e integridad PostgreSQL habilitadas en contenedor propio y descartable. TestGrowthHTTPPostgres también pasa en una segunda base de ese contenedor. Las integraciones que requieren servicios externos distintos siguen siendo opt-in.
- PDF de prueba renderizado: fecha 14/10/2026 para el instante 15/10/2026 01:30 UTC y zona America/Argentina/Buenos_Aires visible; sin desbordes.
- Respaldo de src/internal/cmd/db/docs anterior a los cambios: C:/Users/gcast/AppData/Local/Temp/doctorchart-timezone-1792021224. Preserva los cambios previos del usuario. Comparar solamente los archivos del bloque al revertir; no resetear el repositorio ni restaurar una base completa.

No se publicó, no se borró ni se alteró una base existente. El cambio de baseline requiere preparar una base vacía para ejecutar el nuevo backend en desarrollo, según AGENTS.md.
