# Nombre clínico de los estudios

El nombre es un metadato clínico obligatorio, independiente del nombre del adjunto.
`POST /api/doctorchart/patients/{patientId}/studies` recibe multipart con:

| Campo | Contrato |
| --- | --- |
| `name` | Un único texto no vacío, hasta 240 unidades UTF-16 después de quitar espacios exteriores. |
| `file` | El adjunto con su nombre y extensión; conserva las validaciones de formato, tamaño y conversión existentes. |
| `description` | Texto opcional, hasta 4000 caracteres según el contrato existente. |

El servidor rechaza nombres ausentes (`STUDY_NAME_REQUIRED`), largos
(`STUDY_NAME_TOO_LONG`), duplicados, UTF-8 inválido o caracteres de control/formato
(`INVALID_STUDY_NAME`), con HTTP 400. Comillas, acentos, barras y dos puntos son
válidos en el nombre clínico. El nombre nunca se usa como ruta o nombre físico.

Alta, listado y detalle devuelven `name` y `originalFileName` por separado. La base
guarda el primero en `study_name`. El segundo sigue describiendo el adjunto guardado:
la conversión de imágenes puede ajustar su extensión al formato resultante. La
descarga usa ese nombre de archivo, no el nombre clínico. La propiedad por médico,
los permisos, las transacciones y la gestión privada de archivos se conservan.

## Reconstrucción autorizada del baseline

El propietario autorizó el 26/10/2026 modificar directamente `db/schema.sql`, sin
migraciones, porque todavía no tiene clientes y reconstruirá la base. La excepción
está registrada en `AGENTS.md`; `production=true` se mantiene. El bootstrap crea la
columna y su restricción sobre una base vacía. Los controles de checksum y catálogo
siguen rechazando una base anterior: no hay adaptación ni borrado automático.

El backend nuevo, la reconstrucción y el frontend nuevo deben coordinarse antes de
usar el formulario. El backend exige `name`; el frontend ya no simula el nombre
renombrando el archivo. No se mantiene compatibilidad con el contrato anterior.
La reconstrucción y el despliegue se realizan conforme a `docs/vps-operations.md`;
esta implementación no los ejecuta.

## Estado temporal del formulario

El frontend muestra los registros devueltos por la API. Conserva únicamente inputs
sin guardar en memoria durante la sesión, para volver del detalle a la ficha sin
perder recetas, nuevas consultas o adjuntos pendientes. Cambiar paciente o cuenta
los elimina; recargar o cerrar la aplicación también. No se escriben borradores
clínicos en localStorage, sessionStorage ni IndexedDB.
