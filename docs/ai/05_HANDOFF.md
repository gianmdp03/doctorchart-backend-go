# Handoff — correcciones finales M2-SOL
**29/10/2026 (Argentina). Backend listo para revalidación; M2 aún no aprobado para integración. M3 no autorizado.**

## Git y propiedad
- M1 APROBADO por Argon: 615286720f98768dfbc85bcbe35d315e41405e87; sin merge a main.
- DoctorChart: codex/m2-clinics-backend; **corrección congelada 594402ed46536f145b6ebc29aa7badf8a87cc8bb**, nueve archivos. Base ea1c89d; entrega inicial 1dd09bc y documentación previa 6d7b0d6. Cierre de coordinación posterior exclusivamente documental.
- Billing: codex/m2-doctorchart-entitlements, 80c171c86fcae15a7d52868c5960da340bb9d179, limpio/intacto; no necesitó correcciones.
- Frontend: codex/m2-clinics-frontend, HEAD ea4f4d7b217134cfb7e3f9ccd8902dcd4beae898; al cierre aparecen cambios locales externos en interceptor/spec, formulario TS/HTML, mocks y modelos. Preservados, sin escrituras de Sol ni revalidación de esos diffs nuevos. Gateway main 55197fa intacto.
- Main backend sigue en d5a89e0. Sin worktrees, merge, push, despliegue, configuración comercial ni migraciones reales.
CLAIM exclusivo Sol para corrección backend desde árbol limpio; RELEASE de aplicación en 594402e para revalidación independiente. Ningún segundo escritor por Git.

## Contrato y resultado
M2-CLINICS-v1 canónico: **70e096102182fb5d0880997b6ff428e058d756b987c3d8c2238c8743f780e2fd**, bytes del blob Git 9007d55. DTO/endpoints/errores congelados intactos. El SHA aa7ff31… identifica la propuesta v0.1, no otro contrato vigente.
[Correcciones, diff y evidencia](reviews/M2_SOL_CORRECTIONS.md) reemplazan la propuesta anterior de ampliar el listado por timeZone.
- UTC explícito y regiones válidas funcionan. Go y migración incremental 4 rechazan Etc/, incluidos offsets y aliases no regionales. Publicados schema.sql/0002/0003, AGENTS y algoritmo de catálogo intactos; 26 tablas y 21 FK.
- Filas existentes incompatibles (también archivadas) bloquean upgrade con error de conciliación expresa. No se borran, convierten ni alteran: rollback preserva datos clínicos/sedes/secuencia, función/CHECK, historial y marcador anteriores.
- Fallos SQL/catálogo/seed tardío revierten atómicamente. No hay DOWN automático después de confirmar 4; binario previo rechaza versión 4. No iniciar este binario sobre base real sin autorización específica.
- Pacientes/historias siguen por médico, sin clinic_id ni aislamiento clínico por sede. Sin cambios Supabase, horarios, reservas o staff. /appointments reservado.

## P1 pendiente de Argon
El contexto timeZone tiene uso funcional clínico (días/meses, cálculos y documentos), pero consultorios no lo consume. **No ampliar API Go.** Argon debe excluir /clinics y sus rutas hijas del interceptor Angular, mantenerlo en clínica y probar ClinicsService con interceptores reales. Go sigue rechazando ese parámetro extra en listado. Instrucción detallada en correcciones y ticket M2_ARGON; Sol no modifica Angular ni declara el P1 cerrado.
Persisten también los hallazgos propios de la [auditoría frontend](reviews/M2_ARGON_CROSS_REVIEW.md). Alinear validación exacta de nombres con Go/SQL y actualizar el hash declarado por Argon al canónico, sin cambios unilaterales.

## Pruebas y próximos pasos
Go1.27.3: suite completa Windows/PG18.6 PASS (471 casos/subcasos) y suite completa Linux -race/PG17.11 PASS (471), con startup, seeds reales y nuevas regresiones ejecutados sin SKIP. Build/vet/gofmt/diff check PASS. Fallos iniciales de expectativa de historial y fixture Linux corregidos; resultados/omisiones en correcciones.
Seis pruebas externas omitidas explícitamente: interoperabilidad Billing nueva, tres converter/storage live, estudio converter live y sync legacy. No E2E navegador/servicios reales; interoperabilidad previa no se presenta como reejecutada. Fixtures propios etiquetados y helpers retirados tras verificar identidad; fuentes readonly en Linux.
Solo BASIC/PRO/UNLIMITED. FREE_TRIAL/ENTERPRISE no bloqueantes; compatibilidad heredada preservada, sin ampliarlos. max_clinics real sin configurar: no defaults; snapshot apto permite caída de Billing solo hasta vigencia real. Editor de cuota de Bycode frontend sigue en backlog independiente, sin cantidades inventadas.
Siguiente: Argon revalida 594402e y corrige su frontend con autorización propia; prueba conjunta sin mocks y aceptación del propietario antes de integración/activación. No comenzar M3.
