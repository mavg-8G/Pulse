-- +up
-- Frontera de compatibilidad: el binario migra los campos cifrados a AAD por fila
-- antes de servir peticiones. Los binarios anteriores deben rechazar este esquema.
SELECT 1;

-- +down
-- Bajar el esquema NO revierte el cifrado. Para volver a un binario anterior,
-- restaurar el respaldo anterior a esta migración junto con su clave.
SELECT 1;
