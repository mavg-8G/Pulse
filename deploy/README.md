# Despliegue en el VPS (Docker + Caddy)

Pulse corre como **un solo contenedor** (`pulse`) en la red `proxy`, detrás de Caddy, en `https://pulse.espectro.uk`.

## 1. DNS
Registro **A** `pulse` → IP del VPS.

## 2. Preparar la carpeta
```bash
sudo mkdir -p /opt/docker/pulse && cd /opt/docker/pulse
sudo git clone <tu-repo> src
sudo cp src/deploy/docker-compose.yml .
sudo cp src/deploy/.env.example .env

# Datos: el contenedor corre como uid 65532 (sin privilegios)
sudo install -d -o 65532 -g 65532 -m 700 data
```

## 3. Clave maestra (KEK)
Cifra los datos sensibles de todos los usuarios. **Sin ella, los datos y backups son irrecuperables.**
```bash
sudo install -d -m 700 secrets
openssl rand -base64 32 | sudo tee secrets/pulse_kek > /dev/null
sudo chown 65532:65532 secrets/pulse_kek && sudo chmod 400 secrets/pulse_kek
sudo cat secrets/pulse_kek   # → cópiala a tu gestor de contraseñas y no la pegues en otro lado
```

## 4. Construir y arrancar
```bash
sudo docker compose build      # corre los tests de Go dentro del build: si fallan, no hay imagen
# Genera el secreto VAPID y configura las notificaciones según PUSH.md antes de arrancar.
sudo docker run --rm --user 0 -v "$PWD/secrets:/secrets" pulse:latest vapid --out /secrets
sudo chown 65532:65532 secrets/vapid_private && sudo chmod 400 secrets/vapid_private
# Copia PULSE_VAPID_PUBLIC_KEY (mostrada por el comando) y tu correo en .env.
sudo docker compose up -d
sudo docker compose ps         # debe quedar "healthy"
```

## 5. Caddy
Pega `src/deploy/Caddyfile.snippet` en tu Caddyfile y recarga:
```bash
docker exec caddy caddy reload --config /etc/caddy/Caddyfile
```

## 6. Primera cuenta (tú, como admin)
```bash
docker exec pulse /pulse invite create --role admin --email tu@correo.com --ttl 2h
```
Abre `https://pulse.espectro.uk/registro`, usa el código y configura tu passkey o app de autenticación.

## 7. Verificar
```bash
src/deploy/smoke.sh https://pulse.espectro.uk
```
Agrega en Uptime Kuma un monitor HTTP a `https://pulse.espectro.uk/healthz`.

## Operación diaria
| Tarea | Comando |
|---|---|
| Invitar a alguien | `docker exec pulse /pulse invite create --email x@y.com` |
| Ver invitaciones | `docker exec pulse /pulse invite list` |
| Revocar invitación | `docker exec pulse /pulse invite revoke <id>` |
| Usuarios | `docker exec pulse /pulse user list` |
| Bloquear a alguien | `docker exec pulse /pulse user disable x@y.com` (cierra sus sesiones) |
| Perdió su 2.º factor | `docker exec pulse /pulse user reset-mfa x@y.com` (al entrar configura uno nuevo) |
| Actualizar | `cd src && git pull && cd .. && docker compose build && docker compose up -d && src/deploy/smoke.sh` |
| Logs | `docker logs -f pulse` |

Los backups cifrados automáticos llegan en la Fase 8; mientras tanto:
```bash
docker exec pulse /pulse migrate   # no-op, valida que la base abre
sudo sqlite3 data/pulse.db ".backup '/root/pulse-$(date +%F).db'"
```
