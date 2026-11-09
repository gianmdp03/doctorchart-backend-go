# Protocolo paralelo — Sol y Gemini Argon
**Vigente desde M2 por decisión del propietario. M1 cerrado/APROBADO en 6152867. M2-SOL autorizado e implementado para revisión; Angular reservado a Argon. M3 no autorizado.**

## Roles y repositorios
| Agente | Implementación propia | Revisión cruzada |
|---|---|---|
| Sol | Doctor Chart backend Go, PostgreSQL, consumidor Billing, autorización/cuotas, endpoints/tests | Entrega frontend de Argon |
| Gemini Argon | Doctor Chart frontend Angular, gestión de lugares, formularios/navegación/tests | Entrega backend de Sol |

Ambos pueden implementar simultáneamente cuando el propietario apruebe sus tickets y contrato. Las reglas antiguas de implementador único y Argon solo auditor se conservan como historia de M1, no limitan el nuevo reparto.

## Exclusión por directorio Git — obligatoria
- **No utilizar Git worktrees bajo ninguna circunstancia. Nunca dos agentes modifican simultáneamente el mismo directorio Git.**
- La exclusión incluye aplicación, docs, configuración, archivos generados, formateadores, instalaciones, tests que escriban, stage, commits y cambios de rama. Ramas diferentes no habilitan compartir checkout.
- Antes de escribir, registrar en la coordinación: `CLAIM <repo> <agente> <rama> <base SHA> <ticket>`. El coordinador (propietario o chat de coordinación) confirma que no hay otro escritor. CLAIM y RELEASE backend/Billing de Sol registrados en tablero; frontend no fue escrito desde este chat.
- Al terminar o ceder: informar Git real (staged/unstaged/untracked), SHA/diff y emitir `RELEASE <repo> <agente>`. No inferir liberación por silencio ni cambiar de rama en un checkout ocupado.
- Autor distinto puede leer `git show <SHA>`/diff congelado sin alterar checkout. Para ejecutar tests/build que escriban en repo ajeno, primero RELEASE del autor y CLAIM temporal del revisor; luego liberar y devolver propiedad. No corregir código del otro agente durante revisión.
- Sol es escritor de coordinación canónica en backend mientras tenga ese CLAIM. Argon comunica avances y entrega su informe en frontend o al coordinador; nunca actualiza backend/docs/ai en paralelo. No hay script de bloqueo: la exclusión se registra y confirma expresamente por coordinación.

## Contrato y fases
1. Mandan las decisiones del propietario y AGENTS reales. Leer inicio, handoff, ticket y el [contrato único](contracts/M2_CLINICS_API.md), no auditorías históricas.
2. API M2-CLINICS-v1 congelada, hash en handoff. Sol y Argon entregaron M2; commits congelados e informe cruzado en handoff. Correcciones y cada fase posterior necesitan autorización propia. Snapshot apto permite operar aunque Billing caiga, sin extender cobertura. No duplicar el contrato.
3. Cambio de DTO/ruta/error/semántica → propuesta al coordinador, revisión conjunta y aprobación antes de actualizar contrato/mocks. No inventar endpoints o permisos para desbloquear una rama.
4. Angular puede usar mocks del contrato aprobado, solo tests/desarrollo explícito. No espera backend terminado, pero no habilita operaciones reales sin derechos efectivos; los errores no disparan fallback a mocks.
5. Backend verifica propiedad y derechos en cada mutación. Sin contrato comercial efectivo no hay altas, edición ni cambios de estado operativos. Billing define cantidades; no inferir ilimitado o defaults.
6. Autorización de desarrollo no autoriza commits, merge a main, push, despliegues o bases reales. Registrar el permiso concreto para cada acción. Respetar production=true, baseline inmutable y entornos descartables propios autorizados.
7. Pacientes e historias permanecen por médico, sin pertenencia clínica por consultorio. No exigir lugar para clínica/wizard; no recuperar arquitectura descartada.
8. Rutas en inglés, UI española, fechas dd/MM/aaaa y app-date-picker. /appointments es público futuro; segmentos médico/consultorio pendientes. Calculadoras suspendidas sin cambios.

## Entrega y cierre cruzado
Cada autor entrega repo/rama/base/SHA o diff congelado, archivos, tests/comandos/resultados/omitidos, datos simulados vs integración real y riesgos. El otro agente revisa independientemente esa entrega, con severidad, archivo/línea y evidencia; no audita su propio código.
Autor corrige en su repo; revisor revalida. Propietario acepta fase y autoriza por separado cualquier integración/publicación. No empezar fase siguiente por inferencia.
La revisión frontend no habilita comercialmente el backend ni la revisión backend valida automáticamente mocks como E2E real. Contrato y evidencia de ambos deben coincidir antes de una integración controlada.
