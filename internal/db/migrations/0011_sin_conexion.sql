-- +up
-- Cola sin conexión (fase 10): concurrencia optimista. Cada cambio hecho por el usuario sube la versión;
-- un cambio en cola que se envía con una versión vieja se rechaza como conflicto en vez de pisar otro.
-- last_op guarda el identificador de la última operación del cliente: reenviarla (respuesta perdida) no es conflicto.
ALTER TABLE transactions ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE transactions ADD COLUMN last_op TEXT NOT NULL DEFAULT '';
ALTER TABLE recurring ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE recurring ADD COLUMN last_op TEXT NOT NULL DEFAULT '';

-- +down
ALTER TABLE recurring DROP COLUMN last_op;
ALTER TABLE recurring DROP COLUMN version;
ALTER TABLE transactions DROP COLUMN last_op;
ALTER TABLE transactions DROP COLUMN version;
