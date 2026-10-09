# Informe final de migración DoctorChart

**Nota de continuidad, 2026-10-09:** este informe conserva el cierre histórico de comparación Spring↔Go. El cierre vigente de Angular + Go es **READY FOR STAGING dentro del alcance local probado**, según [el informe actual](../integration/REPORT.md). La aceptación posterior permite diferencias inocuas de PDF y prioriza preservar originales médicos. Se corrigieron además beneficios, PDF Unicode/no-store, almacenamiento y navegación Angular. El antiguo optimizador JPEG y su límite ya no describen el código actual. Los resultados y hashes históricos siguientes permanecen como evidencia de aquella ejecución; no se declara igualdad universal ni se reescriben sus matrices.

**Implementación funcional completa; NOT READY para reemplazo exactamente equivalente.** Se implementaron los **75/75 endpoints y 7/7 procesos de ciclo de vida** del inventario. La fase 3 terminó y la fase 4 se ejecutó en entornos locales aislados. Las diferencias comprobadas y los límites de verificación se detallan abajo; ningún contrato se marca completamente `VERIFIED`.

La referencia es Spring Boot 4.1.1 / Java 25, commit `828691433ac1057d4e7f132685884b5c9cb58e39`. Los 292 archivos registrados conservan sus hashes y el repositorio Spring está limpio. Las compilaciones y pruebas Java se ejecutaron sobre una copia en `.migration/spring`. No se modificaron Spring, bases existentes ni servicios productivos; no hubo despliegue ni cambio de tráfico.

## Alcance implementado

Go usa `net/http`, dependencias explícitas, `pgx`, SQL parametrizado y transacciones sobre el esquema original, sin DDL automático. Hay ocho paquetes internos y 43 archivos Go, incluyendo pruebas. Se conservaron rutas, DTO, validaciones, roles, planes, propiedad de historias, versiones optimistas y efectos transaccionales en los casos comparados.

Los lotes pendientes se completaron en el orden registrado: ocho operaciones de calculadoras con cálculo y redondeo clínico; eliminación de pacientes y dependencias; estudios, almacenamiento local/S3 y cola durable de eliminación; impresión y PDF; inicialización de catálogos y mantenimiento. Se probaron 31 regresiones renales contra la JVM fijada, calendarios inmutables, preservación de catálogos existentes, descargas parciales, rechazo de PDF cifrado y conservación de 100 órdenes impresas.

## Verificación ejecutada

Las evidencias están en [evidence/summary.json](evidence/summary.json), vinculadas por operación en [CONTRACT_MATRIX.csv](CONTRACT_MATRIX.csv). `PASS` significa que pasaron los casos ejecutados, con las excepciones expresas siguientes; no representa una garantía sobre toda entrada posible.

| Control | Resultado | Evidencia |
| --- | --- | --- |
| Formato, compilación, vet, pruebas, race y seguridad Go | PASS: siete controles | [go-quality.log](evidence/go-quality.log) |
| Pruebas Go nativas | PASS: 58; 0 fallos; 1 omitida por symlink no disponible en Windows | [go-tests.jsonl](evidence/go-tests.jsonl); symlink ejecutado en Linux |
| HTTP, permisos, datos y secuencias | PASS: 365/365; los 75 endpoints representados | [compare.json](evidence/compare.json) |
| Esquema PostgreSQL | PASS: 20 tablas, esquema sin cambios en los clones | [compare.json](evidence/compare.json) |
| Rollback y escrituras bidireccionales Spring ↔ Go | PASS: 41/41; Spring reiniciado con Hibernate `validate` | [compare.json](evidence/compare.json) |
| Arranque y configuración | PASS: 9/9 | [lifecycle.json](evidence/lifecycle.json) |
| Estudios, documentos y protocolo S3 | PASS funcional: 14/14; diferencias expresas abajo | [documents.json](evidence/documents.json) |
| Pruebas existentes de Spring | PASS: 663; 0 fallos, errores u omisiones | [spring-tests.json](evidence/spring-tests.json) |
| Backup y restauración local | PASS: dump completo, esquema, datos y secuencias iguales | [restore-runtime.json](evidence/restore-runtime.json) |
| Contenedor Linux y apagado | PASS: usuario 65532, cálculo clínico, petición en curso HTTP 200, SIGTERM con salida 0 y worker finalizado | [restore-runtime.json](evidence/restore-runtime.json) |
| Fuente original intacta | PASS: 292 hashes y Git limpio | [source-preserved.json](evidence/source-preserved.json) |
| Equivalencia exacta de todos los contratos | FAIL: diferencias comprobadas | Matriz siguiente |
| Bases reales, AWS/R2 real y entorno productivo | NOT RUN: fuera del alcance autorizado | Se usaron exclusivamente fixtures locales |

La comparación HTTP verifica estado, JSON, encabezados seleccionados, filas de las 20 tablas y secuencias. Comprueba conflictos concurrentes y rollback SQL. Los trabajos exclusivos se aislaron o coordinaron durante el ensayo de rollback. Los timestamps de auditoría excluidos de igualdad entre procesos están enumerados en el harness; se verificaron ventanas, precisión y fechas clínicas explícitas.

La versión final es **Go 1.27.2**, actualización de seguridad documentada frente a 1.27.1 solicitado por las Skills. `govulncheck` informó **0 vulnerabilidades alcanzables y 0 en paquetes importados**. Hay un aviso de módulo, `GO-2026-5932`, sobre `golang.org/x/crypto/openpgp`, que no se importa ni se llama. No se afirma ausencia de avisos en todas las dependencias transitivas. El detalle está en [vulnerabilities.json](evidence/vulnerabilities.json).

## Diferencias y límites de equivalencia

| Área | Diferencia comprobada / límite | Impacto y evidencia |
| --- | --- | --- |
| PDF | El motor Go genera bytes, metadatos internos y pequeñas diferencias de tipografía/posición respecto de OpenPDF | Se comparó contenido clínico de seis casos, formato A4 y 100 órdenes en cuatro páginas en ambos motores. Se revisó visualmente. La igualdad binaria y visual exacta no pasa. |
| JPEG optimizado | Los codificadores y escaladores nativos producen bytes, `fileSize` y píxeles diferentes | Tres casos: imagen grande, orientación EXIF y original de 48 MP; dimensiones y orientación coinciden. Seis formatos compactos conservan sus bytes. Las métricas de imágenes sintéticas no acreditan calidad diagnóstica. |
| Caché PDF y permisos | Spring devuelve 200 a otro propietario con caché caliente; Go devuelve 403 y valida propiedad siempre | Excepción de seguridad explícita: no se reprodujo la filtración de historias. Tampoco se replica la ventana de documento obsoleto de 30 segundos. Spring quedó intacto. |
| Resolución de entrada | Go limita originales del optimizador a 128 MP y el resultado almacenado a 40 MP | El original de 48 MP se acepta y reduce a 2048 × 1536. La equivalencia para originales superiores a 128 MP no está verificada. |
| Tiempo | Auditoría excluida de igualdad exacta donde la referencia usa su reloj | Ventanas, precisión PostgreSQL, zona horaria y fechas clínicas explícitas probadas; no es una prueba exhaustiva de todos los relojes. |
| Integraciones y datos existentes | S3 se comparó usando clientes reales contra un servidor local de protocolo firmado; datos exclusivamente sintéticos | AWS/R2 real, datos reales y entorno operativo definitivo permanecen sin verificar. |

Revisión PDF, Spring a la izquierda y Go a la derecha: [receta](evidence/pdf-layout-prescription-metadata.png) y [órdenes de consulta](evidence/pdf-layout-encounter-orders.png). Los hashes del código y binario probado están en [go-source-manifest.json](evidence/go-source-manifest.json) y el resumen. Las diferencias no se ocultan mediante normalización general de errores ni sustituciones de endpoints.

## Rendimiento local

Mismos datos clonados y ocho operaciones de lectura/cálculo, 64 solicitudes de calentamiento, 1024 medidas por implementación, concurrencia ocho; cero errores. Ejecuciones secuenciales nativas en Windows. [Resultados completos](evidence/performance.json).

| Medida | Spring | Go |
| --- | ---: | ---: |
| Solicitudes por segundo | 233 | 692 |
| p50 | 24,87 ms | 10,63 ms |
| p95 | 82,99 ms | 16,77 ms |
| p99 | 140,17 ms | 27,46 ms |
| CPU durante la carga | 19,14 s | 1,59 s |
| RSS observado | 498,4 MiB | 26,9 MiB |

Estas mediciones describen ese ensayo local, no capacidad productiva. Los contadores PostgreSQL registrados pueden actualizarse con retraso y no permiten concluir una reducción del trabajo de base de datos.

## Entrega y trabajo restante

No hay operaciones conocidas sin implementar en el inventario. El estado persistente está en [MIGRATION_STATE.md](../MIGRATION_STATE.md) y las instrucciones reproducibles en [README.md](../README.md). Para certificar un reemplazo exacto queda resolver las diferencias o acordar expresamente las excepciones, y verificar el entorno definitivo y sus integraciones cuando exista autorización. Una futura puesta en servicio requiere autorización explícita y el ensayo de rollback en ese entorno. No se ejecutó ningún despliegue.
