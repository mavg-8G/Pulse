-- +up
-- El registro de seguridad sigue siendo de solo inserción, salvo la purga por retención:
-- solo se pueden borrar filas con más de 365 días (ver store.AuditRetention y /privacidad).
-- created_at usa el formato de store.TS (2006-01-02T15:04:05Z, UTC), el mismo que genera
-- este strftime: por eso la comparación de texto equivale a la cronológica.
DROP TRIGGER audit_log_no_delete;
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
WHEN OLD.created_at >= strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-365 days')
BEGIN SELECT RAISE(ABORT, 'audit_log es solo de inserción'); END;

-- +down
DROP TRIGGER audit_log_no_delete;
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
BEGIN SELECT RAISE(ABORT, 'audit_log es solo de inserción'); END;
