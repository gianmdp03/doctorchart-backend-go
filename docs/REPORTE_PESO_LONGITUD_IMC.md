# Reporte: peso para la longitud e IMC sin puntos

Fecha de verificación: 13/10/2026. Caso analizado: paciente 6, referencia masculina, edad actual de 365 días.

**Estado actualizado: resuelto en la ficha.** Se comprobó en `http://localhost:4202/pacientes/6` que peso/longitud e IMC muestran 13 puntos y 13 evaluaciones, sin panel de posición. La ficha suministra automáticamente la posición estándar a la API cuando falta en el historial. Las tablas siguientes documentan el diagnóstico previo y la comparación que identificó la causa.

## Conclusión

**Los datos de peso y talla están presentes y son válidos. El backend que atiende la URL del túnel todavía exige una posición de medición explícita para calcular peso/longitud e IMC.** Las 13 consultas llegan con `measurementMode: "UNKNOWN"`; por ese motivo, la API devuelve ambos gráficos sin puntos.

No es necesario volver a cargar las historias clínicas ni pedirle al médico otra medida. La ficha aplica ahora el flujo automático solicitado y la API calcula los gráficos con esa posición. La corrección del valor predeterminado también está preparada en el backend local; la respuesta de la API activa sin opciones de vista todavía conserva el comportamiento anterior.

## Evidencia de la API activa

Servidor verificado: `https://legacy-burlington-dairy-combining.trycloudflare.com/api/doctorchart`.

Se realizaron exclusivamente lecturas autenticadas de las consultas y los gráficos del paciente 6. No se modificaron historias clínicas, pacientes, formularios ni tablas.

| Comprobación | Resultado |
| --- | --- |
| Historias clínicas registradas | 13 |
| Observaciones recibidas por el endpoint de crecimiento | 13 |
| Consultas con peso y talla positivos en el mismo registro | 13 de 13 |
| Consultas con una posición explícita en `formValues.heightMeasurementMode` | 0 de 13 |
| Observaciones con `measurementMode: "UNKNOWN"` | 13 de 13 |
| Longitudes dentro del dominio de la referencia de peso/longitud: 45–110 cm | 13 de 13 |
| Cálculos de IMC con resultado finito | 13 de 13 |

No se encontraron faltantes de peso/talla ni incompatibilidad de edad que expliquen que ambos gráficos estén vacíos.

## Prueba que identifica la causa

Se compararon dos lecturas del mismo endpoint:

1. `GET /patients/6/growth`, la lectura habitual de la ficha.
2. El mismo `GET`, indicando `RECUMBENT` en el parámetro de vista `measurementModes` para las observaciones del bebé. Esta opción cambia la evaluación de esa respuesta; no guarda ni modifica datos clínicos.

| Indicador | Lectura habitual: puntos / evaluados / omitidos | Lectura con posición acostado: puntos / evaluados / omitidos |
| --- | --- | --- |
| Peso para la edad | 13 / 13 / 0 | 13 / 13 / 0 |
| Longitud para la edad | 13 / 0 / 0 | 13 / 13 / 0 |
| Perímetro cefálico para la edad | 13 / 13 / 0 | 13 / 13 / 0 |
| Peso para la longitud | **0 / 0 / 13** | **13 / 13 / 0** |
| IMC para la edad | **0 / 0 / 13** | **13 / 13 / 0** |

El único cambio entre las dos lecturas fue la posición usada en el cálculo. Peso, talla, fechas y consultas permanecieron iguales. Esto confirma que el bloqueo es la posición desconocida.

Un detalle adicional: que «Longitud para la edad» muestre 13 puntos no significa que estén evaluados. En la respuesta habitual hay 13 valores dibujables, pero **ninguno tiene evaluación OMS**. El backend activo permite mostrar la talla original, mientras rechaza los indicadores derivados que necesitan normalizarla.

## Qué ocurre en el cálculo

El requisito adicional del backend activo funciona así:

`peso + talla + posición desconocida → no se normaliza la talla → se descarta el punto de IMC y peso/longitud`.

El frontend dibuja las curvas de referencia, pero recibe `points: []` y `omitted: 13` para esos dos indicadores. Por eso aparecen «Sin registros» y cero puntos aunque haya peso y talla en cada consulta. Esos mensajes describen el resultado del filtro, no la ausencia real de mediciones.

El IMC usa `pesoKg / (tallaCm / 100)²`, tomando ambos valores de la misma consulta. Peso/longitud usa la longitud como coordenada horizontal y el peso como coordenada vertical. En este paciente, las 13 consultas contienen esos datos.

## Corrección aplicada y comprobación final

El código local ya incorpora:

- Posición predeterminada acostado hasta el día 730 y de pie desde el día 731, conforme al límite utilizado por el catálogo de crecimiento de la aplicación.
- Respeto de una posición explícitamente documentada cuando exista.
- Cálculo de los indicadores sin preguntar por la posición en la ficha.
- Conservación de las medidas originales y de todo el historial.

La regla está en [ResolveMeasurementMode](../internal/growth/measurement.go), se aplica al construir las observaciones en [el endpoint clínico](../internal/clinical/growth.go) y tiene regresiones de límites de edad e historias sin posición en [growth_test.go](../internal/growth/growth_test.go).

Además de retirar el panel, `PatientGrowthStore` solicita automáticamente una evaluación con las posiciones estándar cuando recibe observaciones con posición desconocida. Solo completa posiciones faltantes; respeta las ya documentadas. Peso/longitud, IMC, percentiles y puntuaciones Z siguen calculándose en la API. No se calculan ni se inventan puntos en Angular.

Se verificó en la aplicación real que el paciente 6 muestra 13 de 13 puntos y 13 evaluaciones en ambos gráficos, sin confirmación de posición. Se agregaron pruebas para posiciones ausentes, posiciones documentadas, errores de evaluación y cambio de paciente durante una lectura pendiente. Pasaron 90 pruebas del frontend.

Cuando el backend activo incorpore el valor predeterminado de `ResolveMeasurementMode`, su primera respuesta ya incluirá las posiciones resueltas y la ficha no necesitará la segunda lectura. La aplicación funciona también con la API actualmente activa; no queda bloqueada esperando esa actualización.

No se requiere cambiar el esquema PostgreSQL, reinicializar la base, borrar datos ni volver a crear pacientes. Esta investigación no realizó un despliegue.
