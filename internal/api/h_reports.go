package api

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pulse/internal/auth"
	"pulse/internal/budget"
	"pulse/internal/forecast"
	"pulse/internal/mailparse"
	"pulse/internal/push"
	"pulse/internal/store"
)

// ---------- reportes ----------

// monthSpan lee ?from=AAAA-MM&to=AAAA-MM (por defecto, los últimos 12 meses) y devuelve [inicio, fin) en UTC.
func (s *Server) monthSpan(r *http.Request, id *identity) (months []string, from, to time.Time, err error) {
	loc := s.loc(id)
	q := r.URL.Query()
	last := budget.MonthOf(s.clock.Now(), loc)
	if v := q.Get("to"); v != "" {
		last = v
	}
	end, err := time.ParseInLocation("2006-01", last, loc)
	if err != nil {
		return nil, from, to, badRequest("Mes 'hasta' inválido.")
	}
	start := end.AddDate(0, -11, 0)
	if v := q.Get("from"); v != "" {
		if start, err = time.ParseInLocation("2006-01", v, loc); err != nil {
			return nil, from, to, badRequest("Mes 'desde' inválido.")
		}
	}
	if start.After(end) || start.Year() < 2000 || end.Year() > 2200 || start.AddDate(3, 0, 0).Before(end) {
		return nil, from, to, badRequest("El rango debe ir de un mes a tres años.")
	}
	for m := start; !m.After(end); m = m.AddDate(0, 1, 0) {
		months = append(months, m.Format("2006-01"))
	}
	return months, start.UTC(), end.AddDate(0, 1, 0).UTC(), nil
}

func (s *Server) reportSummary(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ctx := r.Context()
	months, from, to, err := s.monthSpan(r, id)
	if err != nil {
		return err
	}
	cats, err := s.store.Categories(ctx, id.user.ID)
	if err != nil {
		return err
	}
	now := s.clock.Now().In(s.loc(id))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	historyFrom := today.AddDate(0, 0, -90)
	activity, err := s.store.ReportActivity(ctx, id.user.ID, from, to, historyFrom, now)
	if err != nil {
		return err
	}
	out := buildReportSummary(months, from, to, now, cats, activity)
	if out.Forecast.Ready {
		if err := s.horizonModels(r, id, today, cats, &out.Forecast); err != nil {
			return err
		}
	}
	writeJSON(w, 200, out)
	return nil
}

// horizonModels añade la proyección por validación (próxima semana y próximo mes) con su rango del 95 %.
func (s *Server) horizonModels(r *http.Request, id *identity, today time.Time, cats []store.Category, f *reportForecast) error {
	ctx := r.Context()
	loc := today.Location()
	txs, err := s.store.ProjectionActivity(ctx, id.user.ID, forecast.WindowStart(today).UTC(), s.clock.Now())
	if err != nil {
		return err
	}
	first, err := s.store.FirstActivity(ctx, id.user.ID)
	if err != nil {
		return err
	}
	last := max(f.WeekTo, f.MonthTo)
	upcoming, err := s.store.UpcomingRecurring(ctx, id.user.ID, today.AddDate(0, 0, 1).Format("2006-01-02"), last)
	if err != nil {
		return err
	}
	in := projectionInput(today, first, cats, txs, upcoming)
	parse := func(d string) time.Time {
		t, _ := time.ParseInLocation("2006-01-02", d, loc)
		return t
	}
	f.Week = forecast.Horizon(in, parse(f.WeekFrom), parse(f.WeekTo))
	f.Month = forecast.Horizon(in, parse(f.MonthFrom), parse(f.MonthTo))
	return nil
}

// csvSafe evita que una hoja de cálculo interprete un texto como fórmula (=, +, -, @).
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// reportExport descarga los movimientos del rango en CSV. Exige step-up: son todos tus datos de una vez.
func (s *Server) reportExport(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ctx := r.Context()
	months, from, to, err := s.monthSpan(r, id)
	if err != nil {
		return err
	}
	txs, err := s.store.Transactions(ctx, id.user.ID, id.seal(), store.TxFilter{From: from, To: to})
	if err != nil {
		return err
	}
	cats, err := s.store.Categories(ctx, id.user.ID)
	if err != nil {
		return err
	}
	accs, err := s.store.Accounts(ctx, id.user.ID, true)
	if err != nil {
		return err
	}
	catName, accName := map[string]string{}, map[string]string{}
	for _, c := range cats {
		catName[c.ID] = c.Name
	}
	for _, a := range accs {
		accName[a.ID] = a.Name
	}
	loc := s.loc(id)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pulse-%s-a-%s.csv"`, months[0], months[len(months)-1]))
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	cw.Write([]string{"fecha", "comercio", "categoria", "cuenta", "tipo", "monto", "moneda", "estado", "origen", "descripcion", "notas"})
	for _, t := range txs {
		kind := "compra"
		if t.Kind == "refund" {
			kind = "reverso"
		}
		cw.Write([]string{t.OccurredAt.In(loc).Format("2006-01-02 15:04"), csvSafe(t.Merchant), csvSafe(catName[t.CategoryID]), csvSafe(accName[t.AccountID]),
			kind, fmt.Sprintf("%d.%02d", t.AmountCents/100, t.AmountCents%100), t.Currency, t.Status, t.Source, csvSafe(t.Description), csvSafe(t.Notes)})
	}
	cw.Flush()
	s.audit(r, id.user.ID, "csv_exported", map[string]string{"from": months[0], "to": months[len(months)-1]})
	return cw.Error()
}

// ---------- conciliación ----------

// reconciliation compara lo registrado por cuenta en el mes con el total del estado de cuenta ingresado a mano.
// shortcut: usa el mes calendario, no el ciclo de corte de cada tarjeta; agregar el día de corte si las diferencias confunden.
func (s *Server) reconciliation(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ctx := r.Context()
	m, err := s.monthParam(r, id)
	if err != nil {
		return err
	}
	from, to, _ := budget.MonthRange(m, s.loc(id))
	recorded, err := s.store.SpentByAccount(ctx, id.user.ID, from, to)
	if err != nil {
		return err
	}
	stmts, err := s.store.Statements(ctx, id.user.ID, m)
	if err != nil {
		return err
	}
	accs, err := s.store.Accounts(ctx, id.user.ID, true)
	if err != nil {
		return err
	}
	type row struct {
		AccountID       string `json:"account_id"`
		RecordedCents   int64  `json:"recorded_cents"`
		StatementCents  *int64 `json:"statement_cents"`
		DifferenceCents *int64 `json:"difference_cents"` // estado − registrado: positivo = faltan gastos en Pulse
	}
	out := []row{}
	for _, a := range accs {
		st, has := stmts[a.ID]
		if a.Archived && !has && recorded[a.ID] == 0 {
			continue
		}
		x := row{AccountID: a.ID, RecordedCents: recorded[a.ID]}
		if has {
			d := st - x.RecordedCents
			x.StatementCents, x.DifferenceCents = &st, &d
		}
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"month": m, "accounts": out})
	return nil
}

func (s *Server) putStatement(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		AccountID  string `json:"account_id"`
		Month      string `json:"month"`
		TotalCents *int64 `json:"total_cents"` // null: borra el total de ese mes
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if _, _, err := budget.MonthRange(in.Month, s.loc(id)); err != nil {
		return badRequest("Mes inválido.")
	}
	if _, err := s.store.Account(r.Context(), id.user.ID, in.AccountID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return badRequest("La cuenta no existe.")
		}
		return err
	}
	if in.TotalCents == nil {
		if err := s.store.DeleteStatement(r.Context(), id.user.ID, in.AccountID, in.Month); err != nil {
			return err
		}
	} else {
		if *in.TotalCents < 0 || *in.TotalCents > maxAmount {
			return badRequest("Monto inválido.")
		}
		if err := s.store.SetStatement(r.Context(), id.user.ID, in.AccountID, in.Month, *in.TotalCents); err != nil {
			return err
		}
	}
	w.WriteHeader(204)
	return nil
}

// ---------- gastos recurrentes ----------

func (s *Server) listRecurring(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	list, err := s.store.Recurrings(r.Context(), id.user.ID, id.seal(), "")
	if err != nil {
		return err
	}
	writeJSON(w, 200, list)
	return nil
}

type recurringInput struct {
	ID          *string `json:"id"` // opcional al crear: lo genera el cliente para que la cola sin conexión no lo duplique
	AccountID   *string `json:"account_id"`
	CategoryID  *string `json:"category_id"`
	AmountCents *int64  `json:"amount_cents"`
	Merchant    *string `json:"merchant"`
	StartDate   *string `json:"start_date"` // solo al crear, AAAA-MM-DD: primer cobro; se repite cada mes ese día
	Day         *int    `json:"day"`        // solo al editar: nuevo día del mes (1–31)
}

func (s *Server) applyRecurring(r *http.Request, id *identity, in *recurringInput, rec *store.Recurring) error {
	ctx := r.Context()
	if in.AmountCents != nil {
		rec.AmountCents = *in.AmountCents
	}
	if rec.AmountCents <= 0 || rec.AmountCents > maxAmount {
		return badRequest("El monto debe ser mayor a cero.")
	}
	if in.Merchant != nil {
		m, err := cleanText(*in.Merchant, 120, "El comercio")
		if err != nil {
			return err
		}
		rec.Merchant = m
	}
	if in.AccountID != nil && *in.AccountID != rec.AccountID {
		if a, err := s.store.Account(ctx, id.user.ID, *in.AccountID); errors.Is(err, store.ErrNotFound) {
			return badRequest("La cuenta no existe.")
		} else if err != nil {
			return err
		} else if a.Archived {
			return badRequest("La cuenta está archivada.")
		}
		rec.AccountID = *in.AccountID
	}
	if in.CategoryID != nil && *in.CategoryID != rec.CategoryID {
		if _, err := s.store.Category(ctx, id.user.ID, *in.CategoryID); errors.Is(err, store.ErrNotFound) {
			return badRequest("La categoría no existe.")
		} else if err != nil {
			return err
		}
		rec.CategoryID = *in.CategoryID
	}
	if rec.AccountID == "" {
		return badRequest("La cuenta no existe.")
	}
	if rec.CategoryID == "" {
		return badRequest("La categoría no existe.")
	}
	return nil
}

func (s *Server) createRecurring(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ctx := r.Context()
	var in recurringInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Day != nil {
		return badRequest("El día sale de la fecha de inicio.")
	}
	rec := &store.Recurring{ID: auth.NewID()}
	if in.ID != nil {
		if !uuidRe.MatchString(*in.ID) {
			return badRequest("Identificador inválido.")
		}
		if prev, err := s.store.RecurringByID(ctx, id.user.ID, id.seal(), *in.ID); err == nil {
			writeJSON(w, 200, prev) // reenvío desde la cola: ya estaba creado
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		rec.ID = *in.ID
	}
	loc := s.loc(id)
	var startDate string
	if in.StartDate != nil {
		startDate = *in.StartDate
	}
	start, err := time.ParseInLocation("2006-01-02", startDate, loc)
	if err != nil || start.Year() < 2000 || start.After(s.clock.Now().AddDate(1, 0, 0)) {
		return badRequest("Fecha de inicio inválida.")
	}
	if err := s.applyRecurring(r, id, &in, rec); err != nil {
		return err
	}
	rec.Day, rec.NextDate = start.Day(), startDate
	if err := s.store.CreateRecurring(ctx, id.user.ID, id.seal(), rec); err != nil {
		return err
	}
	if err := s.runRecurring(ctx, id.user); err != nil { // si empieza hoy o antes, el primer gasto aparece ya
		return err
	}
	cur, err := s.store.RecurringByID(ctx, id.user.ID, id.seal(), rec.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 201, cur)
	return nil
}

// updateRecurring cambia monto, comercio, cuenta, categoría o día de un gasto fijo. Los gastos ya registrados no cambian.
// Con un día nuevo, el próximo cobro queda en el mismo mes que el pendiente; si ese día ya pasó, se registra al guardar.
func (s *Server) updateRecurring(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ctx := r.Context()
	rec, err := s.store.RecurringByID(ctx, id.user.ID, id.seal(), r.PathValue("id"))
	if err != nil {
		return err
	}
	expect, op, err := conditional(r)
	if err != nil {
		return err
	}
	var in recurringInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.ID != nil || in.StartDate != nil {
		return badRequest("Solo se pueden cambiar monto, comercio, cuenta, categoría y día.")
	}
	if op != "" && rec.LastOp == op {
		writeJSON(w, 200, rec)
		return nil
	}
	if expect != 0 && rec.Version != expect {
		return versionConflict(rec)
	}
	if err := s.applyRecurring(r, id, &in, rec); err != nil {
		return err
	}
	if in.Day != nil && *in.Day != rec.Day {
		if *in.Day < 1 || *in.Day > 31 {
			return badRequest("El día debe estar entre 1 y 31.")
		}
		next, _ := time.Parse("2006-01-02", rec.NextDate)
		last := time.Date(next.Year(), next.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		rec.Day = *in.Day
		rec.NextDate = time.Date(next.Year(), next.Month(), min(rec.Day, last), 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}
	if err := s.store.UpdateRecurring(ctx, id.user.ID, id.seal(), rec, expect, op); err != nil {
		var vc *store.VersionConflict
		if errors.As(err, &vc) {
			cur, cerr := s.store.RecurringByID(ctx, id.user.ID, id.seal(), rec.ID)
			if cerr != nil {
				return cerr
			}
			return versionConflict(cur)
		}
		return err
	}
	if err := s.runRecurring(ctx, id.user); err != nil {
		return err
	}
	cur, err := s.store.RecurringByID(ctx, id.user.ID, id.seal(), rec.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, cur)
	return nil
}

func (s *Server) deleteRecurring(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	expect, _, err := conditional(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteRecurring(r.Context(), id.user.ID, r.PathValue("id"), expect); err != nil {
		var vc *store.VersionConflict
		if errors.As(err, &vc) {
			cur, cerr := s.store.RecurringByID(r.Context(), id.user.ID, id.seal(), r.PathValue("id"))
			if cerr != nil {
				return cerr
			}
			return versionConflict(cur)
		}
		return err
	}
	w.WriteHeader(204)
	return nil
}

// nextMonthly devuelve la fecha del mes siguiente con el mismo día (o el último día si ese mes es más corto).
func nextMonthly(date string, day int) string {
	t, _ := time.Parse("2006-01-02", date)
	first := time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(day, last), 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// runRecurring registra los gastos recurrentes vencidos del usuario. Es idempotente: cada cobro lleva
// un hash (recurrente + fecha), así que un reinicio a mitad de camino no lo duplica.
func (s *Server) runRecurring(ctx context.Context, u *store.User) error {
	sl, err := s.keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		return err
	}
	loc := s.userLoc(u)
	today := s.clock.Now().In(loc).Format("2006-01-02")
	due, err := s.store.Recurrings(ctx, u.ID, sl, today)
	if err != nil {
		return err
	}
	var cats []string
	var whens []time.Time
	for _, rec := range due {
		next := rec.NextDate
		for i := 0; next <= today && i < 12; i++ { // como mucho un año de cobros atrasados
			d, _ := time.ParseInLocation("2006-01-02", next, loc)
			t := &store.Transaction{ID: auth.NewID(), AccountID: rec.AccountID, CategoryID: rec.CategoryID, AmountCents: rec.AmountCents,
				Currency: "USD", OccurredAt: d.Add(12 * time.Hour).UTC(), Kind: "purchase", Merchant: rec.Merchant,
				Description: "Gasto recurrente", Source: "manual", Status: "confirmed",
				MessageHash: s.keys.Lookup("recurring", []byte(u.ID+"|"+rec.ID+"|"+next))}
			if err := s.store.CreateTransaction(ctx, u.ID, sl, t); err != nil && !errors.Is(err, store.ErrDuplicate) {
				return err
			}
			cats, whens = append(cats, rec.CategoryID), append(whens, t.OccurredAt)
			next = nextMonthly(next, rec.Day)
		}
		if err := s.store.AdvanceRecurring(ctx, u.ID, rec.ID, rec.NextDate, next); err != nil {
			return err
		}
	}
	if len(cats) > 0 {
		return s.RecheckBudgets(ctx, u, cats, whens)
	}
	return nil
}

// RunRecurring revisa cada hora los gastos recurrentes de todos los usuarios.
func (s *Server) RunRecurring(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		users, err := s.store.ListUsers(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Error("gastos recurrentes", "err", err.Error())
		}
		for i := range users {
			if users[i].DisabledAt != nil {
				continue
			}
			if err := s.runRecurring(ctx, &users[i]); err != nil && ctx.Err() == nil {
				s.log.Error("gastos recurrentes", "user", users[i].ID, "err", err.Error())
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---------- detector de plantillas ----------

// TemplateChanged avisa por push a los administradores: un banco pudo cambiar su plantilla.
// El aviso no lleva el asunto ni nada del correo; uno por banco y día.
func (s *Server) TemplateChanged(ctx context.Context, u *store.User, parser string) {
	s.store.Audit(ctx, u.ID, "template_unknown", "", "pulse", map[string]string{"parser": parser})
	if s.push == nil {
		return
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.log.Error("avisando plantilla desconocida", "err", err.Error())
		return
	}
	day := s.clock.Now().UTC().Format("2006-01-02")
	for _, a := range users {
		if a.Role != "admin" || a.DisabledAt != nil {
			continue
		}
		p := push.Payload{Title: "Plantilla de correo desconocida", URL: "/ajustes",
			Body: fmt.Sprintf("Llegó un correo de %s que Pulse no supo leer. Puede que el banco haya cambiado su plantilla.", mailparse.Bank(parser))}
		if err := s.push.Notify(ctx, a.ID, "template:"+parser+":"+day, "security", p); err != nil {
			s.log.Error("avisando plantilla desconocida", "err", err.Error())
		}
	}
}
