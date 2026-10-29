# Handoff — estado real de coordinación
**Versión 0.3 · 29/10/2026 (Argentina).**

## Decisiones del propietario y autorización
- Pacientes y toda su historia pertenecen al médico, sin separación por consultorio ni `patients.clinic_id`. El consultorio organiza lugares, horarios y turnos; no condiciona acceso a clínica.
- Médicos conservan Supabase/Gateway; secretarias usarán autenticación local Doctor Chart; Bycode/Billing conserva autoridad sobre planes y límites.
- Corrección definitiva: ruta pública `/appointments`, sin alias ni redirecciones para esta función todavía no lanzada. Segmentos de médico/consultorio pendientes de contrato; no fijarlos como aprobados.
- El propietario confirmó que descartó intencionalmente los cambios anteriores y preparó repositorios limpios. No recuperar ni reconstruir código o documentación de la arquitectura descartada.
- Autorización expresa de esta conversación: instalar y corregir exclusivamente documentación en `docs/ai/`, actualizar este handoff y preparar M1, preservando `AGENTS.md`.
- **M1 no autorizado.** Tampoco se autorizaron cambios de aplicación, SQL, migraciones, acceso a bases, commits, pushes o despliegues.

## Repositorios verificados
Ubicación común: `C:/Users/gcast/Documents/GitHub/`. Inspección local, sin fetch.

| Repositorio | Rama | HEAD completo | Estado antes de instalar docs |
|---|---|---|---|
| doctorchart-frontend | main | 14196974125e5769c5a95e309896397acad96e63 | Limpio, sin staged/unstaged/untracked ni stashes |
| doctorchart-backend-go | main | c2c30c1142b39519532eb986dcad811fbaa2619b | Limpio, sin staged/unstaged/untracked ni stashes |
| api-ai-gateway-go | main | 55197fa5701d15ed67a69e395bee4e3699c5a705 | Limpio, sin staged/unstaged/untracked ni stashes |
| bycode-backend-go | main | bf65c042cc7480cbf4603447becfeb14002dbde1 | Limpio, sin staged/unstaged/untracked ni stashes |

Tras esta fase el backend contiene solamente archivos nuevos sin stage en `docs/ai/`; los otros repositorios conservan su estado limpio. HEAD no cambia; no existe SHA/PR de esta entrega.
Antes había directorios vacíos en `backend/docs/ai/`, sin documentos. El paquete fuente está en `C:/Users/gcast/Downloads/doctorchart_ai_coordination_v03/doctorchart_ai_coordination_v03/docs/ai/`; la copia instalada y corregida aquí es la referencia de coordinación.

## Hechos técnicos acotados
- `db/schema.sql`: pacientes con `user_id`, sin `clinic_id`; `doctor_workspaces.id` es bigint y su propietario es `user_id`.
- `db/init.go`: solo admite baseline 1 o BD vacía; comprueba identidad, checksum y catálogo. No existe todavía runner de upgrades versionados.
- Se conservan READ COMMITTED, contexto 30s, lock timeout 10s, statement timeout 20s y advisory lock transaccional (17209, 3).
- `AGENTS.md` tiene `production: true`. La excepción del 26/10 sobre nombres de estudios no autoriza cambios de baseline para M1. No se modifica el flag ni se extiende esa excepción.

## Entrega documental y verificación
- Archivos: `00_START_HERE.md` a `10_REPOSITORY_MAP.md`, `NEW_CHAT_SOL_PROMPT.md`, `README.md`; cinco tickets en `tasks/`, dos plantillas en `templates/`, cuatro auditorías y dos archivos de aviso/contexto obsoleto en `sources/`.
- Se corrigieron la incertidumbre sobre el trabajo anterior, la ruta pública, el estado de R0 y la autoridad de los históricos. Los textos históricos pueden contener propuestas descartadas: su aviso inicial y `02_DECISIONS.md` impiden usarlos como instrucciones.
- Comprobaciones: status/diff/staged, HEAD, stashes; lectura de documentación y puntos de esquema/bootstrap; enlaces Markdown relativos, consistencia de rutas/estados y huellas de archivos protegidos.
- No se ejecutaron tests de aplicación o integración: esta fase es exclusivamente documental. No se conectó a bases ni se ejecutaron migraciones.
- Revisión independiente de Argon: **no realizada en esta fase**. No atribuirle una aprobación ni una ejecución de pruebas.
- R0: entrega documental completada, pendiente de aceptación del propietario. Registro detallado en [R0](tasks/R0_CONTEXT_RESET.md).

## Próxima acción
Presentar [M1](tasks/M1_FOUNDATION.md) y pedir aprobación explícita para comenzar. El ticket propone infraestructura versionada y lugares, sin frontend ni cambios clínicos; su ingeniería continúa siendo propuesta.
Sol será implementador principal; Argon revisará el diff entregado en solo lectura. Cada fase siguiente requiere aprobación del propietario.
