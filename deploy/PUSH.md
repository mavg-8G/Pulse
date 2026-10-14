# Notificaciones de Pulse

La fase 5 incluye suscripciones por dispositivo, avisos de presupuesto, avisos de seguridad, resúmenes semanales y mensuales, privacidad de montos y horario silencioso.

## Configuración del servidor

Genera una pareja VAPID una sola vez con el binario compilado:

```sh
./pulse vapid --out ./secrets
```

El comando guarda `vapid_private` y muestra la clave pública. No sobrescribe una clave existente. Conserva ambas entre despliegues: si cambian, los dispositivos deberán desactivar y activar sus notificaciones.

Configura:

```env
PULSE_VAPID_PUBLIC_KEY=<clave pública>
PULSE_VAPID_PRIVATE_FILE=/run/secrets/vapid_private
PULSE_VAPID_SUBJECT=mailto:tu-correo@example.com
```

La clave privada se monta como Docker secret; nunca se pega en el frontend ni en `.env`. Si no se establece ninguna variable VAPID, Pulse funciona con push desactivado. Una configuración parcial o una pareja de claves incorrecta impide arrancar.

Para el compose de este proyecto, primero construye la imagen y genera la clave desde ella:

```sh
docker compose build
docker run --rm --user 0 -v "$PWD/secrets:/secrets" pulse:latest vapid --out /secrets
sudo chown 65532:65532 secrets/vapid_private
sudo chmod 400 secrets/vapid_private
# Copia la clave pública mostrada y tu correo a .env.
docker compose up -d
```

En Windows, `scripts/dev.ps1` prepara un entorno local, compila ambas partes, genera las claves si faltan y muestra una invitación. Los datos quedan en `.dev/`.

## Uso

En **Ajustes → Notificaciones**, activa este dispositivo, acepta el permiso del navegador y envía una notificación de prueba. En iPhone/iPad abre Pulse instalada en la pantalla de inicio. En producción se requiere HTTPS; localhost sirve para pruebas locales.

- Los umbrales de presupuesto se programan una vez por presupuesto, mes y umbral, usando los avisos de la fase 4.
- Los avisos de seguridad incluyen nuevos inicios de sesión, intentos fallidos, cambios de contraseña, MFA y códigos de recuperación.
- El resumen semanal se programa el domingo desde las 09:00 y cubre los siete días anteriores. El mensual se programa el día 1 desde las 09:00 y cubre el mes anterior. Se usa la zona horaria del usuario.
- Los montos están ocultos por defecto. La preferencia se comprueba al entregar, incluso si el aviso estaba en cola cuando cambió.
- El horario silencioso retrasa todos los avisos, incluso los de prueba y seguridad. Admite intervalos que cruzan medianoche.
- El servidor revisa la cola cada 10 segundos. Los errores de red, 429 y 5xx se reintentan con espera exponencial y `Retry-After`; 404/410 eliminan la suscripción. Los avisos vencen a las 48 horas.
- Una suscripción se asocia a una sesión completa. Cerrar, revocar o expirar esa sesión impide nuevos envíos. Para recibir avisos durante más tiempo, usa la opción de recordar dispositivo al iniciar sesión. Tras volver a entrar, activa de nuevo las notificaciones.
- Los endpoints y las claves del navegador, así como el contenido de la cola, se guardan cifrados con la DEK del usuario. El service worker confirma la identidad actual antes de mostrar un aviso, para descartar entregas en vuelo de una sesión anterior.

## Pruebas y aceptación

Las pruebas Go utilizan SQLite real, reloj falso y un servidor HTTP push de prueba. Verifican el cifrado Web Push descifrando el payload en el receptor, la deduplicación, aislamiento, privacidad, silencio, resúmenes, reintentos y cierre de sesión. Playwright verifica controles y el listener del service worker en escritorio y móvil, con autorización y navegación reales; sustituye el proveedor push y la superficie de notificaciones del sistema porque Chromium headless en Windows no la admite correctamente.

La aceptación final en un teléfono físico necesita el servidor configurado y un dispositivo suscrito: crea un presupuesto de prueba, alcanza el 80 % y comprueba que el aviso abre **Plan**. Las pruebas automatizadas no certifican la entrega del proveedor externo a un teléfono real.
