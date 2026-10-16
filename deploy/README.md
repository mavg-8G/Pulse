# Despliegue en el VPS (Docker + Caddy + GitHub Actions)

Pulse corre como **un solo contenedor** (`pulse`) en la red `proxy`, detrás de Caddy, en `https://pulse.espectro.uk`.
El VPS **no compila**: GitHub Actions construye la imagen, la publica en GHCR y la firma; luego entra al VPS por SSH
con una clave que solo puede ejecutar `deploy.sh`.

```
git push (main)
  └─ GitHub Actions: go · web · e2e ─► imagen (tests dentro) ─► Trivy ─► GHCR ghcr.io/mavg-8g/pulse@sha256:…
                                                                         └─ firma cosign (sin claves, OIDC de GitHub)
     └─ job deploy (entorno "production") ── ssh deploy@VPS "deploy sha256:… <commit>"
          VPS: /opt/docker/pulse/deploy.sh  (comando forzado: la clave no puede hacer otra cosa)
            1. descarga la imagen por digest y verifica la firma (solo el workflow de main de tu repo)
            2. respaldo en caliente de la base (data/backups/, se guardan 15)
            3. arranca la versión nueva y espera "healthy"; corre smoke.sh contra https://pulse.espectro.uk
            4. si algo falla: restaura la base, vuelve a la imagen anterior y el job queda en rojo
```

Ramas que no son `main`, pull requests y el fuzzing nocturno solo prueban; nunca despliegan.

## Versiones de construcción — 14 de octubre de 2026

| Componente | Versión | Fuente de configuración |
|---|---|---|
| Go | 1.27.2 | `go.mod`; CI la pasa como argumento a Docker |
| Node LTS | 24.22.0 | `.node-version`; CI la pasa como argumento a Docker |
| Imágenes de compilación | Alpine 3.24 | `Dockerfile` |
| Imagen de ejecución | Distroless static Debian 13, nonroot | `Dockerfile` |
| Trivy | 0.76.0 | `TRIVY_VERSION` y `TRIVY_SHA256` en CI |
| Trivy Action | 0.36.0 | SHA completo en CI |
| Cosign | 3.1.3 | `COSIGN_VERSION` en CI y paso 4 de esta guía |

Trivy se descarga desde una release oficial de GitHub con reintentos y hash SHA-256 fijado; no ejecuta un instalador obtenido de `main`. Las acciones de CI están fijadas a commits de sus releases oficiales. El escaneo sigue bloqueando vulnerabilidades altas y críticas con corrección disponible.

Antes de desplegar con este workflow, actualiza también Cosign en el VPS siguiendo el paso 4. Cambiar el instalador de CI no actualiza el ejecutable del servidor. Docker declara el mismo puerto 8080 que `PULSE_LISTEN` y Caddy.

---

## Primera vez

### 1. DNS
Registro **A** `pulse` → IP del VPS.

### 2. Usuario `deploy` y carpeta (en el VPS)
```bash
# Usuario sin contraseña que solo entra con la clave de GitHub Actions
sudo adduser --system --group --home /home/deploy --shell /bin/bash deploy
sudo usermod -aG docker deploy
# Si usas "AllowUsers" en /etc/ssh/sshd_config, agrega deploy y: sudo systemctl reload ssh

sudo mkdir -p /opt/docker/pulse && cd /opt/docker/pulse
REF=https://raw.githubusercontent.com/mavg-8G/Pulse/main/deploy
for f in docker-compose.yml deploy.sh smoke.sh Caddyfile.snippet; do sudo curl -fsSLo "$f" "$REF/$f"; done
sudo curl -fsSLo .env "$REF/.env.example"
sudo chmod 755 deploy.sh smoke.sh

# root es dueño de los scripts; deploy puede crear sus archivos (release.env, deploy.log) pero
# el bit "sticky" (1) le impide reemplazar o borrar los de root.
sudo chown root:deploy . && sudo chmod 1770 .
sudo chown root:deploy .env && sudo chmod 640 .env

# Datos: el contenedor corre como uid 65532 (sin privilegios)
sudo install -d -o 65532 -g 65532 -m 700 data
```
Edita `.env` (`sudo nano .env`) con tu configuración (Gmail y VAPID se completan en los pasos 4 y 7).

> El grupo `docker` equivale a root en ese servidor. Por eso la clave de `deploy` está atada a un comando
> forzado: aunque alguien la robara de GitHub, solo podría pedir que se despliegue una imagen **firmada por tu workflow**.

### 3. Clave maestra (KEK)
Cifra los datos sensibles de todos los usuarios. **Sin ella, los datos y los backups son irrecuperables.**
```bash
cd /opt/docker/pulse
sudo install -d -o root -g deploy -m 750 secrets
openssl rand -base64 32 | sudo tee secrets/pulse_kek > /dev/null
sudo chown 65532:65532 secrets/pulse_kek && sudo chmod 400 secrets/pulse_kek
sudo cat secrets/pulse_kek   # → cópiala a tu gestor de contraseñas y no la pegues en otro lado
```

### 4. cosign (verificación de firmas)
```bash
VER=v3.1.3 # misma versión que COSIGN_VERSION en .github/workflows/ci.yml
cd /tmp
curl -fsSLO https://github.com/sigstore/cosign/releases/download/$VER/cosign-linux-amd64
curl -fsSLO https://github.com/sigstore/cosign/releases/download/$VER/cosign_checksums.txt
grep ' cosign-linux-amd64$' cosign_checksums.txt | sha256sum -c -   # debe decir OK
sudo install -m 755 cosign-linux-amd64 /usr/local/bin/cosign && rm cosign-linux-amd64 cosign_checksums.txt
cosign version
```

### 5. Clave SSH para GitHub Actions
En tu PC (PowerShell o bash):
```bash
ssh-keygen -t ed25519 -N "" -C github-actions-pulse -f pulse_deploy
```

Si usas Windows PowerShell y `-N ""` no funciona, ejecuta `ssh-keygen -t ed25519 -C github-actions-pulse -f pulse_deploy` y pulsa Enter en las dos preguntas de contraseña. Esta identidad es exclusiva del despliegue y el usuario del VPS tiene el comando restringido indicado abajo.

En PowerShell, comprueba el archivo y copia su contenido completo al portapapeles, sin mostrarlo en el terminal:

```powershell
ssh-keygen -y -f .\pulse_deploy | Out-Null
# Debe terminar correctamente sin pedir contraseña.
Get-Content -LiteralPath .\pulse_deploy -Raw | Set-Clipboard
```

Pégalo en `VPS_SSH_KEY`, dentro del entorno `production`. Incluye las líneas `-----BEGIN OPENSSH PRIVATE KEY-----` y `-----END OPENSSH PRIVATE KEY-----` y todos los saltos de línea intermedios. No pegues el archivo `.pub`, una huella `SHA256:…`, una ruta de archivo, texto codificado en Base64 de todo el archivo ni secuencias literales `\n`. No compartas la clave privada en el chat.
En el VPS, autoriza la clave **pública** (`pulse_deploy.pub`) solo para `deploy.sh`:
```bash
sudo install -d -o deploy -g deploy -m 700 /home/deploy/.ssh
echo 'command="/opt/docker/pulse/deploy.sh",restrict ssh-ed25519 AAAA…contenido de pulse_deploy.pub… github-actions-pulse' \
  | sudo tee /home/deploy/.ssh/authorized_keys > /dev/null
sudo chown deploy:deploy /home/deploy/.ssh/authorized_keys && sudo chmod 600 /home/deploy/.ssh/authorized_keys
```
Huella del servidor (para que Actions no se conecte a un impostor). En tu PC, usa exactamente el mismo dominio/IP y puerto que guardarás en `VPS_HOST` y `VPS_PORT`:
```bash
ssh-keyscan -p 22 -t ed25519 <IP-del-VPS>
```
y compárala con la de la consola del proveedor del VPS: `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` (mismo `SHA256:…` que `ssh-keyscan … | ssh-keygen -lf -`). La consulta por red no verifica por sí sola la identidad del servidor.

También puedes obtener la clave pública directamente en esa consola confiable:

```bash
cat /etc/ssh/ssh_host_ed25519_key.pub
```

`VPS_KNOWN_HOSTS` debe contener la clave pública ED25519 completa, no solo la huella `SHA256:…` ni la clave privada del usuario `deploy`. El workflow acepta la línea de `ssh-keyscan` o la clave pública anterior y la asocia al host y puerto configurados; rechaza varias claves ED25519 distintas. Para el puerto 22, el archivo generado usa `HOST ssh-ed25519 CLAVE`; para otro puerto, `[HOST]:PUERTO ssh-ed25519 CLAVE`.

Prueba desde tu PC: `ssh -i pulse_deploy deploy@<IP> status` → debe responder `imagen:` vacía y `estado: ausente`.
Cualquier otro comando (`ssh -i pulse_deploy deploy@<IP> bash`) debe responder “acción no permitida”.

### 6. GitHub
En el repositorio → **Settings → Environments → New environment** → `production`:
- **Deployment branches and tags**: *Selected branches* → `main`.
- (Opcional) **Required reviewers**: tú, si quieres aprobar cada despliegue con un clic.
- **Environment secrets**:

| Nombre | Valor |
|---|---|
| `VPS_HOST` | Solo IP o nombre del VPS, sin `:puerto`, `ssh://` ni `usuario@` |
| `VPS_PORT` | `22` (o tu puerto SSH) |
| `VPS_SSH_KEY` | contenido completo de `pulse_deploy` (la **privada**) |
| `VPS_KNOWN_HOSTS` | clave pública ED25519 del servidor, obtenida o contrastada con la consola del proveedor |

Después **borra `pulse_deploy` de tu PC** (la pública puede quedarse). En **Settings → Actions → General**, deja
*Workflow permissions* en “Read repository contents”: el workflow pide por sí mismo los permisos extra.

### 7. Primer despliegue
Haz push a `main` (o **Actions → CI/CD → Run workflow**). Este primer job `deploy` **fallará** porque aún no
existe la clave VAPID (necesitas la imagen para generarla); es lo esperado. Cuando el job `image` termine:
1. **Paquete público**: en tu perfil → **Packages → pulse → Package settings → Change visibility → Public**.
   La imagen no contiene secretos (el repositorio ya es público) y así el VPS la descarga sin credenciales.
   *Si prefieres privado*: crea un token clásico con solo `read:packages` y en el VPS
   `sudo -u deploy docker login ghcr.io -u mavg-8G` (pega el token como contraseña).
2. **VAPID** (notificaciones), una sola vez, con la imagen ya publicada:
   ```bash
   cd /opt/docker/pulse
   sudo docker run --rm --user 0 -v "$PWD/secrets:/secrets" ghcr.io/mavg-8g/pulse:main vapid --out /secrets
   sudo chown 65532:65532 secrets/vapid_private && sudo chmod 400 secrets/vapid_private
   ```
   Pega `PULSE_VAPID_PUBLIC_KEY` y tu correo en `.env` (detalles en `PUSH.md`; Gmail en `GMAIL.md`).
3. En Actions abre la ejecución y pulsa **Re-run failed jobs**: ahora sí despliega.

### 8. Caddy
Pega `/opt/docker/pulse/Caddyfile.snippet` en tu Caddyfile y recarga:
```bash
docker exec caddy caddy reload --config /etc/caddy/Caddyfile
```

### 9. Primera cuenta (tú, como admin)
```bash
docker exec pulse /pulse invite create --role admin --email tu@correo.com --ttl 2h
```
Abre `https://pulse.espectro.uk/registro`, usa el código y configura tu passkey o app de autenticación.
Agrega en Uptime Kuma un monitor HTTP a `https://pulse.espectro.uk/healthz`.

---

## Si falla la verificación de la clave del servidor

`No ED25519 host key is known …` significa que el cliente no tiene una entrada válida para ese destino; no demuestra un fallo de la clave privada de acceso. Revisa los secrets del entorno `production`, separa host y puerto y guarda la clave ED25519 completa. Conserva `StrictHostKeyChecking=yes`.

Si la identidad del servidor cambia, verifica la nueva huella desde la consola del proveedor antes de sustituir el secret. Después de actualizar el secret, **Re-run failed jobs** repite el despliegue con la imagen ya construida.

`Load key …: error in libcrypto` corresponde a la lectura de `VPS_SSH_KEY`, una vez superada la comprobación del servidor. Revisa la clave privada con el paso 5 y vuelve a guardar el archivo completo en el secret. CI comprueba que se pueda leer sin contraseña antes de conectarse. Si no conservas el archivo privado válido, genera una identidad nueva con otro nombre, autoriza su pública en el VPS con la misma restricción y usa su privada en GitHub. No cambies `VPS_KNOWN_HOSTS` para corregir este error.

## Día a día

| Tarea | Cómo |
|---|---|
| Publicar una versión | `git push` a `main`. El job `deploy` queda en verde solo si la versión nueva pasó la prueba de humo |
| Ver qué corre | En la app: pie de página y **Ajustes › Versión** (`V<fecha y hora de compilación>` en tu zona horaria). En el VPS: `sudo -u deploy /opt/docker/pulse/deploy.sh status`. Para ver también el commit, el workflow debe pasar `build-args: PULSE_COMMIT=${{ github.sha }}` al construir la imagen |
| Historial de despliegues | `/opt/docker/pulse/deploy.log` |
| Volver a una versión anterior | En **Actions**, abre la ejecución de ese commit → **Re-run all jobs**. Si la versión vieja no puede abrir la base (esquema más nuevo), no arranca y `deploy.sh` deja la actual |
| Restaurar la base a mano | `docker compose --env-file release.env stop pulse && docker compose --env-file release.env run --rm --no-deps pulse restore /data/backups/<archivo>.db && docker compose --env-file release.env up -d` |
| Respaldo manual | `docker exec pulse /pulse backup /data/backups/$(date -u +%Y%m%dT%H%M%SZ)-manual.db` (local) · **Ajustes › Respaldo › Respaldar ahora** (cifrado, fuera del VPS) |
| Invitar a alguien | `docker exec pulse /pulse invite create --email x@y.com` |
| Ver / revocar invitaciones | `docker exec pulse /pulse invite list` · `docker exec pulse /pulse invite revoke <id>` |
| Usuarios | `docker exec pulse /pulse user list` · `user disable x@y.com` · `user reset-mfa x@y.com` |
| Derecho de acceso / portabilidad | `docker exec pulse /pulse user export x@y.com /data/export-x.json` → entrégalo por un canal seguro y bórralo: `sudo rm /opt/docker/pulse/data/export-x.json` |
| Derecho de supresión (baja) | `docker exec pulse /pulse user delete x@y.com --confirmar x@y.com` |
| Páginas legales | `/privacidad` y `/terminos` (`web/public/*.html`). Si cambias qué datos se tratan o su retención, actualízalas |
| Logs | `docker logs -f pulse` |
| Cambiar `.env` | `cd /opt/docker/pulse && sudo nano .env && docker compose --env-file release.env up -d` |

Los respaldos de `data/backups/` están en el mismo disco: protegen de un despliegue fallido, no de perder el VPS.

## Respaldos fuera del servidor (cifrados)

Desde la fase 12 el propio Pulse saca la copia fuera del VPS: hace una instantánea consistente (`VACUUM INTO`),
la cifra con [age](https://age-encryption.org) usando solo tu **clave pública** y la sube a un almacenamiento
S3 compatible (Backblaze B2, Cloudflare R2, Wasabi, MinIO, AWS) o a WebDAV (Nextcloud). Reintenta con espera
creciente si el destino no responde y, si falla o el último respaldo correcto es viejo, avisa por push a los
administradores. En **Ajustes › Respaldo** (solo admin) ves fecha, tamaño, huella y resultado de cada intento,
y con confirmación de identidad puedes **Respaldar ahora** o **Descargar la copia cifrada**.

Por qué es zero trust:

- El servidor no puede leer sus propios respaldos: tiene la clave pública, nunca la identidad age.
- La copia sin cifrar existe solo en `data/backups-externos/` mientras se cifra y se borra pase lo que pase.
- El destino recibe bytes cifrados y autenticados (alterarlos hace fallar el descifrado); la credencial
  basta con que pueda **escribir**, así que si roban el VPS no pueden borrar ni leer los respaldos anteriores.
- Para leer un respaldo hacen falta la identidad age **y** la KEK, y ninguna de las dos se guarda junto al respaldo.
- Las credenciales se leen de archivos (Docker secrets), nunca de variables de entorno ni de la base.
- En producción el destino debe ser `https://` y no se siguen redirecciones.

### Configurar

1. **En tu PC** (no en el VPS): `age-keygen -o pulse-backup.key`. Guarda ese archivo junto a la KEK, fuera del
   servidor y en dos lugares (p. ej. gestor de contraseñas y una memoria USB). Copia la línea `public key: age1…`.
   Puedes poner varias claves públicas separadas por comas (p. ej. la tuya y la de una persona de confianza).
2. Crea el destino:
   - **S3 compatible**: un bucket privado y una clave de acceso que **solo pueda escribir** en `pulse/`.
     En AWS la política mínima es:
     ```json
     {"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:PutObject","Resource":"arn:aws:s3:::<bucket>/pulse/*"}]}
     ```
     En B2/R2/Wasabi elige una clave «write only» limitada al bucket. Activa **Object Lock** o versionado si el
     proveedor lo ofrece, y una **regla de ciclo de vida** que borre objetos con más de 90 días (la retención la
     decide el bucket, no Pulse, para que el servidor no necesite permiso de borrar).
   - **WebDAV (Nextcloud)**: la carpeta `pulse-backups` y una **contraseña de aplicación** (Ajustes personales → Seguridad).
3. En el VPS guarda el secreto y agrega la configuración (no secreta) a `.env`:

```sh
cd /opt/docker/pulse
# S3: la clave secreta del bucket
printf '%s' '<secret-access-key>' | sudo tee secrets/backup_s3_secret >/dev/null
# o WebDAV: la contraseña de aplicación
# printf '%s' '<contraseña-de-aplicación>' | sudo tee secrets/backup_webdav_password >/dev/null
sudo chown 65532:65532 secrets/backup_* && sudo chmod 400 secrets/backup_*
sudo tee -a .env >/dev/null <<'ENV'
PULSE_BACKUP_TARGET=s3
PULSE_BACKUP_AGE_RECIPIENTS=age1…
PULSE_BACKUP_INTERVAL=24h
PULSE_BACKUP_S3_ENDPOINT=https://s3.us-west-004.backblazeb2.com
PULSE_BACKUP_S3_REGION=us-west-004
PULSE_BACKUP_S3_BUCKET=<bucket>
PULSE_BACKUP_S3_ACCESS_KEY_ID=<key-id>
ENV
```

Luego descomenta en `docker-compose.yml` las líneas del secreto que uses y `docker compose --env-file release.env up -d`.
En **Ajustes › Respaldo** pulsa **Respaldar ahora**: debe terminar en «Correcto» y el objeto aparecer en el bucket.

| Variable | Valor |
|---|---|
| `PULSE_BACKUP_AGE_RECIPIENTS` | una o más claves públicas `age1…` separadas por comas; con solo esto ya se respalda en la carpeta local |
| `PULSE_BACKUP_TARGET` | destinos externos además de la carpeta local: `gdrive`, `s3`, `webdav` (varios separados por comas); vacío = solo local |
| `PULSE_BACKUP_LOCAL_DIR` | carpeta local dentro del contenedor (`/backups`, montada desde `/opt/backup/pulse`); `off` = sin copia local |
| `PULSE_BACKUP_LOCAL_KEEP` | cuántos respaldos locales conservar (`30`; `0` = todos) |
| `PULSE_BACKUP_GDRIVE_CLIENT_ID` · `PULSE_BACKUP_GDRIVE_CLIENT_SECRET_FILE` · `PULSE_BACKUP_GDRIVE_TOKEN_FILE` | cliente OAuth «app de escritorio», archivo con su secreto y archivo con el refresh token de `pulse gdrive-auth` |
| `PULSE_BACKUP_GDRIVE_FOLDER` · `PULSE_BACKUP_GDRIVE_KEEP` | carpeta en Mi unidad (`Backups/pulse`) y cuántos conservar allí (`30`; los viejos van a la papelera de Drive) |
| `PULSE_BACKUP_INTERVAL` | cada cuánto (`24h` por defecto, entre `15m` y `168h`) o `manual` |
| `PULSE_BACKUP_DIR` | carpeta de trabajo (por defecto `/data/backups-externos`) |
| `PULSE_BACKUP_S3_ENDPOINT` · `_REGION` · `_BUCKET` · `_PREFIX` | URL del servicio, región (`us-east-1`), bucket y prefijo (`pulse/`) |
| `PULSE_BACKUP_S3_ACCESS_KEY_ID` · `PULSE_BACKUP_S3_SECRET_FILE` | id de la clave y archivo con el secreto (`/run/secrets/backup_s3_secret`) |
| `PULSE_BACKUP_S3_PATH_STYLE` | `true` (por defecto) o `false` para `bucket.endpoint` |
| `PULSE_BACKUP_WEBDAV_URL` · `_USER` · `PULSE_BACKUP_WEBDAV_PASSWORD_FILE` | carpeta WebDAV, usuario y archivo con la contraseña |

### Copia local en el servidor (`/opt/backup/pulse`)

Con solo `PULSE_BACKUP_AGE_RECIPIENTS` en `.env`, cada respaldo programado o manual deja además una copia **cifrada**
en `/opt/backup/pulse` del host (dentro del contenedor, `/backups`) y conserva las 30 más recientes. Sirve para
volver atrás rápido; no protege si se pierde el VPS, por eso conviene sumar un destino externo.

```sh
sudo install -d -o 65532 -g 65532 -m 700 /opt/backup/pulse
```

El volumen ya está en `docker-compose.yml` (`/opt/backup/pulse:/backups`). Para restaurar uno de estos archivos
sigue «Restaurar desde un respaldo externo» usando `/opt/backup/pulse/pulse-…db.age` como archivo de partida.

### Google Drive (`Backups/pulse`)

Pulse sube cada respaldo cifrado a la carpeta `Backups/pulse` de tu Mi unidad (la crea si falta; si ya tienes
una carpeta `Backups` creada a mano, Pulse no la ve y crea la suya propia, porque su permiso es solo
`drive.file`: ver y tocar **únicamente los archivos que él mismo creó**, nunca el resto de tu Drive).

Diferencia con S3: ese permiso también deja a Pulse borrar sus propios respaldos en Drive, así que no es «solo
escritura». Por eso los que pasan de `PULSE_BACKUP_GDRIVE_KEEP` van a la **papelera** (recuperables 30 días) y
la copia local sigue existiendo aparte.

1. En [Google Cloud Console](https://console.cloud.google.com/) (puede ser el mismo proyecto de Gmail):
   - **APIs y servicios › Biblioteca**: activa **Google Drive API**.
   - **Pantalla de consentimiento OAuth**: agrega el permiso `.../auth/drive.file` y pon el estado de
     publicación en **En producción**. Si queda en «Prueba», Google invalida el permiso a los 7 días y los
     respaldos a Drive empiezan a fallar. `drive.file` no necesita verificación de Google; solo verás una vez
     el aviso «Google no verificó esta app» → *Configuración avanzada* → *Ir a Pulse*.
   - **Credenciales › Crear credenciales › ID de cliente de OAuth › App de escritorio**. Copia el ID y el secreto.
2. En el VPS guarda el secreto del cliente:
   ```sh
   cd /opt/docker/pulse
   printf '%s' '<secreto-del-cliente>' | sudo tee secrets/backup_gdrive_client_secret >/dev/null
   sudo chown 65532:65532 secrets/backup_gdrive_client_secret && sudo chmod 400 secrets/backup_gdrive_client_secret
   ```
3. Autoriza una sola vez. Desde **tu PC** abre un túnel SSH (Google redirige a `127.0.0.1:53682` en tu navegador
   y el túnel lo lleva al servidor):
   ```sh
   ssh -L 53682:127.0.0.1:53682 matias@<servidor>
   # ya dentro del VPS:
   cd /opt/docker/pulse
   docker compose --env-file release.env run --rm --no-deps -p 127.0.0.1:53682:53682 \
     -v "$PWD/secrets/backup_gdrive_client_secret:/run/secrets/gcs:ro" \
     pulse gdrive-auth --client-id '<id>.apps.googleusercontent.com' --client-secret-file /run/secrets/gcs \
     --listen 0.0.0.0:53682 --out /data/gdrive_token
   sudo mv data/gdrive_token secrets/backup_gdrive_token && sudo chmod 400 secrets/backup_gdrive_token
   ```
   Abre en tu navegador la dirección que imprime, entra con tu cuenta de Google y acepta. La terminal dice
   «✓ Permiso guardado». Para revocarlo: [myaccount.google.com/permissions](https://myaccount.google.com/permissions).
4. Agrega a `.env` y descomenta en `docker-compose.yml` las dos líneas `PULSE_BACKUP_GDRIVE_*_FILE` y sus secretos:
   ```sh
   PULSE_BACKUP_AGE_RECIPIENTS=age1…
   PULSE_BACKUP_TARGET=gdrive
   PULSE_BACKUP_GDRIVE_CLIENT_ID=<id>.apps.googleusercontent.com
   PULSE_BACKUP_GDRIVE_FOLDER=Backups/pulse
   ```
5. `docker compose --env-file release.env up -d` y en **Ajustes › Respaldo** pulsa **Respaldar ahora**. Debe
   decir «Correcto» con destino «Este servidor · /backups + Google Drive · Backups/pulse», y el archivo aparecer
   en los dos sitios. Si Drive falla, la copia local igual queda guardada y el respaldo se marca como fallido
   (con aviso push) hasta que Drive vuelva a funcionar.

**Vigilancia**: `/healthz` incluye `"backup":"ok|pending|stale|failing|off"` (sin destino ni fechas). En Uptime
Kuma usa un monitor de tipo *HTTP(s) – Keyword* sobre `/healthz` con la palabra `"backup":"ok"`. Un respaldo
viejo no tumba el contenedor: reiniciarlo no lo arreglaría.

El script anterior `deploy/backup-offsite.sh` (cron en el host) sigue funcionando; si activas el respaldo
integrado puedes quitar su línea de `/etc/cron.d/pulse-backup`.

### Restaurar desde un respaldo externo

Necesitas: el archivo `.age` (del bucket, de Nextcloud o el que descargaste en Ajustes), `pulse-backup.key` y la KEK.

```sh
# 1. Descarga el objeto (elige uno; la credencial de escritura del VPS no sirve para leer: usa la tuya)
aws s3 cp s3://<bucket>/pulse/pulse-20261016T033000Z-v13.db.age . --endpoint-url https://…
# rclone copy b2:<bucket>/pulse/pulse-20261016T033000Z-v13.db.age .
# Ajustes › Respaldo › Descargar copia cifrada también sirve.

# 2. En el VPS (o en una máquina limpia con Pulse desplegado y la MISMA KEK)
cd /opt/docker/pulse
sudo install -o 65532 -g 65532 -m 400 ~/pulse-20261016T033000Z-v13.db.age data/restaurar.db.age
docker compose --env-file release.env stop pulse
docker compose --env-file release.env run --rm --no-deps \
  -v "$HOME/pulse-backup.key:/clave/age.txt:ro" \
  pulse restore --identity /clave/age.txt --sha256 <huella de Ajustes, opcional> /data/restaurar.db.age
docker compose --env-file release.env up -d && ./smoke.sh https://pulse.espectro.uk
sudo rm data/restaurar.db.age && shred -u ~/pulse-backup.key   # no dejes la identidad en el VPS
```

`pulse restore` descifra a un archivo privado dentro de `/data`, verifica la huella si la das, comprueba la
integridad de SQLite y borra el archivo descifrado al terminar; si el respaldo es de un esquema anterior, las
migraciones se aplican al arrancar. Si la identidad no corresponde o el archivo fue alterado, se detiene sin tocar la base.
También puedes descifrar en tu PC (`age -d -i pulse-backup.key x.db.age > restaurar.db`) y usar
`pulse restore /data/backups/restaurar.db` como antes.

La CI repite el recorrido completo (respaldo cifrado → base vacía → descifrar y `restore` → arranque sano) en
cada push con `go test ./internal/backup/...`.

`/healthz` responde 503 si la base no contesta o su esquema no coincide con el del binario; además indica si Gmail y push están activos (`"gmail"`, `"push"`). En Uptime Kuma usa un monitor HTTP con palabra clave `"status":"ok"`.
