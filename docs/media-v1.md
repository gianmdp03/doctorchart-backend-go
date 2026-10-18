# Integración de medios v1

DoctorChart conserva autorización clínica y descargas privadas. Catálogo/outbox se escriben junto al estudio, con UUID estable del paciente. No se borran huérfanos por ausenciaSQL; las intenciones explícitas respetan protección y recuperación.

Contrato, configuración canónica, límites de recuperación y procedimiento operativo: [arquitectura compartida](../../bycode-image-email-service-go/docs/media-v1.md), [operación y restauración](../../bycode-image-email-service-go/docs/media-operations.md), [ejemplo de variables](../../bycode-image-email-service-go/pkg/media/.env.example).

Archivos de esta integración: cmd/api/main.go, internal/storage/store.go, internal/clinical/studies.go, internal/clinical/media_integration_test.go, db/schema.sql, db/init_test.go, internal/bootstrap/schema_test.go, go.mod, go.sum, Dockerfile, .dockerignore.

No hubo deploy, push ni cambios sobre bases existentes. Los backends usan baselines preproducción nuevos; no arrancar el binario nuevo sobre un esquema anterior ni ignorar su fingerprint. Si hay datos/archivos que conservar, preparar inventario y copia transformada verificable antes de habilitarlo; no está autorizado descartarlos. La guía compartida describe rollback y recuperación.

Los assets estáticos de marca, SEO y marketing permanecen como assets del despliegue, fuera del catálogo de archivos subidos por usuarios. Las URLs externas no se importan automáticamente.
