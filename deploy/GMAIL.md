# Gmail en Pulse (fases 6 y 7)

Pulse lee **solo** las notificaciones de tus bancos y las convierte en gastos “Por revisar”.
Sin Gmail también puedes subir un correo `.eml` desde **Movimientos → Importar correo**.

## Qué hace y qué no hace

- Permiso `gmail.readonly`, el mínimo que permite leer el contenido de un correo.
- La búsqueda va filtrada **en Google** por remitente:
  `from:(servicios@tarjetasbancopichincha.com OR bancaenlinea@produbanco.com OR servicios@titanium.com.ec) newer_than:Nd`.
  Pulse nunca lista ni descarga otros correos.
- El correo se procesa en memoria y se descarta. Solo queda el HMAC del id del mensaje (para no repetirlo) y el gasto, con el comercio cifrado.
- El email de la cuenta y el refresh token se guardan cifrados con la clave de cada usuario.
- Una cuenta de Gmail solo puede estar conectada a un usuario de Pulse.
- **Modo sombra** (activado por defecto): todo lo importado queda “Por revisar”. Si lo apagas, solo se confirman solos los gastos que el parser leyó con seguridad **y** cuyo comercio tiene una regla tuya (“usar siempre esta categoría”).
- **Gemini** (opcional, desactivado): solo para correos de un banco soportado con una plantilla que Pulse no reconoce. Se activa por usuario con step-up. Antes de enviar se quitan el nombre del titular (también del asunto), los emails y las secuencias de 6+ dígitos. Lo que lee Gemini se valida igual que un parser (monto, fecha cercana a la del correo) y siempre queda por revisar.
- Desconectar detiene el aviso de Pub/Sub, revoca el token en Google y borra la conexión. Los gastos ya importados se quedan.

## 1. Proyecto en Google Cloud (una vez)

1. <https://console.cloud.google.com> → crea un proyecto (p. ej. `pulse-espectro`).
2. **APIs y servicios → Biblioteca**: habilita **Gmail API**. Si vas a usar tiempo real, habilita también **Cloud Pub/Sub API**.
3. **Pantalla de consentimiento de OAuth** → tipo *Externo*:
   - Nombre: Pulse. Correo de asistencia: el tuyo.
   - Dominios autorizados: `espectro.uk`.
   - Permisos: agrega `.../auth/gmail.readonly`.
   - **Publica la app (estado “En producción”)**. En “Pruebas” el refresh token caduca a los 7 días y tendrías que reconectar cada semana. Google mostrará “Google no ha verificado esta app” una vez por persona: es normal para una app personal (límite 100 usuarios).
4. **Credenciales → Crear credenciales → ID de cliente de OAuth** → *Aplicación web*:
   - URI de redireccionamiento autorizado: `https://pulse.espectro.uk/ajustes/gmail`
   - (Para probar en tu PC, añade también `http://localhost:8080/ajustes/gmail`.)
5. En Pulse, con tu cuenta **admin**: **Ajustes → Correo del banco (Gmail) → Configuración del servidor (admin)**. Pega el **ID de cliente** y el **secreto**, pulsa **Guardar credenciales** y confirma tu identidad. Se aplica al momento, sin reiniciar ni tocar el VPS. El secreto se guarda cifrado con la KEK y nunca se vuelve a mostrar; la misma pantalla te enseña la URI de redireccionamiento exacta para el paso 4.

<details><summary>Alternativa: variables de entorno (lo que hacías antes)</summary>

Si prefieres no guardarlo en la base, copia el **ID de cliente** a `.env` y el **secreto** a un Docker secret:

```sh
cd /opt/docker/pulse
printf '%s' 'GOCSPX-…' > secrets/google_client_secret
sudo chown 65532:65532 secrets/google_client_secret && sudo chmod 400 secrets/google_client_secret
```

```env
PULSE_GOOGLE_CLIENT_ID=1234567890-abc.apps.googleusercontent.com
```

Descomenta en `docker-compose.yml` `PULSE_GOOGLE_CLIENT_SECRET_FILE` y el secreto `google_client_secret`, y `docker compose up -d`. Lo guardado en Ajustes tiene prioridad sobre estas variables; **Borrar** en Ajustes vuelve a ellas.
</details>

Con esto ya funciona por **polling** (cada 15 min). En Pulse: **Ajustes → Correo del banco (Gmail) → Conectar Gmail**.

## 2. Tiempo real con Pub/Sub (opcional, < 1 min)

1. **Pub/Sub → Temas → Crear tema** `gmail-pulse` (sin suscripción predeterminada).
2. En el tema → **Permisos → Agregar principal** `gmail-api-push@system.gserviceaccount.com` con el rol **Publicador de Pub/Sub**. (Así Gmail puede publicar en tu tema.)
3. **IAM → Cuentas de servicio → Crear** `pubsub-push` (sin roles). Es la identidad que firma los avisos que recibe Pulse.
4. **Pub/Sub → Suscripciones → Crear**:
   - Tema: `gmail-pulse`. Tipo: **Push**.
   - Endpoint: `https://pulse.espectro.uk/api/gmail/webhook`
   - **Habilitar autenticación** → cuenta de servicio `pubsub-push@<proyecto>.iam.gserviceaccount.com`; audiencia: `https://pulse.espectro.uk/api/gmail/webhook`.
   - Si Google pide el rol *Creador de tokens de cuenta de servicio* para el agente de Pub/Sub, acéptalo.
5. En **Ajustes → Configuración del servidor (admin) → Tiempo real con Pub/Sub**, escribe el tema `projects/<proyecto>/topics/gmail-pulse` y la cuenta de servicio `pubsub-push@<proyecto>.iam.gserviceaccount.com` y guarda. (O, con variables de entorno, en `.env`:)

```env
PULSE_GMAIL_PUBSUB_TOPIC=projects/<proyecto>/topics/gmail-pulse
PULSE_GMAIL_PUBSUB_SA=pubsub-push@<proyecto>.iam.gserviceaccount.com
# PULSE_GMAIL_PUBSUB_AUDIENCE=https://pulse.espectro.uk/api/gmail/webhook  (es el valor por defecto)
```

Pulse llama a `users.watch` al sincronizar y lo renueva solo cuando faltan menos de 24 h (caduca a los 7 días).
Cada aviso trae un JWT firmado por Google; Pulse comprueba firma (claves públicas de Google, cacheadas), emisor,
audiencia, vigencia y que lo emitió **tu** cuenta de servicio. Sin eso responde 401 y no hace nada.
El polling sigue activo como respaldo.

## 3. Gemini (opcional)

1. Crea una clave en <https://aistudio.google.com/apikey>.
2. Pégala en **Ajustes → Configuración del servidor (admin) → Gemini** y guarda. (Alternativa: `printf '%s' 'AIza…' > secrets/gemini_key` con dueño 65532 y permisos 400, y descomenta `PULSE_GEMINI_KEY_FILE` y el secreto `gemini_key`.)
3. Cada persona lo activa en Ajustes (pide confirmar identidad).

## Problemas frecuentes

| Ajustes muestra | Qué pasó | Qué hacer |
|---|---|---|
| “Google retiró el acceso” | Quitaste el acceso en tu cuenta de Google, cambiaste la contraseña o la app quedó en “Pruebas” 7 días | Publica la app y pulsa **Reconectar** |
| “Google pidió esperar” / “Gmail no respondió” | Límite o caída temporal | Nada: Pulse reintenta con espera creciente (1 min → 6 h) |
| `redirect_uri_mismatch` en Google | La URI no coincide exactamente | Revisa el paso 1.4 (sin barra final) |
| Un consumo no aparece | Plantilla nueva del banco | `pulse parse --texto --titular "TU NOMBRE" correo.eml` y comparte la salida anonimizada |

Los avisos de seguridad (Gmail conectado, desconectado o sin acceso; Gemini activado) llegan por push y quedan en **Actividad de seguridad**.
