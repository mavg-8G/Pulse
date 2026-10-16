-- +up
-- Fusión lógica: una cuenta puede apuntar a otra principal de la misma persona. Ninguna se borra
-- (los correos siguen asignando gastos a cualquiera de las dos), pero los totales, reportes,
-- presupuestos y el filtro de movimientos las leen como una sola. Separar = volver a NULL.
ALTER TABLE accounts ADD COLUMN merged_into TEXT REFERENCES accounts (id) ON DELETE SET NULL;
CREATE INDEX accounts_merged_into ON accounts (merged_into) WHERE merged_into IS NOT NULL;

-- La principal debe ser de la misma persona, no ser ella misma, no estar vinculada a otra
-- y la cuenta vinculada no puede tener sus propias vinculadas: un solo nivel, sin ciclos.
CREATE TRIGGER accounts_merge_check_update BEFORE UPDATE OF merged_into ON accounts
WHEN NEW.merged_into IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'vinculación de cuentas inválida')
     WHERE NEW.merged_into = NEW.id
        OR NOT EXISTS (SELECT 1 FROM accounts p WHERE p.id = NEW.merged_into AND p.user_id = NEW.user_id AND p.merged_into IS NULL)
        OR EXISTS (SELECT 1 FROM accounts c WHERE c.merged_into = NEW.id);
END;

CREATE TRIGGER accounts_merge_check_insert BEFORE INSERT ON accounts
WHEN NEW.merged_into IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'vinculación de cuentas inválida')
     WHERE NEW.merged_into = NEW.id
        OR NOT EXISTS (SELECT 1 FROM accounts p WHERE p.id = NEW.merged_into AND p.user_id = NEW.user_id AND p.merged_into IS NULL);
END;

-- +down
DROP TRIGGER accounts_merge_check_insert;
DROP TRIGGER accounts_merge_check_update;
DROP INDEX accounts_merged_into;
ALTER TABLE accounts DROP COLUMN merged_into;
