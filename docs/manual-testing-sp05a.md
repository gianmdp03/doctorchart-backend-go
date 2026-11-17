# DoctorChart manual en Windows

Este entorno sirve para recorrer DoctorChart con información ficticia. Usa el mismo laboratorio M6 con PostgreSQL, adjuntos y correo persistentes bajo el proyecto Docker `doctorchart-manual`.

## Empezar

Abrí PowerShell en el repositorio `doctorchart-backend-go` con Docker Desktop iniciado. La primera ejecución necesita Go 1.27.4, Node y las dependencias Angular instaladas en el repositorio vecino. También usa el checkout vecino de API Gateway y el módulo compartido `bycode-image-email-service-go/pkg/media`. Reutiliza la imagen local del conversor `bycode/image-email:local`.

```powershell
node ops/local/manual.mjs open
```

El comando prepara el entorno la primera vez, inicia los servicios y abre [DoctorChart](https://localhost:4354/) en un perfil Chrome separado. Ese perfil acepta el certificado del laboratorio identificado por su clave pública. También podés iniciar desde PowerShell sin abrir Chrome.

```powershell
node ops/local/manual.mjs start
node ops/local/manual.mjs credentials
```

El archivo `%USERPROFILE%\Documents\DoctorChartManual\ACCESOS.txt` contiene las contraseñas generadas para esta instalación. En esa misma carpeta queda el acceso directo `Abrir DoctorChart Manual.cmd`. [Buzón local de Mailpit](http://localhost:18030/)

## Elegir una cuenta

| Cuenta ficticia | Qué probar |
| --- | --- |
| `medicina@manual.local.test` | Médico con 19 contextos. Elegí una especialidad desde **Mis especialidades** y abrí su ficha de ejemplo. En **Personalizar ficha** podés editar el borrador del wizard. |
| `medicina-dos@manual.local.test` | Médico con sus propias fichas en Pediatría y Cardiología. |
| `catalogo@manual.local.test` | Administración del catálogo y sus revisiones. |
| `secretaria@manual.local.test` | Abrí [Secretaría](https://localhost:4354/staff/login). Tiene permiso sobre el Consultorio Centro Ficticio. Norte y Teleconsulta quedan fuera de su delegación. |
| Visitante | Abrí [Reservas](https://localhost:4354/appointments) en una ventana de incógnito de ese perfil o tras cerrar sesión. Buscá **Profesional Ficticio DoctorChart**. Podés ingresar un solicitante distinto de quien será atendido. |

Para alternar médicos o acceder al catálogo, cerrá sesión o usá ventanas con perfiles distintos. Los pacientes son fichas clínicas de ejemplo. La reserva pública admite visitantes sin cuenta.

## Qué quedó cargado

- 19 especialidades. Las tres originales son Pediatría, Medicina General y Dermatología. Las provisionales son Cardiología, Endocrinología, Gastroenterología, Neumonología, Urología, Otorrinolaringología, Ginecología, Alergia e Inmunología, Neurología, Reumatología, Traumatología y Ortopedia, Geriatría, Clínica Médica, Oftalmología, Psiquiatría y Obstetricia.
- Cada contexto del primer médico tiene una ficha y primera consulta de ejemplo. En Pediatría, buscá el DNI ficticio `90000199` para la ficha sin consultas y `90000100` para la ficha con antecedentes, mediciones, seguimiento corregido, consulta anulada, vacuna, receta, orden, PDF emitido y estudio adjunto convertido. Desde **Documentos emitidos y exportación** podés generar otro ZIP. La copia inicial está en `Ficha-ejemplo.zip`.
- **Agenda** reúne Centro, Norte y Teleconsulta. Centro tiene bloques para los 19 contextos distribuidos en la semana. Hay reservas confirmadas pendientes de atención y un ingreso registrado. El producto usa los estados Confirmado e Ingresado para estos escenarios. La publicación ofrece Pediatría presencial y Medicina General presencial o virtual cuando hay bloques compatibles.
- Las 19 decisiones de aprobación son sintéticas y están ligadas a su revisión exacta. Las primeras tres usan el fixture de arranque M6. Las 16 provisionales se cargan por la API administrativa y se aprueban y publican con el revisor de laboratorio.

## Mantener el entorno

| Acción | Comando |
| --- | --- |
| Iniciar o volver a iniciar | `node ops/local/manual.mjs start` |
| Detener y conservar el estado | `node ops/local/manual.mjs stop` |
| Ver disponibilidad de contenedores | `node ops/local/manual.mjs status` |
| Probar el acceso y los ejemplos originales | `node ops/local/manual.mjs verify` |
| Ver logs del backend | `node ops/local/manual.mjs logs backend` |
| Reiniciar sólo el backend | `node ops/local/manual.mjs restart backend` |
| Recompilar tras editar los repositorios | `node ops/local/manual.mjs build` y luego `node ops/local/manual.mjs start` |
| Restaurar los datos de ejemplo | `node ops/local/manual.mjs reset --confirm-doctorchart-manual` |

Para los comandos por servicio usá `web`, `gateway`, `backend`, `pg18`, `support`, `converter` o `mailpit`. El reset elimina el estado clínico y las personalizaciones de este entorno únicamente. Verifica el proyecto, volumen, marcador sintético y baseline antes de recrear la base. Conserva las cuentas, el buzón y una carpeta con los archivos clínicos anteriores. Las fechas de los turnos de muestra se calculan al preparar o restablecer los datos. Los bloques semanales siguen disponibles cuando pasa esa fecha.

La web Angular, el gateway y el backend son reales. El adaptador local emite JWT firmados y permisos comerciales de prueba. La teleconsulta permite configurar horarios y reservar esta modalidad. El laboratorio utiliza Mailpit para SMTP y el conversor existente para imágenes de estudios. Las videollamadas, los cobros, la autenticación de cuentas reales, la firma digital y la entrega institucional quedan fuera de este entorno.

La web y el buzón se publican sólo en loopback. PostgreSQL, Auth/Billing, gateway, API y conversor quedan en la red interna. El certificado y las credenciales son nuevos y locales. El baseline de la base manual es 20 sobre PostgreSQL 18. Los clústeres locales anteriores de PostgreSQL 17 se encontraron detenidos. El lanzador anterior espera baseline 16, pero el marcador efectivo de esas bases no se consultó para mantenerlas detenidas.

## Verificaciones del 17/11/2026

- Go 1.27.4 y Node 26.13.0 en Windows. Compilaron backend, gateway y Angular de laboratorio. PostgreSQL 18.6 informó baseline 20. El laboratorio mantiene los mismos binarios al detener y arrancar normalmente.
- En Chrome, el recorrido focal verificó ambos accesos médicos, selector, personalización, panel del catálogo, ficha con documentos y exportación, agenda delegada y confirmación de reserva pública con un solicitante distinto del paciente. La prueba bloqueó solicitudes del navegador a orígenes distintos del laboratorio. Mailpit recibió la confirmación por SMTP.
- Se verificaron por API los rechazos de acceso fuera del ámbito de cada médico, del catálogo y de secretaría. También se verificaron la agenda virtual, la vacunación, la corrección y la anulación, la auditoría, el ZIP y la integridad del PDF y el adjunto.
- Se detuvieron todos los servicios y se volvieron a iniciar. Coincidieron el inventario, el SHA-256 del ZIP y el correo de confirmación. Después de reiniciar sólo el backend, la API volvió a entregar el PDF y el estudio con sus hashes originales.
- El restablecimiento se probó al reconstruir el fixture inicial tras corregir una solicitud de receta incompleta. La ejecución sin la confirmación explícita fue rechazada. El primer arranque con una sola red Docker interna no publicaba los puertos en Windows. La web y Mailpit quedaron conectados también al acceso de loopback y se verificaron ambos servicios.

El estado entregado contiene 19 especialidades, 19 aprobaciones sintéticas, 22 fichas, 21 consultas incluidas las anuladas, tres consultorios, cuatro reservas, una vacuna, tres revisiones, un estudio, una emisión y una evidencia de exportación. Los archivos `browser-report.json` y `persistence-report.json` en `DoctorChartManual` conservan los resultados.

## Inventario Git al cierre

| Repositorio | Rama y revisión | Archivos rastreados con cambios | Archivos nuevos |
| --- | --- | ---: | ---: |
| DoctorChart Go | `main` · `1763153` | 70 | 59 |
| DoctorChart Angular | `main` · `90d10ec` | 164 | 41 |
| API Gateway | `main` · `ec878ce` | 9 | 0 |
| Conversor | `main` · `7b5f649` | 0 | 0 |

No había archivos preparados para commit. Quedan por consolidar los cambios de producto y las pruebas y documentación de las etapas anteriores. SP-05A agrega la operación manual, sus ejemplos y la configuración Angular dedicada. Los scripts y plantillas requeridos aparecen en Git como archivos rastreados o nuevos. La configuración Angular generada, las credenciales, la base, el almacenamiento y los artefactos de prueba son locales. El único documento ignorado en las rutas de código y documentación revisadas fue un resultado de validación del Gateway, ajeno al arranque.
