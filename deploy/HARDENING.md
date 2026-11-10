# Aplicar el endurecimiento en una instalación existente

Los cambios de código no cambian la configuración del VPS ni los permisos de GitHub. Aplicar estos pasos desde una consola administrativa confiable, con un respaldo verificado y acceso de recuperación. No se ejecutaron contra producción durante la corrección.

## Compatibilidad de esta actualización

- Las passkeys requieren verificación local (PIN o biometría). Una llave sin esa capacidad debe sustituirse o complementarse con TOTP antes del despliegue; conservar los códigos de recuperación fuera del dispositivo.
- La migración 0015 identifica un nuevo formato de cifrado. El arranque convierte movimientos, recurrentes y asuntos de alertas en lotes transaccionales reanudables. Los binarios anteriores no leen ese formato: para retroceder hay que restaurar el respaldo anterior, no solo la imagen.
- El navegador migra su caché y cambios pendientes al verificar una sesión completa. La clave ya no se persiste en IndexedDB. Una pestaña desbloqueada conserva uso offline; recargar sin red exige reconectar. Se bloquea tras 15 minutos sin interacción. No borrar el almacenamiento del navegador si quedan cambios pendientes.
- Todos los correos importados requieren revisión manual. El remitente visible, las cabeceras aportadas por el archivo y el historial del parser no acreditan autenticidad.
- Consultas que descifran más de 10.000 movimientos devuelven `query_limit`; reducir periodo o filtros. Las lecturas financieras/importaciones pesadas comparten un máximo de dos trabajos simultáneos y 25 segundos por solicitud.

## Red exclusiva con Caddy

1. Crear una red dedicada: `sudo docker network create pulse_proxy`.
2. Añadir `pulse_proxy` como red externa del compose de **Caddy**, manteniendo las redes de sus otros servicios. Conectar su contenedor en ejecución a esa red antes del cambio de Pulse: `sudo docker network connect pulse_proxy caddy`.
3. Consultar la IP de Caddy en esa red mediante `sudo docker inspect caddy`. Reservar esa IP en el compose de Caddy para que no cambie al recrearlo.
4. En la configuración de Pulse establecer `PULSE_PROXY_NETWORK=pulse_proxy` y `PULSE_TRUSTED_PROXY=<IP-reservada-de-Caddy>/32` (o `/128` en IPv6). No confiar en toda una subred compartida.
5. Aplicar el compose de Pulse usando `.env` para interpolación y `release.env` para el digest, por ejemplo `sudo docker compose --env-file .env --env-file release.env up -d --wait pulse`. Comprobar HTTPS, IP auditada y que un contenedor ajeno no tenga acceso directo a Pulse.

El `env_file: .env` del servicio solo configura su proceso: no basta para interpolar el nombre de red en Compose. El script de despliegue carga ambos archivos explícitamente. Conservar la definición de red en Caddy para que sobreviva a su siguiente recreación.

## Quitar a deploy del grupo Docker

La clave SSH sigue limitada a un comando forzado y las imágenes siguen verificándose con cosign. Para evitar acceso general al daemon desde la cuenta deploy:

1. Hacer `/opt/docker/pulse` y sus scripts propiedad de root, sin escritura para deploy; mantener los directorios `data` y `secrets` con los propietarios que necesita el contenedor. El directorio principal puede ser `root:root 0755`; `deploy.sh` y `smoke.sh`, `root:root 0755`. Revisar que tampoco se puedan sustituir desde sus directorios padres.
2. Crear `/etc/sudoers.d/pulse-deploy`, root:root 0440, usando `visudo -f`:

   ```sudoers
   Defaults:deploy env_reset
   Defaults:deploy secure_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
   Defaults:deploy env_keep += "SSH_ORIGINAL_COMMAND"
   deploy ALL=(root) NOPASSWD: /opt/docker/pulse/deploy.sh ""
   ```

   `""` limita la regla a invocación sin argumentos. Solo `SSH_ORIGINAL_COMMAND` se conserva para que el script valide acción, digest y commit; no añadir `SETENV` ni preservar `PULSE_DEPLOY_DIR`, `PATH`, `BASH_ENV` u otras variables controlables.
3. Cambiar la entrada de la clave pública en `authorized_keys` a `command="sudo -n /opt/docker/pulse/deploy.sh",restrict ssh-ed25519 ...`. Proteger archivo y carpeta `.ssh` contra cambios de otros usuarios; si se administran como root, OpenSSH permite archivos de claves propiedad de root.
4. Si GHCR es privado, configurar las credenciales de solo lectura del registro para **root**, que ejecutará el script. No imprimir tokens.
5. Validar `visudo -c`, `ssh ... status`, rechazo de `ssh ... bash` y rechazo de un digest no firmado, antes de retirar el acceso anterior. Después quitar deploy del grupo con `sudo gpasswd -d deploy docker` y cerrar sesiones anteriores para eliminar grupos heredados.

No dar permiso sudo genérico para docker, bash, compose o editores. Un script privilegiado editable por deploy anula esta separación.

## Rotación de claves de envoltura

`pulse rotate-keys --out-db copia-rotada.db --out-key llavero-rotado.json` prepara una **pareja nueva** sin reemplazar la base ni el secreto activos. Usar rutas nuevas: no sobrescribe archivos existentes.

1. Programar mantenimiento y detener el servidor **antes** de preparar la copia final para no perder escrituras posteriores a la instantánea. Conservar fuera del VPS un respaldo y su clave actual.
2. Ejecutar la CLI con la configuración y secreto actuales. Validar la pareja de salida en un entorno aislado: arranque, contraseña+MFA, datos financieros, Gmail y restauración.
3. Con el servicio detenido, instalar juntos la base nueva y el llavero nuevo en sus rutas configuradas, con los permisos del contenedor. No mezclar base nueva con clave antigua ni a la inversa. Si falla la preparación, no activar ninguno de sus archivos de salida.
4. Reiniciar, verificar y crear un nuevo respaldo externo. Las sesiones anteriores se revocan; las contraseñas y los factores se conservan. Guardar cada respaldo junto con la referencia a su generación de clave, separando sus secretos.

El llavero v2 tiene identificador y claves independientes para envoltura, ajustes, índices y pepper. Esta rotación cambia envoltura y ajustes y reenvuelve las DEK. Conserva índices y pepper para mantener búsquedas, contraseñas y almacenes locales: **no es recuperación completa de una KEK comprometida**. Ante compromiso hay que revocar credenciales, reconstruir el host y planificar también rotación de DEK, pepper/contraseñas e índices. Un KMS tampoco impide que un proceso autorizado comprometido use sus permisos.

## Evidencia y operación pendientes de configuración

- Proteger main y cambios de workflows/seguridad; comprobar permisos reales del entorno production.
- Exportar logs/auditoría a un destino independiente. Alertar por `audit_write_failed` y por `"audit":"failing"` en `/healthz` (sin convertir esa alerta por sí sola en reinicios automáticos).
- Respaldos con retención independiente o inmutabilidad, y simulacro age + clave + login/datos en una máquina limpia. Acordar RPO/RTO.
- Pentest autenticado independiente y DAST en staging por roles, sin datos reales.
- Las bases de Docker y ZAP están fijadas a digests verificados. Actualizar tag y digest juntos; el Dockerfile rechaza versiones de Go/Node que no coincidan.
- npm audit incluye herramientas de construcción y corre de noche. El aviso bajo de deepmerge transitivo no tenía versión corregida al verificar esta entrega; no usar `audit fix --force` para degradar PWA. Revisar con cada actualización; no hay excepciones que oculten alertas altas.
