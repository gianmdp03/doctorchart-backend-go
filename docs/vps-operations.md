# Contexto de este repositorio

Backend DoctorChart: servicio doctorchart, base doctorchart, propietario doctorchart_app, puerto interno 8083. Preservar siempre los cambios locales del propietario. El bootstrap del código desplegado reconstruye el esquema sobre una base vacía.

# Operación del VPS ByCode

Guía para agentes de IA y para el propietario. Documentada el 24/10/2026 a partir
de salidas reales del VPS del 23/10/2026. Es una referencia, no una autorización
para ejecutar comandos. Revalidar el estado actual antes de operar.

## Acceso y directorios

- Sistema: Ubuntu; usuario observado: ubuntu; hostname: vps-c0b04563.
- El hostname del prompt no prueba cuál es la IP o dominio SSH. No inventarlos.
- Infraestructura Git: /home/ubuntu/infra/bycode-stack.
- Compose en ejecución: compose.production.yml; proyecto Compose: bycode-stack.
- Variables de interpolación: /home/ubuntu/infra/bycode-stack/.env.production.
- Código: /home/ubuntu/infra/repos/<repositorio>.
- Docker requiere sudo para ubuntu. No cambiar grupos/permisos como solución automática.
- No hay conexión SSH automática confirmada para el agente. Cuando no haya acceso,
  entregar comandos al propietario y verificar la salida; no afirmar que se ejecutaron.

## Mapa confirmado del stack

| Repositorio en el VPS | Servicio Compose | Contenedor | Imagen observada | Puerto interno |
| --- | --- | --- | --- | --- |
| bycode-backend-go | billing | bycode-stack-billing-1 | bycode/billing:vps | 8081 |
| pointly-backend-go | pointly | bycode-stack-pointly-1 | bycode/pointly:vps | 8080 |
| doctorchart-backend-go | doctorchart | bycode-stack-doctorchart-1 | bycode/doctorchart:vps | 8083 |
| api-ai-gateway-go | gateway | bycode-stack-gateway-1 | bycode/gateway:vps | 8000 |
| bycode-image-email-service-go | image-email | bycode-stack-image-email-1 | bycode/image-email:vps | 8080 |

El servicio de imágenes/correos también es llamado informalmente "image converter".
El nombre real del repositorio es bycode-image-email-service-go.

También existen postgres, auth, proxy y cloudflared. PostgreSQL es compartido
entre aplicaciones y autenticación: no es un contenedor exclusivo de un producto.
El proxy observado es Nginx y existe un túnel Cloudflare. Los puertos de aplicaciones
son internos; no asumir que localhost:8080 o localhost:8083 del host los expone.

Los frontends, Pointly Mobile y ByCode Renders no tienen un servicio ni repositorio
confirmados en este VPS. El propietario se encarga del frontend de Pointly.
No inferir que se despliegan con Compose ni inventar una plataforma de hosting.

## PostgreSQL y variables

- Versión observada: PostgreSQL 18.6.
- Contenedor: bycode-stack-postgres-1.
- Publicación del puerto: 127.0.0.1:55432 hacia 5432 del contenedor.
- Volumen compartido: postgres-data, montado en /var/lib/postgresql.
- Pointly: base pointly, propietario/usuario pointly_app.
- DoctorChart: base doctorchart, propietario/usuario doctorchart_app.
- Ambos conectan con DATABASE_URL a postgres:5432 desde la red Docker.
- Billing usa DB_URL y puede usar DB_USERNAME/DB_PASSWORD. Su nombre de base y
  propietario productivos deben inspeccionarse, no deducirse de los defaults locales.
- Las bases de auth y otros servicios tampoco se identifican por suposiciones.

.env.production contiene valores privados. Hay archivos por servicio dentro de
production/, incluidos pointly.env, doctorchart.env, billing.env, gateway.env,
image-email.env, auth.env y r2-*.env. Su existencia no demuestra cómo se cargan:
revisar la configuración de Compose del VPS.

No pegar .env completos, DATABASE_URL, contraseñas, tokens o claves privadas en
chats, commits o esta guía. Un docker inspect completo o docker compose config
interpolado puede exponerlos: seleccionar solamente los campos necesarios.

## Inspección inicial sin cambios

~~~bash
sudo docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Ports}}'
sudo docker compose ls

cd /home/ubuntu/infra/bycode-stack
git status --short --branch

cd /home/ubuntu/infra/repos/pointly-backend-go
git status --short --branch
git log -1 --oneline
~~~

Para revisar la construcción de un servicio sin mostrar variables privadas:

~~~bash
cd /home/ubuntu/infra/bycode-stack
sudo env STACK_DIR=/home/ubuntu/infra/bycode-stack \
  REPOS_DIR=/home/ubuntu/infra/repos \
  docker compose --env-file .env.production \
  -f compose.production.yml -f compose.build.yml \
  config --no-interpolate --format json | python3 -c '
import json, sys
s = json.load(sys.stdin)["services"]["pointly"]
print(json.dumps({k: s[k] for k in ("image", "build") if k in s}, indent=2))
'
~~~

Cambiar el servicio seleccionado cuando corresponda. Evitar imprimir todo el JSON.
Los Dockerfiles/rutas de otros servicios pueden pertenecer al stack; no asumir
que todos tienen un Dockerfile en su repositorio.

### Identificar la conexión sin revelar la contraseña

Antes de un wipe, confirmar la configuración del contenedor del producto,
sin asumir que sigue igual al inventario:

~~~bash
sudo docker inspect bycode-stack-doctorchart-1 | python3 -c '
import json, sys
from urllib.parse import urlsplit
c = json.load(sys.stdin)[0]
env = dict(v.split("=", 1) for v in c["Config"]["Env"] if "=" in v)
url = urlsplit(env.get("DATABASE_URL", ""))
print("Imagen:", c["Config"]["Image"])
print("Servidor:", url.hostname)
print("Base:", url.path.lstrip("/"))
print("Usuario:", url.username)
'
~~~

Para Pointly seleccionar su contenedor. Si no coincide con la base/usuario
esperados, detener el procedimiento y revisar. No usar este parser como si
Billing tuviera necesariamente DATABASE_URL.


## Construcción y despliegue de un servicio

Solo ejecutar cuando el propietario haya autorizado actualizar ese servicio.
Primero comprobar que el código revisado está en el remoto y el checkout del VPS
no contiene cambios ajenos. Usar git fetch y merge --ff-only contra el commit
verificado, sin reset --hard, clean ni descartar modificaciones.

Ejemplo para Pointly, después de actualizar su checkout al commit aprobado:

~~~bash
(
set -eu
cd /home/ubuntu/infra/repos/pointly-backend-go
release="$(git rev-parse HEAD)"

cd /home/ubuntu/infra/bycode-stack
sudo env STACK_DIR=/home/ubuntu/infra/bycode-stack \
  REPOS_DIR=/home/ubuntu/infra/repos BUILD_TAG="$release" \
  docker compose --env-file .env.production \
  -f compose.production.yml -f compose.build.yml build pointly

sudo env BUILD_TAG="$release" \
  docker compose --env-file .env.production \
  -f compose.production.yml up -d --no-deps --force-recreate pointly
)
~~~

Adaptar repositorio y servicio juntos conforme al mapa. BUILD_TAG se aplica solo
a ese comando; no editar un tag global para todo el stack. La construcción debe
terminar correctamente antes de detener nada o reinicializar una base.

El intento sin --env-file .env.production falló por variables vacías.
compose.build.yml también requiere STACK_DIR absoluto; REPOS_DIR absoluto evita
ambigüedad. Pointly y DoctorChart usan el contexto adicional media desde
/home/ubuntu/infra/repos/bycode-image-email-service-go/pkg/media. Si sus cambios
requieren una versión nueva de ese paquete, revisar y coordinar también ese checkout;
no actualizar todos los repositorios indiscriminadamente.

En el stack observado la etiqueta normal es vps. Después de verificar una imagen
nueva se puede actualizar su alias, por ejemplo:
sudo docker image tag bycode/pointly:<release> bycode/pointly:vps
Sustituir <release> por el valor construido. Revalidar qué tag usará Compose en
futuras recreaciones: mover el alias no cambia un contenedor ya iniciado.

La actualización normal conserva la base. Un esquema incompatible se trata según
AGENTS.md y la autorización concreta; no agregar un wipe para resolver un arranque.

## Reinicio sin cambios de Git ni reconstrucción de imagen

Si solo se pide reiniciar DoctorChart:

~~~bash
sudo docker restart bycode-stack-doctorchart-1
~~~

Eso conserva sus datos. Stop/start vuelve a ejecutar la misma imagen del contenedor;
no incluye cambios de código que todavía no se hayan construido/desplegado.

## Wipe autorizado de una sola base

Este procedimiento es destructivo. Documentarlo no autoriza su ejecución. Una
petición anterior de vaciar una base no es permiso permanente para futuros resets.
Confirmar el producto y alcance del pedido actual, y la identidad del contenedor,
base y propietario. Respaldar cuando corresponda al pedido; no añadir confirmaciones
repetidas si el propietario ya autorizó expresamente ese wipe.

Las aplicaciones tienen bootstrap de esquema antes de atender HTTP/jobs. Una base
vacía se reconstruye al iniciar el backend. No ejecutar schema.sql manualmente ni
alterar app_schema_state/checksums para adoptar una base incompatible.
La política y excepciones de AGENTS.md de cada proyecto mandan sobre el esquema.

Ejemplo acotado a los dos productos cuyas identidades fueron verificadas.
Elegir service explícitamente; no agregar otros productos a esta lista sin
identificar antes su base real:

~~~bash
(
set -eu
service=doctorchart

case "$service" in
  doctorchart) database=doctorchart; owner=doctorchart_app ;;
  pointly) database=pointly; owner=pointly_app ;;
  *) printf '%s\n' 'Servicio sin identidad de base verificada'; exit 1 ;;
esac

container="bycode-stack-$service-1"
sudo docker inspect --format 'Contenedor: {{.Name}} | Imagen: {{.Config.Image}}' "$container"
sudo docker stop "$container"

sudo docker exec -i bycode-stack-postgres-1 \
  sh -c 'exec psql -U "${POSTGRES_USER:-postgres}" -d postgres -v ON_ERROR_STOP=1' <<SQL
DROP DATABASE $database WITH (FORCE);
CREATE DATABASE $database OWNER $owner;
SQL

sudo docker start "$container"
)
~~~

Este ejemplo reinicia la misma imagen. Si también cambia el código/esquema,
construir primero la imagen nueva y, tras recrear la base, reemplazar el último
docker start por compose up del servicio con el BUILD_TAG nuevo. No arrancar
deliberadamente la imagen vieja sobre el esquema que se desea reconstruir para
la nueva versión. Si algo falla después de detener el backend, revisar el error
antes de arrancar otra versión por intuición.

Nunca usar docker compose down -v, borrar postgres-data, detener PostgreSQL
compartido ni vaciar otras bases para reinicializar un producto.
La reconstrucción de esquema no garantiza que estén todos los datos comerciales:
revisar seeds requeridos y estado de aplicación. No borrar almacenamiento R2
ni archivos de otro servicio como parte implícita del wipe SQL.

## Verificación posterior

No dar por concluido un despliegue solo porque Docker imprime Started.
Comprobar imagen, estado, reinicios, logs recientes, salud y marcador de esquema.

~~~bash
sudo docker logs --since 5m --tail 60 bycode-stack-doctorchart-1
sudo docker inspect --format \
  'Imagen: {{.Config.Image}} | Estado: {{.State.Status}} | Reinicios: {{.RestartCount}}' \
  bycode-stack-doctorchart-1
~~~

Antes de compartir logs, revisar que no contengan datos personales o secretos.

| Producto | Salud interna |
| --- | --- |
| Pointly | http://<IP-del-contenedor>:8080/readyz |
| DoctorChart | http://<IP-del-contenedor>:8083/api/doctorchart/health |

La salud de Pointly responde sin cuerpo en caso exitoso. DoctorChart debe devolver
status UP. No inventar la misma ruta para gateway, billing o image-email: consultar
sus rutas actuales. La prueba directa interna no valida por sí sola gateway,
túnel, dominio público ni recorridos funcionales.

Ejemplo de consulta desde el host, sin depender de herramientas en imágenes scratch:

~~~bash
doctorchart_ip="$(sudo docker inspect \
  --format '{{range .NetworkSettings.Networks}}{{println .IPAddress}}{{end}}' \
  bycode-stack-doctorchart-1 | head -n 1)"
curl --fail --show-error --max-time 10 \
  "http://$doctorchart_ip:8083/api/doctorchart/health"
~~~

La IP Docker es dinámica. Con varias redes, identificar la accesible desde el host;
no persistir una IP ni publicar puertos para evitar esta comprobación.

Para un wipe autorizado y ya completado, verificar el marcador sin exponer datos:

~~~bash
sudo docker exec -i bycode-stack-postgres-1 \
  sh -c 'exec psql -U "${POSTGRES_USER:-postgres}" -d doctorchart -v ON_ERROR_STOP=1' <<'SQL'
SELECT backend, baseline_version FROM public.app_schema_state;
SELECT count(*) AS pacientes FROM public.patients;
SELECT count(*) AS consultas FROM public.clinical_encounters;
SQL
~~~

En Pointly usar la base pointly y contar companies/clients; no ejecutar consultas
de tablas de otro producto. El esquema nuevo también debe pasar reinicio y salud.

## Recuperación y límites de autorización

- Conservar referencias de imágenes/commits anteriores cuando se requiera rollback.
  Revertir una imagen no restaura datos eliminados y puede ser incompatible con
  un esquema nuevo. No prometer recuperación después de un wipe sin respaldo.
- No ejecutar cambios sobre auth, proxy, cloudflared, PostgreSQL ni servicios
  vecinos como efecto secundario de un despliegue de aplicación.
- No hacer commits/pushes ni desplegar por el solo hecho de leer esta guía.
- No copiar excepciones de baseline de Pointly a DoctorChart o Billing.
- La guía se replica en los repositorios del grupo para que sea accesible a agentes
  trabajando en uno solo. Mantener coherente el contenido común al actualizarla.

