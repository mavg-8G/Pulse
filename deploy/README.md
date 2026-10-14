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
VER=v2.6.5
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
En el VPS, autoriza la clave **pública** (`pulse_deploy.pub`) solo para `deploy.sh`:
```bash
sudo install -d -o deploy -g deploy -m 700 /home/deploy/.ssh
echo 'command="/opt/docker/pulse/deploy.sh",restrict ssh-ed25519 AAAA…contenido de pulse_deploy.pub… github-actions-pulse' \
  | sudo tee /home/deploy/.ssh/authorized_keys > /dev/null
sudo chown deploy:deploy /home/deploy/.ssh/authorized_keys && sudo chmod 600 /home/deploy/.ssh/authorized_keys
```
Huella del servidor (para que Actions no se conecte a un impostor). En tu PC:
```bash
ssh-keyscan -p 22 -t ed25519 <IP-del-VPS>
```
y compárala con la del VPS: `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` (mismo `SHA256:…` que `ssh-keyscan … | ssh-keygen -lf -`).

Prueba desde tu PC: `ssh -i pulse_deploy deploy@<IP> status` → debe responder `imagen:` vacía y `estado: ausente`.
Cualquier otro comando (`ssh -i pulse_deploy deploy@<IP> bash`) debe responder “acción no permitida”.

### 6. GitHub
En el repositorio → **Settings → Environments → New environment** → `production`:
- **Deployment branches and tags**: *Selected branches* → `main`.
- (Opcional) **Required reviewers**: tú, si quieres aprobar cada despliegue con un clic.
- **Environment secrets**:

| Nombre | Valor |
|---|---|
| `VPS_HOST` | IP o nombre del VPS |
| `VPS_PORT` | `22` (o tu puerto SSH) |
| `VPS_SSH_KEY` | contenido completo de `pulse_deploy` (la **privada**) |
| `VPS_KNOWN_HOSTS` | la línea que devolvió `ssh-keyscan` |

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

## Día a día

| Tarea | Cómo |
|---|---|
| Publicar una versión | `git push` a `main`. El job `deploy` queda en verde solo si la versión nueva pasó la prueba de humo |
| Ver qué corre | `sudo -u deploy /opt/docker/pulse/deploy.sh status` |
| Historial de despliegues | `/opt/docker/pulse/deploy.log` |
| Volver a una versión anterior | En **Actions**, abre la ejecución de ese commit → **Re-run all jobs**. Si la versión vieja no puede abrir la base (esquema más nuevo), no arranca y `deploy.sh` deja la actual |
| Restaurar la base a mano | `docker compose --env-file release.env stop pulse && docker compose --env-file release.env run --rm --no-deps pulse restore /data/backups/<archivo>.db && docker compose --env-file release.env up -d` |
| Respaldo manual | `docker exec pulse /pulse backup /data/backups/$(date -u +%Y%m%dT%H%M%SZ)-manual.db` |
| Invitar a alguien | `docker exec pulse /pulse invite create --email x@y.com` |
| Ver / revocar invitaciones | `docker exec pulse /pulse invite list` · `docker exec pulse /pulse invite revoke <id>` |
| Usuarios | `docker exec pulse /pulse user list` · `user disable x@y.com` · `user reset-mfa x@y.com` |
| Logs | `docker logs -f pulse` |
| Cambiar `.env` | `cd /opt/docker/pulse && sudo nano .env && docker compose --env-file release.env up -d` |

Los respaldos de `data/backups/` están en el mismo disco: protegen de un despliegue fallido, no de perder el VPS.
Los backups cifrados fuera del servidor llegan en la Fase 8; mientras tanto copia de vez en cuando
`data/backups/` (y guarda la KEK aparte: sin ella no sirven).
