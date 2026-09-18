# Filezam

> Gestor de archivos web autoalojado, rápido y seguro: expón una carpeta de tu servidor a través del navegador, con usuarios, subidas grandes reanudables y enlaces públicos temporales. Un único binario Go con la interfaz integrada, pensado para ejecutarse en un contenedor detrás de tu proxy inverso.

[![CI](https://github.com/miguelzamberlan/filezam/actions/workflows/ci.yml/badge.svg)](https://github.com/miguelzamberlan/filezam/actions/workflows/ci.yml)
[![Versión](https://img.shields.io/github/v/release/miguelzamberlan/filezam?label=versi%C3%B3n)](https://github.com/miguelzamberlan/filezam/releases)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![SQLite](https://img.shields.io/badge/SQLite-sin%20CGO-003B57?logo=sqlite&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-distroless-2496ED?logo=docker&logoColor=white)
[![Licencia](https://img.shields.io/badge/licencia-AGPL--3.0-blue)](LICENSE)

[🇧🇷 Português](README.md) · [🇬🇧 English](README.en.md) · 🇪🇸 Español

<p align="center">
  <img src="docs/img/desktop-light.png" alt="Filezam en el escritorio, tema claro" width="49%">
  <img src="docs/img/desktop-dark.png" alt="Filezam en el escritorio, tema oscuro" width="49%">
</p>

> La documentación completa (`docs/`), el changelog y `CONTRIBUTING.md` están escritos en portugués de Brasil; los enlaces a ellos se marcan más abajo con *(portugués)*. La interfaz está disponible en portugués, inglés y español. Las issues y pull requests en español son bienvenidas (también en inglés o portugués).

## Índice

- [Por qué existe](#por-qué-existe)
- [Novedades en la 1.6.0](#novedades-en-la-160)
- [Funcionalidades](#funcionalidades)
- [Cómo ejecutarlo](#cómo-ejecutarlo)
  - [En tu máquina en un minuto](#en-tu-máquina-en-un-minuto)
  - [Sin Docker](#sin-docker)
  - [En un servidor, detrás de un proxy](#en-un-servidor-detrás-de-un-proxy)
  - [En Easypanel](#en-easypanel)
- [Configuración](#configuración)
- [Cómo usarlo](#cómo-usarlo)
- [Seguridad](#seguridad)
- [Documentación](#documentación)
- [Desarrollo](#desarrollo)
- [Contribuir](#contribuir)
- [Hoja de ruta y limitaciones](#hoja-de-ruta-y-limitaciones)
- [Versiones](#versiones)
- [Autor y licencia](#autor-y-licencia)

## Por qué existe

Filezam nació de una necesidad concreta: un disco externo conectado a un servidor Linux doméstico, compartido en la red local por Samba, que debía ser accesible desde internet para la familia y para clientes, cada uno viendo solo su propia carpeta. Las alternativas existentes eran pesadas, exigían una base de datos externa o trataban la seguridad del sistema de archivos como un detalle.

Otras dos necesidades aparecieron poco después, y moldearon buena parte de lo que Filezam es hoy.

La primera fue **entregar material a los clientes y recibir material de ellos**. Enviar el enlace de una carpeta terminada resuelve la mitad del problema; la otra mitad es el cliente que tiene que devolver las fotos en bruto, el contrato firmado o el arte aprobado — y que no va a crearse una cuenta para eso. De ahí los dos tipos de enlace público: el de **solo lectura**, para entregar, y el de **recepción**, un buzón donde quien no tiene cuenta solo escribe y nunca ve lo que ya está ahí.

La segunda fue **saber qué produjo el equipo**, sin abrir archivo por archivo. Una carpeta de trabajo con cientos de vídeos y fotos no responde por sí sola cuántas horas de material hay ahí, cuántos vídeos salieron en 4K, cuántas fotos son verticales o con qué cámara se hicieron. De ahí el análisis de medios, que lee todo eso directamente de la cabecera de los archivos.

Tres prioridades guían cada decisión, en este orden:

1. **Seguridad.** Es imposible leer o escribir fuera de la carpeta expuesta: todo acceso al disco pasa por `os.Root` de Go, que valida cada componente de la ruta en el kernel, symlinks incluidos. Ningún archivo subido por un usuario puede ejecutar scripts en el navegador de otro. Las contraseñas, las sesiones y los secretos de 2FA nunca se guardan en texto plano en la base de datos.
2. **Velocidad.** E/S en streaming sin cargar archivos en memoria, pocas peticiones para muchos archivos pequeños, subidas en bloques paralelos y operaciones largas en segundo plano con progreso.
3. **Simplicidad de operación.** Un contenedor, configurado por variables de entorno, sin base de datos externa, sin shell en la imagen, ejecutándose sin root.

Es un proyecto personal, gratuito y de código abierto. Sirve bien para servidores domésticos, pequeñas oficinas y para quien quiere entregar archivos a clientes sin depender de servicios de terceros. Lo que **no** pretende ser: un Dropbox con sincronización, un editor de documentos ni un servidor WebDAV/S3.

## Novedades en la 1.6.0

**📊 Datos técnicos de foto y vídeo, y análisis de una carpeta entera.** Las propiedades de un archivo ahora muestran resolución, duración, códec, cuadros por segundo y tasa de bits y — cuando el archivo lo trae — cámara, objetivo, ISO, obturador, apertura, distancia focal y fecha de captura. Una foto de móvil tomada en vertical aparece en vertical, como la muestra un visor, y no como están guardados los píxeles.

Y **«Analizar medios»**, en el menú contextual de un archivo, de una selección o de una carpeta entera, responde de una vez: cuántos vídeos y fotos hay, la duración sumada, cuántos vídeos en 4K, 2K, Full HD y HD, cuántas fotos en cada rango de megapíxeles, cuántas verticales y horizontales, los formatos, los códecs, los cuadros por segundo, las cámaras usadas y el periodo de las capturas — con el más largo, el más grande y el de más píxeles destacados, y exportación a hoja de cálculo.

Todo se lee de la **cabecera de los archivos**, sin `ffmpeg` y sin decodificar un solo fotograma: MP4, MOV, MKV, WebM, AVI, JPEG, PNG, TIFF, HEIC, AVIF y los RAW de cámara. El binario sigue siendo único y sin CGO. La primera lectura de cada archivo queda guardada, así que repetir el análisis es instantáneo; en una carpeta con muchos archivos nuevos la lectura corre en segundo plano con barra de progreso.

**📁 Mover a…** La selección ganó un «Mover» que abre un selector de carpetas: navega hasta el destino — subiendo, entrando, o creando una carpeta ahí mismo — y confirma. Es el camino del móvil y de quien no arrastra con el ratón. El listado también ganó la columna **Tipo**, que agrupa los archivos por extensión.

El historial completo está en [`CHANGELOG.md`](CHANGELOG.md) *(en portugués)*.

## Funcionalidades

- **Usuarios y ámbitos**: inicio de sesión con usuario y contraseña, perfiles administrador/usuario, cada cuenta con acceso a la raíz completa o a una subcarpeta que ve como si fuera la raíz.
- **Gestión completa**: navegar, crear carpetas, renombrar, copiar, cortar/pegar (mover), eliminar, descargar un archivo o un ZIP, favoritos, propiedades (tamaño calculado, cantidad de elementos, enlaces activos) y espacio libre en disco.
- **Búsqueda** por nombre en todas las subcarpetas a las que el usuario tiene acceso, sin distinguir mayúsculas ni acentos, respondida por un índice en SQLite (escaneo periódico) y verificada en el disco.
- **Papelera** con retención configurable y restauración a la ubicación original. `Shift+Del` elimina de forma permanente.
- **Vista previa** de imágenes, video, audio, PDF, texto y Markdown con formato sin salir de la página.
- **Subidas serias**: archivos de varios GB en bloques paralelos reanudables, carpetas enteras arrastrando y soltando, miles de archivos pequeños por lotes, todo con panel de progreso, pausa y reintento de fallos.
- **Miniaturas**: las carpetas de fotos muestran una vista previa de cada imagen, generada bajo demanda y guardada en caché. Se puede desactivar por instalación y por persona.
- **Datos técnicos de foto y vídeo**: resolución, duración, códec, cuadros por segundo y tasa de bits, cámara, objetivo, ISO y fecha de captura en las propiedades del archivo — y el análisis de una selección o de una carpeta entera: cuántos vídeos, duración sumada, cuántos en 4K, 2K y 1080p, cuántas fotos en cada rango de megapíxeles, cuántas en vertical y en horizontal, formatos, códecs y cámaras, con exportación a hoja de cálculo. Leído directamente de la cabecera de los archivos, sin `ffmpeg`.
- **Extraer y comprimir**: descomprime un `.zip` en una carpeta nueva desde el menú contextual, o genera un `.zip` con lo que esté seleccionado. La extracción nunca sobrescribe lo que ya existe y rechaza archivos que intenten escribir fuera de la carpeta.
- **Editor integrado**: edita archivos de texto, markdown y configuración desde el navegador, con vista previa con formato para `.md` y protección contra dos personas guardando una encima de la otra.
- **Enlaces públicos**: comparte una carpeta o un archivo mediante un enlace de solo lectura con fecha de caducidad, contraseña opcional, recuento de accesos y revocación inmediata. La dirección puede ser un nombre que tú eliges (`/s/presupuesto-2026`), con contraseña obligatoria.
- **Enlaces para recibir archivos**: un buzón público para que quien no tiene cuenta envíe archivos a una carpeta tuya, con cuota y caducidad obligatorias. Quien envía no ve ni descarga lo que ya está allí.
- **Verificación en dos pasos** (TOTP) con códigos de recuperación y "confiar en este dispositivo durante 30 días"; opcionalmente obligatoria para administradores.
- **Administración**: panel con quién está activo y subiendo ahora, uso y cuota de cada uno, sesiones (con cierre) y alertas; usuarios con cuota de disco y restablecimiento de 2FA, auditoría de inicios de sesión y cambios, bloqueo progresivo contra fuerza bruta, historial de operaciones y métricas de Prometheus.
- **Interfaz**: portugués, inglés o español, tema claro/oscuro/sistema, colores personalizables, zoom del listado, iconos por tipo, arrastrar y soltar para mover, listado paginado para carpetas enormes, atajos de teclado y uso cómodo en el teléfono.
- **Operación**: binario estático, imagen `distroless` sin shell, rootfs de solo lectura, SQLite integrado (sin CGO), migraciones automáticas, healthcheck.

<p align="center">
  <img src="docs/img/mobile.png" alt="Filezam en el teléfono" width="30%">
</p>

## Cómo ejecutarlo

Todos los escenarios, con proxies (nginx, Caddy, Traefik, Cloudflare Tunnel), systemd, copias de seguridad y diagnóstico, están en [`docs/08-operacao.md`](docs/08-operacao.md) *(portugués)*. Aquí va lo esencial de cada uno.

### En tu máquina en un minuto

Requisitos: Docker con Compose v2.

```bash
git clone https://github.com/miguelzamberlan/filezam.git && cd filezam
docker compose up -d --build
docker compose logs filezam | grep password   # la contraseña del admin, mostrada una sola vez
```

No hace falta un `.env`: cada valor tiene uno por defecto. Abre `http://127.0.0.1:8080` e inicia sesión como `admin` con la contraseña del log — cambiarla es obligatorio en el primer acceso. Los datos quedan en `./data` y la base de datos en `./config`.

Para usarlo de verdad (carpeta del host, tu usuario, proxy), copia `.env.example` a `.env` y ajústalo; compose lee el archivo si existe.

> **Windows y macOS**: deja los datos **dentro de WSL2** (o en un volumen de Docker), nunca en un
> `C:\...` montado. Además de ser mucho más rápido, NTFS y APFS no distinguen mayúsculas de minúsculas, y
> Filezam lo necesita — consulta [`docs/08`](docs/08-operacao.md#sistema-de-arquivos-da-pasta-de-dados) *(portugués)*.

### Sin Docker

Requisitos: Go (versión en `go.mod`) y Node 22+.

```bash
make web && make build     # frontend + binario ./filezam con la UI integrada
FILEZAM_ROOT=$HOME/Archivos FILEZAM_DATA_DIR=$HOME/.filezam FILEZAM_SECURE_COOKIES=false ./filezam
```

Un único binario, sin dependencias en tiempo de ejecución. Para ejecutarlo como servicio hay un ejemplo de unidad systemd con endurecimiento en [`docs/08-operacao.md`](docs/08-operacao.md#4-binário-direto-com-systemd) *(portugués)*.

### En un servidor, detrás de un proxy

El escenario para el que se hizo Filezam: el contenedor escucha solo en `127.0.0.1` y un proxy con HTTPS se sitúa delante.

```bash
sudo mkdir -p /opt/filezam && cd /opt/filezam
git clone https://github.com/miguelzamberlan/filezam.git . && cp .env.example .env
```

```ini
# .env
PUID=1000                                  # dueño de la carpeta de datos (id -u)
PGID=1000
FILEZAM_HOST_ROOT=/mnt/datos               # carpeta expuesta
FILEZAM_HOST_CONFIG=/opt/filezam/config    # base de datos + secret.key, fuera de la carpeta expuesta
FILEZAM_TRUSTED_PROXIES=172.31.250.1       # gateway de la red de compose: por donde llega el proxy del host
FILEZAM_SECURE_COOKIES=true
FILEZAM_PUBLIC_URL=https://archivos.ejemplo.com
FILEZAM_ADMIN_PASSWORD=cambia-esta-clave
FILEZAM_REQUIRE_2FA_ADMINS=true
```

```bash
docker compose up -d --build && docker compose logs -f filezam
```

**Solo en la red local, sin proxy**: `FILEZAM_BIND=0.0.0.0`, `FILEZAM_TRUSTED_PROXIES=` (vacío) y `FILEZAM_SECURE_COOKIES=false`; abre `http://ip-del-servidor:8080`. Nunca pongas el rango de la red local (`192.168.0.0/16`) ni `0.0.0.0/0` en `FILEZAM_TRUSTED_PROXIES`: cualquier máquina podría elegir su propia IP y esquivar el bloqueo de inicio de sesión. Gravedad y combinaciones (proxy + puerto local, proxy en contenedor) en [docs/08](docs/08-operacao.md#porta-local-e-proxies-confiáveis) *(portugués)*.

En el proxy, el cuerpo de las peticiones no puede limitarse ni almacenarse en búfer. Caddy funciona sin ajustes (`reverse_proxy 127.0.0.1:8080`); nginx necesita:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;
    proxy_read_timeout 600s;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Traefik en la misma red de Docker: usa los `labels` comentados en `docker-compose.yml` y aumenta el `readTimeout` del entrypoint. Cloudflare (proxy naranja o Tunnel): mantén el bloque de subida en 64 MiB como máximo (el valor por defecto de 16 MiB ya sirve).

### En Easypanel

Easypanel construye la imagen a partir del repositorio y pone su propio Traefik delante con HTTPS automático:

1. **+ Service → App**. *Source*: GitHub, repositorio `miguelzamberlan/filezam`, rama `main`. *Build*: Dockerfile.
2. **Environment**:
   ```ini
   FILEZAM_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
   FILEZAM_SECURE_COOKIES=true
   FILEZAM_PUBLIC_URL=https://archivos.ejemplo.com
   FILEZAM_ADMIN_PASSWORD=cambia-esta-clave
   FILEZAM_REQUIRE_2FA_ADMINS=true
   ```
3. **Mounts**: un *Volume Mount* en `/config` (nace con permiso de escritura, la carpeta ya existe en la imagen con el uid `65532`) y un *Bind Mount* de la carpeta del host en `/data`, con permiso de escritura para el uid `65532` (`sudo chown -R 65532:65532 /mnt/datos`).
4. **Domains**: tu dominio en el puerto `8080` con HTTPS.
5. **Deploy** e inicio de sesión con `admin` + la contraseña de la variable.

Si la carpeta de datos pertenece a otro usuario (compartida por Samba, por ejemplo), usa el tipo *Compose* de Easypanel con el `docker-compose.yml` del repositorio y `user: "uid:gid"` del dueño. Detalles en [`docs/08-operacao.md`](docs/08-operacao.md#3-no-easypanel) *(portugués)*.

## Configuración

### Carpetas y permisos

| Volumen | Contenido |
|---|---|
| `${FILEZAM_HOST_ROOT}` → `/data` | Carpeta raíz mostrada por el gestor. Para exponer varias carpetas del host, móntalas como subcarpetas (justo debajo). |
| `${FILEZAM_HOST_CONFIG}` → `/config` | Base de datos SQLite (usuarios, sesiones, enlaces, papelera, índice, auditoría) y `secret.key` (2FA). Nunca se sirve y no puede estar dentro de `/data`. Haz copia de seguridad de la carpeta entera. |

El contenedor se ejecuta sin root, como `PUID:PGID`, y las dos carpetas deben tener permiso de escritura para ese usuario. Como Docker crea como `root` las carpetas de *bind mount* inexistentes, el compose incluye un servicio `init` (Alpine, se ejecuta una vez) que ajusta el dueño de `/config` y de los archivos de Filezam dentro de ella (nunca de forma recursiva, y se niega a tocar una carpeta que tenga subcarpetas, para que un `FILEZAM_HOST_CONFIG` equivocado no altere un árbol entero del host) y, solo si `/data` está vacía, también de ella. Nunca cambia el dueño de una carpeta de datos que ya tenga contenido. Si la carpeta no tiene permiso de escritura, la aplicación termina al iniciar indicando el `chown` sugerido. Nunca uses `PUID=0`.

### Exponer varias carpetas del host

Filezam expone una sola raíz, pero puedes montar tantas carpetas del host como quieras **como subcarpetas** de ella, en el `docker-compose.yml`. No hay variable de entorno para esto: quien monta es el compose.

```yaml
    volumes:
      - ${FILEZAM_HOST_ROOT:-./data}:/data
      - /mnt/multimedia:/data/multimedia       # aparece como la carpeta "multimedia" en la raíz
      - /mnt/backup/proyectos:/data/proyectos
      - ${FILEZAM_HOST_CONFIG:-./config}:/config
```

Salvedades: las carpetas adicionales ya deben tener permiso de escritura para `PUID:PGID` (el servicio `init` no las toca); mover o eliminar entre montajes se convierte en copia + borrado, porque son sistemas de archivos distintos, y la papelera está siempre en la raíz del ámbito; el espacio libre mostrado es el del disco que contiene la raíz del ámbito, nunca la suma de los montajes; y un symlink no sirve de atajo — `os.Root` rechaza enlaces que apunten fuera de la raíz, tiene que ser un bind mount. Detalles en [`docs/08`](docs/08-operacao.md#expondo-várias-pastas-do-host) *(portugués)*.

### Variables de entorno

| Variable | Por defecto | Descripción |
|---|---|---|
| `FILEZAM_ROOT` | `/data` (imagen) / `./data` | Carpeta raíz de los archivos |
| `FILEZAM_DATA_DIR` | `/config` (imagen) / `./config` | Base de datos y `secret.key`. No puede estar dentro de `FILEZAM_ROOT` |
| `FILEZAM_LISTEN` | `:8080` | Dirección de escucha |
| `FILEZAM_TRUSTED_PROXIES` | vacío | IPs/CIDRs del proxy inverso, separadas por comas. Confía solo en el proxy: quien esté en la lista elige la IP de la auditoría y de los límites de inicio de sesión ([puerto local y proxies](docs/08-operacao.md#porta-local-e-proxies-confiáveis), *portugués*) |
| `FILEZAM_SECURE_COOKIES` | `auto` | `true` / `false` / `auto` (detecta vía `X-Forwarded-Proto` de un proxy de confianza) |
| `FILEZAM_PUBLIC_URL` | vacío | Base de los enlaces públicos (`https://...`); sin ella la interfaz usa la dirección del navegador |
| `FILEZAM_ADMIN_USER` / `FILEZAM_ADMIN_PASSWORD` | `admin` / *(aleatoria)* | Admin creado en el primer inicio; una contraseña vacía se genera al azar y aparece una sola vez en el log. Cambio obligatorio en el primer acceso |
| `FILEZAM_SESSION_TTL` | `168h` | Validez deslizante de la sesión (tope absoluto: 30 días) |
| `FILEZAM_MAX_UPLOAD_CHUNK` | `16MiB` | Tamaño del bloque de subida (1 MiB–1 GiB) |
| `FILEZAM_UPLOAD_MAX_RESERVED` | `100GiB` | Espacio que las subidas sin terminar de un usuario pueden reservar en el disco; `0` = sin tope |
| `FILEZAM_SHARE_MAX_TTL` | `720h` | Validez máxima de un enlace público |
| `FILEZAM_TRASH_RETENTION` | `720h` | Tiempo en la papelera antes de eliminar de forma permanente; `0` desactiva la papelera. Valor inicial: el admin lo cambia en **Configuración del sistema** |
| `FILEZAM_INDEX_INTERVAL` | `6h` | Escaneo completo del índice de nombres de la búsqueda; `0` desactiva el índice. Valor inicial (mínimo 15 min): el admin cambia el intervalo en **Configuración del sistema** |
| `FILEZAM_METRICS_TOKEN` | vacío | Activa `GET /metrics` (Prometheus) con `Authorization: Bearer` |
| `FILEZAM_SECRET_KEY` | vacío | Clave (64 hex) que cifra los secretos de 2FA; vacío = `/config/secret.key` generado en el primer inicio |
| `FILEZAM_REQUIRE_2FA_ADMINS` | `false` | Obliga a los administradores a activar la verificación en dos pasos |
| `FILEZAM_FSYNC` | `true` | `fsync` antes de finalizar cada subida |
| `FILEZAM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

En el `.env` del compose también están `PUID`/`PGID`, `FILEZAM_HOST_ROOT`, `FILEZAM_HOST_CONFIG`, `FILEZAM_BIND` y `FILEZAM_PORT` (puerto local al que se conecta el proxy; por defecto solo `127.0.0.1`).

## Cómo usarlo

### Usuarios y ámbitos

En **Administración → Usuarios** el admin crea cuentas y define el perfil (administrador o usuario) y la **carpeta de acceso**: la raíz completa o una subcarpeta específica. El usuario restringido ve la subcarpeta como si fuera la raíz y no puede alcanzar nada fuera de ella. Cambiar el ámbito o la contraseña invalida las sesiones del usuario; reducir el ámbito también elimina los enlaces públicos que tenía fuera de la nueva carpeta. No es posible eliminar ni degradar al último administrador.

Tras 10 intentos fallidos seguidos de inicio de sesión desde la misma dirección, ese par usuario + IP queda bloqueado durante 15 minutos (duplicándose en cada repetición); la respuesta es la misma que la de una contraseña incorrecta, así que nadie descubre si la cuenta existe, y el usuario legítimo puede seguir entrando desde otra dirección. También hay un límite de intentos por IP y por usuario. El admin puede asignar a cada cuenta una **cuota de disco** (en GB). Todo queda registrado en **Auditoría**.

### Enlaces públicos

Selecciona una carpeta o un archivo, haz clic en **Compartir**, elige la validez y, si quieres, una contraseña: el enlace (`/s/<token>`) se copia al instante y puede volver a copiarse en **Enlaces compartidos** o en las propiedades de la carpeta. Quien tenga el enlace puede listar, previsualizar y descargar (archivo o ZIP), nada más. El token tiene 256 bits. Revocarlo o que caduque invalida el enlace al instante; desactivar al usuario o sacar la carpeta de su ámbito también. Mover, renombrar o recrear el elemento compartido invalida el enlace.

**Dirección personalizada.** En lugar del token puedes elegir la dirección (`/s/presupuesto-2026`), útil para dictarla por teléfono o imprimirla. Como un nombre así es fácil de adivinar, exige contraseña — es ella la que protege el enlace. Una dirección revocada sigue reservada para ti: nadie más puede crear un enlace con ella hasta que la liberes en **Enlaces compartidos**.

### Enlaces para recibir archivos

¿Necesitas que un cliente te envíe documentos y no quieres crearle una cuenta? En **Compartir**, elige **Recibir archivos**: indica el nombre de una carpeta nueva, la cuota y la validez (como máximo 30 días). La carpeta se crea en el momento y debe estar vacía — nada de lo que ya tienes queda expuesto.

Quien abre el enlace solo puede enviar. No lista, no descarga, no ve lo que otras personas enviaron (ve únicamente sus propios envíos) y nunca sobrescribe nada: un nombre repetido se guarda como `nombre (1).ext`. Cada envío aparece en **Auditoría** con la IP de origen.

La función viene desactivada. Un administrador la activa en **Administración → Configuración del sistema**, donde también están los topes: cuota máxima por enlace, validez máxima, tamaño por archivo, número de archivos y cuántos enlaces puede tener cada usuario.

### Subidas

| Tamaño | Método |
|---|---|
| ≤ 1 MiB | Varios archivos por petición (`multipart`), hasta 200 archivos / 32 MiB por lote |
| ≤ bloque | Un `PUT` directo |
| > bloque | Sesión por bloques: bloques paralelos, fuera de orden, idempotentes; `complete` solo después de todos |

Los archivos se escriben en `.filezam-upload-*.part` en el directorio de destino y se renombran de forma atómica al final, así que nunca aparece un archivo a medias. Las sesiones abandonadas se eliminan tras 24 h. Si el navegador se cierra en medio de una subida grande, al volver aparece un aviso: suelta el mismo archivo en la misma carpeta y solo se envían los bloques que faltan.

### Interfaz

- **Archivos** (menú lateral) es la navegación en sí. El filtro de la parte superior de la lista solo filtra la carpeta actual; **Buscar** busca por nombre en todas las subcarpetas. El resultado lleva a la carpeta con el elemento seleccionado.
- Las **subidas** se hacen arrastrando archivos/carpetas al listado o con los botones **Subir archivos**/**Subir carpeta**; el progreso aparece en un panel flotante con pausa, cancelación y reintento de fallos.
- **Papelera**: lo que eliminas queda allí durante el plazo elegido en **Configuración del sistema** (30 días de fábrica) y puede restaurarse. Los archivos modificados fuera de Filezam (Samba, SSH) aparecen en la búsqueda tras el siguiente escaneo del índice (intervalo también en **Configuración del sistema**) o cuando el admin hace clic en **Escanear ahora**.
- **Operaciones**: copias, movimientos y eliminaciones en curso, con cancelación, y el historial de los últimos 30 días.
- **Cuenta**: cambiar la contraseña y activar la verificación en dos pasos escaneando el código QR con la app autenticadora; guarda los 10 códigos de recuperación. En el inicio de sesión, "Confiar en este dispositivo durante 30 días" evita pedir el código en ese navegador.
- **Tema, colores y zoom** (**Mi cuenta**, en la tarjeta con tu nombre al pie del menú, junto con la contraseña y la verificación en dos pasos): claro, oscuro o igual que el sistema; colores de acento con combinaciones predefinidas o selector libre; ampliación del listado sin tocar el zoom del navegador. Todo queda en el navegador, por usuario.
- **Arrastrar y soltar**: arrastra elementos del listado a una carpeta o a un nivel de la ruta de navegación para moverlos.
- **Teléfono**: el menú se convierte en un cajón, la barra de acciones se reduce a lo esencial más **⋯**, un toque abre, una pulsación larga selecciona y abre el menú.

Atajos: `↑ ↓ Home End PgUp PgDn` navegar · `Shift`/`Ctrl` selección múltiple · `Ctrl+A` todo · `Enter` abrir · `Backspace`/`Alt+↑` subir un nivel · `F2` renombrar · `Del` eliminar · `Ctrl+C` `Ctrl+X` `Ctrl+V` copiar/cortar/pegar · `Ctrl+Shift+N` nueva carpeta · `Esc` limpiar · escribir letras salta al nombre.

## Seguridad

Resumen de lo que el proyecto garantiza. El modelo de amenazas completo, los controles y las limitaciones aceptadas están en [`docs/03-seguranca.md`](docs/03-seguranca.md) *(portugués)*; cómo reportar una vulnerabilidad, en [`SECURITY.md`](SECURITY.md).

- **Sandbox de rutas**: todo acceso al disco pasa por `os.Root`; `..`, symlinks hacia fuera y nombres internos (`.filezam-*`) se rechazan en la entrada, incluso en lectura. Los nombres nuevos no aceptan caracteres de control. Las pruebas mantienen un archivo canario fuera de la raíz y comprueban que ninguna operación lo alcanza.
- **El contenido subido por usuarios nunca se convierte en página**: HTML/JS/SVG-como-texto se sirven como `text/plain`, el resto como descarga; las vistas previas inline llevan CSP `sandbox` y `nosniff`; Markdown se renderiza en el cliente sin HTML en bruto.
- **Sesiones**: cookie `HttpOnly` + `SameSite=Strict`, solo el hash en la base de datos, validez deslizante con tope de 30 días; contraseñas con Argon2id; bloqueo progresivo y limitación de frecuencia en el inicio de sesión **y en el cambio de contraseña**; 2FA TOTP con secreto cifrado (AES-GCM) y protección contra reutilización.
- **CSRF**: cabecera `X-Filezam: 1` obligatoria en toda petición que modifica estado, además de `Sec-Fetch-Site`/`Origin`.
- **Cabeceras**: CSP estricta en la SPA, `X-Frame-Options`, `Referrer-Policy: same-origin`, HSTS detrás de un proxy HTTPS de confianza; los tokens de enlace nunca llegan a los logs.
- **Recursos**: cuota de disco por usuario, límite de zips, jobs, búsquedas y subidas simultáneas, limitación de frecuencia en las rutas públicas.
- **Contenedor**: distroless sin shell, sin root, rootfs de solo lectura, `cap_drop ALL`, `no-new-privileges`. Versión de Go fijada; `govulncheck` y `npm audit` se ejecutan en la CI.
- **Limitaciones aceptadas**: un admin con ámbito ve los enlaces de todos; el token del enlace se guarda en claro en la base de datos para poder volver a copiarlo; los enlaces van ligados a ruta + inode; quien comparte la IP con un atacante queda bloqueado junto con él. Lista completa en [`docs/10-roadmap.md`](docs/10-roadmap.md) *(portugués)*.

## Documentación

La carpeta [`docs/`](docs/README.md) es la fuente de verdad del proyecto y se actualiza en el mismo commit que cambia el comportamiento. Está escrita en portugués de Brasil:

| Documento | Contenido |
|---|---|
| [01 · Visión general](docs/01-visao-geral.md) | Objetivo, requisitos, stack y decisiones de arquitectura (ADRs) |
| [02 · Arquitectura](docs/02-arquitetura.md) | Paquetes, flujo de una petición, ciclo de vida, límites |
| [03 · Seguridad](docs/03-seguranca.md) | Modelo de amenazas, sandbox, autenticación, CSRF, cabeceras, enlaces públicos, contenedor |
| [04 · API HTTP](docs/04-api.md) | Todos los endpoints, payloads y códigos de error |
| [05 · Subidas](docs/05-uploads.md) | Modos de envío, sesiones por bloques, reanudación, conflictos, limpieza |
| [06 · Base de datos](docs/06-banco-de-dados.md) | Esquema SQLite, migraciones, copia de seguridad |
| [07 · Frontend](docs/07-frontend.md) | Estructura React, estado, subidas, teclado, tema, teléfono |
| [08 · Operación](docs/08-operacao.md) | Local, servidor, Easypanel, systemd, variables, proxies, actualización, diagnóstico |
| [09 · Pruebas](docs/09-testes.md) | Suites automatizadas y checklist manual de versión |
| [10 · Hoja de ruta](docs/10-roadmap.md) | Limitaciones conocidas y próximos pasos |

## Desarrollo

Requisitos: Go (la versión de `go.mod` la descarga automáticamente `go`), Node 22+.

```bash
make run     # API en :8080 sirviendo ./data (cookies sin Secure)
make dev     # Vite en :5173 con proxy de /api hacia :8080
make web     # build del frontend en internal/server/webdist/dist
make build   # binario ./filezam con la UI integrada
make test    # go test ./... + tsc + vitest
make vuln    # govulncheck + npm audit
```

Estructura en dos partes: `cmd/` e `internal/` (Go: `vfs` es el núcleo de seguridad, `server` los handlers HTTP, `store` el SQLite, `uploads` el protocolo de subida, `jobs` las operaciones en segundo plano) y `web/` (React 19 + TypeScript + Vite + Tailwind 4, integrado en el binario vía `go:embed`). Las llamadas a la API desde scripts necesitan la cabecera `X-Filezam: 1`.

Subcomandos del binario: `serve` (por defecto), `healthcheck` (usado por Docker), `reset-admin [contraseña]` (restablece la contraseña del admin y obliga a cambiarla en el siguiente inicio de sesión), `version`.

La CI de GitHub ejecuta pruebas, `govulncheck`, `npm audit` y el build de la imagen en cada PR; las tags `v*` publican la imagen multi-arch en `ghcr.io/miguelzamberlan/filezam`. El checklist manual antes de una versión está en [`docs/09-testes.md`](docs/09-testes.md#checklist-manual-antes-de-uma-versão) *(portugués)*.

## Contribuir

Las issues y pull requests son bienvenidas, en español, inglés o portugués. [`CONTRIBUTING.md`](CONTRIBUTING.md) *(portugués)* explica cómo preparar el entorno, las reglas innegociables (todo acceso al disco a través de `internal/vfs`, rutas nunca en segmentos de URL, nunca servir HTML subido por usuarios, documentación actualizada en el mismo commit, pruebas para todo endpoint que modifica estado) y el flujo de un PR. Para vulnerabilidades, sigue [`SECURITY.md`](SECURITY.md) en lugar de abrir una issue pública.

## Hoja de ruta y limitaciones

Lo que ya se sabe que falta (reanudación de operaciones interrumpidas, passkeys) y lo que se descartó a propósito (búsqueda por contenido, reanudación automática de subidas) está en [`docs/10-roadmap.md`](docs/10-roadmap.md) *(portugués)*. Las sugerencias pasan por una issue antes de convertirse en código.

## Versiones

La versión actual es la **1.6.0**; el historial está en el [`CHANGELOG.md`](CHANGELOG.md) *(portugués)*. El proyecto usa versionado semántico: las versiones publicadas no cambian, y las correcciones y novedades salen en versiones nuevas. En producción, fija una versión (`ghcr.io/miguelzamberlan/filezam:1.6.0`, o `:1.6` para recibir solo correcciones) en lugar de seguir `main`. Detalles en [`CONTRIBUTING.md`](CONTRIBUTING.md#versões-e-lançamentos) *(portugués)*.

## Autor y licencia

**Miguel Zamberlan** ([@miguelzamberlan](https://github.com/miguelzamberlan)) es el autor y mantenedor: define las prioridades, revisa las contribuciones y publica las versiones. Filezam se desarrolla en el tiempo libre, con el apoyo de herramientas de IA para revisión e implementación, y todo el código pasa por pruebas automatizadas y verificación manual antes de entrar en `main`.

Copyright (C) 2026 Miguel Zamberlan. Filezam es software libre bajo la **[GNU Affero General Public License v3](LICENSE)** (`AGPL-3.0-only`), con **licencia dual**:

- **AGPLv3, gratuita**: úsalo, estúdialo, modifícalo y distribúyelo. Quien modifique Filezam y lo ofrezca a usuarios a través de la red (servidor propio, nube, SaaS) debe poner a disposición de esos usuarios el código fuente de las modificaciones, bajo la misma licencia.
- **Licencia comercial**: para usar o integrar Filezam sin las obligaciones de la AGPL, contacta con el autor a través de su [perfil de GitHub](https://github.com/miguelzamberlan).

Las copias, forks y obras derivadas deben conservar las cabeceras de licencia de los archivos, el [`LICENSE`](LICENSE), el [`NOTICE.md`](NOTICE.md) y el crédito al autor original que se muestra en la interfaz (términos adicionales de la sección 7 de la AGPLv3, detallados en el `NOTICE.md`). Las versiones 1.0.0 a 1.2.0 se publicaron bajo la licencia MIT.
