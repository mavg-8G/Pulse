-- +up
-- Los correos sin "terminada en …" creaban una cuenta nueva por cada gasto (mismo banco, mismo nombre, sin dígitos).
-- Se conservan la más antigua de cada grupo y sus movimientos; las copias se borran. Las cuentas sin banco no se tocan.
CREATE TEMP TABLE account_merge AS
SELECT a.id AS dup,
       (SELECT k.id FROM accounts k
         WHERE k.user_id = a.user_id AND k.institution = a.institution AND k.name = a.name AND k.type = a.type AND k.card_suffix = ''
         ORDER BY k.archived, k.created_at, k.id LIMIT 1) AS keep
  FROM accounts a
 WHERE a.card_suffix = '' AND a.institution != '';
DELETE FROM account_merge WHERE dup = keep;
UPDATE transactions SET account_id = (SELECT keep FROM account_merge WHERE dup = account_id) WHERE account_id IN (SELECT dup FROM account_merge);
UPDATE recurring SET account_id = (SELECT keep FROM account_merge WHERE dup = account_id) WHERE account_id IN (SELECT dup FROM account_merge);
UPDATE OR IGNORE statements SET account_id = (SELECT keep FROM account_merge WHERE dup = account_id) WHERE account_id IN (SELECT dup FROM account_merge);
DELETE FROM statements WHERE account_id IN (SELECT dup FROM account_merge);
DELETE FROM accounts WHERE id IN (SELECT dup FROM account_merge);
DROP TABLE account_merge;

-- +down
-- Unir cuentas no se puede deshacer; bajar de versión no cambia nada.
SELECT 1;
