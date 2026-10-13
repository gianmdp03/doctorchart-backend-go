# DoctorChart → conversor compartido

Se retiraron `internal/storage/optimizer.go` y su codificación/escalado local. La etapa de procesamiento ahora hace un POST multipart al servicio `syntah-image-converter-go`. El backend mantiene validación de archivos y almacenamiento local/S3/R2, sin implementar una segunda conversión.

## Configurar ambos procesos

Para probar compresión con datos sintéticos, copiar `syntah-image-converter-go/integration/doctorchart.env.example` a un archivo privado del conversor. Ese ejemplo define `doctorchart-study` como transformación WebP, calidad 85, fit contain, sin ampliación y máximo 2048 px. Sus límites de entrada están alineados con los 10 MB (10.000.000 bytes) por archivo y 40 MP de DoctorChart. `MAX_FILE_BYTES=10000000` y `MAX_BODY_BYTES=10065536` permiten ese archivo más el multipart. Un original mayor devuelve 413 antes de conversión y guardado. Los valores de calidad y dimensiones solo pertenecen al conversor.

Generar una clave independiente, guardarla como `APP_DOCTORCHART_API_KEY` en ese archivo y como `IMAGE_CONVERTER_API_KEY` en el `.env` de DoctorChart. La credencial de Pointly del ejemplo debe ser otra clave. No usar valores de ejemplo ni reutilizar claves de Gateway o Billing.

En DoctorChart:

Estos valores corresponden a procesos nativos en el mismo host. En una red privada de contenedores usar el DNS del servicio y su puerto interno, por ejemplo `http://image-converter:8080`; publicar el conversor en `127.0.0.1:8086` evita colisionar con Pointly nativo en `8080`. Cada despliegue configura sus propias URLs; loopback no comunica contenedores diferentes.

```ini
IMAGE_CONVERTER_URL=http://127.0.0.1:8086
IMAGE_CONVERTER_API_KEY=<la-misma-clave-de-APP_DOCTORCHART_API_KEY>
STUDIES_ALLOW_IMAGE_TRANSFORM=true
```

En una terminal, desde el repositorio del conversor:

```powershell
go run -tags nodynamic ./cmd/converter -env-file doctorchart.env
```

En otra terminal, desde DoctorChart, con su configuración habitual de PostgreSQL, Gateway, Billing y almacenamiento:

```powershell
go run ./cmd/api
```

Angular continúa usando `POST /api/doctorchart/patients/{patientId}/studies` a través del Gateway. No recibe la credencial del conversor ni cambia su endpoint. El perfil enviado por el backend siempre es `doctorchart-study`; un campo `profile` enviado desde Angular no lo modifica. Tampoco se reenvían opciones de formato, calidad o dimensiones, identidad del paciente, bearer del usuario ni secreto de Gateway. El archivo se envía con un nombre técnico genérico.

Para conservación, usar `STUDIES_ALLOW_IMAGE_TRANSFORM=false` y un perfil del conversor con `MODE=preserve-original`, `FORMAT=auto`, `METADATA=preserve` y dimensiones compatibles. Su `.env.example` principal conserva esa política inicial. Una respuesta transformada cuando el backend exige conservación se rechaza, sin guardado. No se infiere aptitud de transformación por apariencia o por el nombre de un estudio.

Los JPEG, PNG y WebP válidos se envían incluso cuando son pequeños. Si el resultado es mayor o igual, se mantiene el original para respetar el objetivo de ahorro. PDF, GIF, BMP y TIFF se validan y guardan mediante la ruta original porque el servicio no implementa esos formatos. Las respuestas `415 special_image` y `415 animated_image` conservan el archivo ya validado, mediante una política explícita. No se conserva silenciosamente el original ante `401`, `403`, límites, archivos corruptos, errores de red o fallos internos del conversor.

El cliente tiene timeout de 160 segundos para incluir la cola, propaga el contexto, limita la respuesta a 25 MiB y rechaza redirecciones. Comprueba perfil, tipo real, dimensiones y el indicador de conservación; en conservación exige igualdad de bytes. No expone errores internos o credenciales al navegador. Configuración parcial, URL inválida, token inválido o reutilización de claves de Gateway/Billing se rechazan al iniciar. Sin conexión configurada, las nuevas cargas raster devuelven `IMAGE_CONVERTER_UNAVAILABLE` (503).

El handler comprueba rol, propiedad del paciente y habilitación de estudios antes de llamar al almacenamiento/conversor. La primera transacción de autorización termina antes de la llamada externa, para que esperar en cola no ocupe conexiones de PostgreSQL. Después de confirmar el guardado se abre una transacción corta, se revalida la autorización y se inserta el registro. Si cambió el permiso o falla la inserción, se elimina el objeto guardado mediante la limpieza existente. No hay reintentos automáticos de conversión, cambios de esquema productivo ni recomprensión de objetos anteriores.

El conversor tiene una plaza activa, 20 lugares de espera FIFO y 90 segundos máximos en cola. DoctorChart reserva una de sus 21 plazas antes de leer/spoolar el multipart y valida hasta dos imágenes a la vez; las plazas de validación no se retienen durante la llamada al conversor. Su endpoint tiene un presupuesto total de 175 segundos y un margen de escritura de 180 segundos. El Gateway Go usa `STUDY_UPLOAD_TIMEOUT_SECONDS=180` únicamente para `POST /api/doctorchart/patients/{id}/studies`; otros endpoints conservan su presupuesto habitual. Alinear cualquier proxy frontal con estos tiempos antes de desplegar.

Angular mantiene el indicador “Subiendo y procesando estudio…” y los controles deshabilitados mientras la respuesta está pendiente; no inicia otra carga ni informa éxito antes del resultado. El máximo de selección y subida es 10 MB. La cola es por instancia y no persiste al reiniciar. Ver [mediciones y configuración de la cola](../../syntah-image-converter-go/docs/capacity.md).

## Prueba reproducible y aislada

Desde el repositorio del conversor:

```powershell
./integration/verify_doctorchart.ps1
```

Requiere PowerShell 7, Go y Docker. Construye el conversor, genera credenciales aleatorias temporales y crea contenedores propios de conversor y PostgreSQL 18 con puertos dinámicos en loopback. No usa la configuración privada de DoctorChart ni una base existente. S3/R2 se prueba con un servidor HTTP local que verifica el protocolo de guardado; no accede a un bucket real. Finalmente detiene sus dos contenedores y elimina sus archivos temporales. Se puede indicar `-BackendDirectory` para otra ubicación del repositorio.

La prueba ejecuta almacenamiento local y S3, formato WebP, alfa, orientación EXIF, conservación de 16 bits, credencial de Pointly sin permiso para estudios, y el endpoint real de estudios con PostgreSQL descartable. Este último comprueba rechazo de usuarios/roles no autorizados antes de cualquier llamada externa, perfil fijado por el backend, fallos de conversión/guardado sin registros y limpieza del objeto ante rollback de la inserción.

## Evidencia de esta implementación

Ejecutado el 2026-10-09 con Go 1.27.2, el conversor compilado con `nodynamic` y PostgreSQL 18:

| Caso sintético | Resultado |
| --- | --- |
| PNG opaco 2400×1000 | 7.194.692 → 1.131.068 bytes; WebP 2048×853 |
| PNG con alfa 2400×1000 | 9.605.316 → 2.845.154 bytes; WebP 2048×853 con alfa |
| JPEG con orientación EXIF 6 | WebP 682×2048 orientado por el servicio |
| PNG de 16 bits | Conservación exacta del original |
| Credencial Pointly para estudios | Rechazo; ningún objeto S3 nuevo |
| Endpoint + PostgreSQL + almacenamiento | Autorización, guardado, metadatos y rollback verificados |

La compresión se probó con fixtures sintéticas; no acredita calidad diagnóstica. Las evidencias anteriores de integración/migración describen políticas previas. Esta prueba reemplaza específicamente la optimización local para nuevas cargas, sin modificar los otros servicios ni frontend.
