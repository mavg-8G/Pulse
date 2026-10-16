package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pulse/internal/auth"
	"pulse/internal/budget"
	"pulse/internal/importer"
	"pulse/internal/store"
)

var (
	reColor    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	reSuffix   = regexp.MustCompile(`^[0-9]{3,4}$`)
	reCurrency = regexp.MustCompile(`^[A-Z]{3}$`)
	accTypes   = map[string]bool{"bank": true, "credit_card": true, "debit_card": true, "cash": true, "wallet": true}
)

const maxAmount = 100_000_000 // USD 1.000.000,00

func (s *Server) loc(id *identity) *time.Location { return s.userLoc(id.user) }

func (s *Server) userLoc(u *store.User) *time.Location {
	l, err := time.LoadLocation(u.Timezone)
	if err != nil {
		l, _ = time.LoadLocation(s.cfg.DefaultTZ)
	}
	return l
}

// ---------------- Cuentas ----------------

type accountInput struct {
	Name        *string `json:"name"`
	Type        *string `json:"type"`
	Institution *string `json:"institution"`
	CardSuffix  *string `json:"card_suffix"`
	Currency    *string `json:"currency"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
	Archived    *bool   `json:"archived"`
}

func (in *accountInput) apply(a *store.Account) error {
	var err error
	if in.Name != nil {
		if a.Name, err = cleanText(*in.Name, 60, "El nombre"); err != nil {
			return err
		}
	}
	if in.Type != nil {
		a.Type = *in.Type
	}
	if in.Institution != nil {
		if a.Institution, err = cleanText(*in.Institution, 60, "La institución"); err != nil {
			return err
		}
	}
	if in.CardSuffix != nil {
		a.CardSuffix = strings.TrimSpace(*in.CardSuffix)
	}
	if in.Currency != nil {
		a.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if in.Color != nil {
		a.Color = *in.Color
	}
	if in.Icon != nil {
		if a.Icon, err = cleanText(*in.Icon, 8, "El ícono"); err != nil {
			return err
		}
	}
	if in.Archived != nil {
		a.Archived = *in.Archived
	}
	switch {
	case a.Name == "":
		return badRequest("La cuenta necesita un nombre.")
	case !accTypes[a.Type]:
		return badRequest("Tipo de cuenta inválido.")
	case a.CardSuffix != "" && !reSuffix.MatchString(a.CardSuffix):
		return badRequest("Los últimos dígitos de la tarjeta deben ser 3 o 4 números.")
	case !reCurrency.MatchString(a.Currency):
		return badRequest("Moneda inválida (código de 3 letras, ej. USD).")
	case !reColor.MatchString(a.Color):
		return badRequest("Color inválido (formato #RRGGBB).")
	}
	return nil
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) error {
	as, err := s.store.Accounts(r.Context(), ident(r).user.ID, r.URL.Query().Get("archived") == "1")
	if err != nil {
		return err
	}
	writeJSON(w, 200, as)
	return nil
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) error {
	var in accountInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	a := &store.Account{ID: auth.NewID(), Currency: "USD", Color: "#64748b"}
	if err := in.apply(a); err != nil {
		return err
	}
	if err := s.store.CreateAccount(r.Context(), ident(r).user.ID, a); err != nil {
		return err
	}
	writeJSON(w, 201, a)
	return nil
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	a, err := s.store.Account(r.Context(), id.user.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	var in accountInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := in.apply(a); err != nil {
		return err
	}
	if err := s.store.UpdateAccount(r.Context(), id.user.ID, a); err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteAccount(r.Context(), ident(r).user.ID, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

// ---------------- Categorías ----------------

type categoryInput struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
	Color    *string `json:"color"`
	Icon     *string `json:"icon"`
	Archived *bool   `json:"archived"`
}

func (s *Server) applyCategory(r *http.Request, id *identity, in *categoryInput, c *store.Category) error {
	var err error
	if in.Name != nil {
		if c.Name, err = cleanText(*in.Name, 40, "El nombre"); err != nil {
			return err
		}
	}
	if in.Color != nil {
		c.Color = *in.Color
	}
	if in.Icon != nil {
		if c.Icon, err = cleanText(*in.Icon, 8, "El ícono"); err != nil {
			return err
		}
	}
	if in.Archived != nil {
		c.Archived = *in.Archived
	}
	if in.ParentID != nil && *in.ParentID != c.ParentID {
		p := *in.ParentID
		if p != "" {
			if c.IsSystem {
				return badRequest("La categoría del sistema no puede ser subcategoría.")
			}
			parent, err := s.store.Category(r.Context(), id.user.ID, p)
			if errors.Is(err, store.ErrNotFound) {
				return badRequest("La categoría padre no existe.")
			} else if err != nil {
				return err
			}
			if parent.ParentID != "" || parent.ID == c.ID {
				return badRequest("Solo se permite un nivel de subcategorías.")
			}
			if c.ID != "" {
				if has, err := s.store.HasChildren(r.Context(), id.user.ID, c.ID); err != nil {
					return err
				} else if has {
					return badRequest("Una categoría con subcategorías no puede volverse subcategoría.")
				}
			}
		}
		c.ParentID = p
	}
	if c.Name == "" {
		return badRequest("La categoría necesita un nombre.")
	}
	if !reColor.MatchString(c.Color) {
		return badRequest("Color inválido (formato #RRGGBB).")
	}
	return nil
}

func (s *Server) listCategories(w http.ResponseWriter, r *http.Request) error {
	cs, err := s.store.Categories(r.Context(), ident(r).user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, cs)
	return nil
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in categoryInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	c := &store.Category{Color: "#64748b"}
	if err := s.applyCategory(r, id, &in, c); err != nil {
		return err
	}
	c.ID = auth.NewID()
	if err := s.store.CreateCategory(r.Context(), id.user.ID, c); err != nil {
		return err
	}
	writeJSON(w, 201, c)
	return nil
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	c, err := s.store.Category(r.Context(), id.user.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	var in categoryInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.applyCategory(r, id, &in, c); err != nil {
		return err
	}
	if err := s.store.UpdateCategory(r.Context(), id.user.ID, c); err != nil {
		return err
	}
	writeJSON(w, 200, c)
	return nil
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) error {
	moved, err := s.store.DeleteCategory(r.Context(), ident(r).user.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"moved_transactions": moved})
	return nil
}

// ---------------- Movimientos ----------------

type txInput struct {
	AccountID   *string `json:"account_id"`
	CategoryID  *string `json:"category_id"`
	AmountCents *int64  `json:"amount_cents"`
	OccurredAt  *string `json:"occurred_at"`
	Kind        *string `json:"kind"`
	Merchant    *string `json:"merchant"`
	Description *string `json:"description"`
	Notes       *string `json:"notes"`
	Status      *string `json:"status"`
	// ID opcional al crear: lo genera el cliente para que reenviar un gasto hecho sin conexión no lo duplique.
	ID *string `json:"id"`
	// LearnRule: "usar siempre esta categoría para este comercio" (solo al editar).
	LearnRule bool `json:"learn_rule"`
}

// parseWhen acepta RFC3339 con zona, "AAAA-MM-DDTHH:MM" o "AAAA-MM-DD" (hora local del usuario).
func parseWhen(v string, loc *time.Location) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC().Truncate(time.Second), true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			if layout == "2006-01-02" {
				t = t.Add(12 * time.Hour)
			}
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (s *Server) applyTx(r *http.Request, id *identity, in *txInput, t *store.Transaction, creating bool) error {
	ctx := r.Context()
	var err error
	if in.AccountID != nil && *in.AccountID != t.AccountID {
		a, err := s.store.Account(ctx, id.user.ID, *in.AccountID)
		if errors.Is(err, store.ErrNotFound) {
			return badRequest("La cuenta no existe.")
		} else if err != nil {
			return err
		}
		if a.Archived {
			return badRequest("La cuenta está archivada.")
		}
		t.AccountID, t.Currency = a.ID, a.Currency
	}
	if in.CategoryID != nil && *in.CategoryID != t.CategoryID {
		if _, err := s.store.Category(ctx, id.user.ID, *in.CategoryID); errors.Is(err, store.ErrNotFound) {
			return badRequest("La categoría no existe.")
		} else if err != nil {
			return err
		}
		t.CategoryID = *in.CategoryID
	}
	if in.AmountCents != nil {
		t.AmountCents = *in.AmountCents
	}
	if in.OccurredAt != nil {
		when, ok := parseWhen(*in.OccurredAt, s.loc(id))
		if !ok {
			return badRequest("Fecha inválida.")
		}
		now := s.clock.Now()
		if when.Year() < 2000 || when.After(now.AddDate(1, 0, 0)) {
			return badRequest("La fecha está fuera de rango.")
		}
		t.OccurredAt = when
	}
	if in.Kind != nil {
		t.Kind = *in.Kind
	}
	if in.Merchant != nil {
		if t.Merchant, err = cleanText(*in.Merchant, 120, "El comercio"); err != nil {
			return err
		}
	}
	if in.Description != nil {
		if t.Description, err = cleanText(*in.Description, 500, "La descripción"); err != nil {
			return err
		}
	}
	if in.Notes != nil {
		if t.Notes, err = cleanText(*in.Notes, 2000, "Las notas"); err != nil {
			return err
		}
	}
	if in.Status != nil {
		t.Status = *in.Status
	}
	switch {
	case t.AccountID == "":
		return badRequest("Elige una cuenta.")
	case t.CategoryID == "":
		return badRequest("Elige una categoría.")
	case t.AmountCents <= 0:
		return badRequest("El monto debe ser mayor a cero.")
	case t.AmountCents > maxAmount:
		return badRequest("El monto es demasiado alto.")
	case t.OccurredAt.IsZero():
		return badRequest("Indica la fecha.")
	case t.Kind != "purchase" && t.Kind != "refund":
		return badRequest("Tipo inválido (purchase o refund).")
	case t.Status != "confirmed" && t.Status != "pending_review":
		return badRequest("Estado inválido.")
	}
	return nil
}

func (s *Server) createTransaction(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in txInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	t := &store.Transaction{ID: auth.NewID(), Kind: "purchase", Source: "manual", Status: "confirmed", Currency: "USD"}
	if in.ID != nil {
		if !uuidRe.MatchString(*in.ID) {
			return badRequest("Identificador inválido.")
		}
		if prev, err := s.store.Transaction(r.Context(), id.user.ID, id.seal(), *in.ID); err == nil {
			writeJSON(w, 200, prev) // reenvío del mismo gasto: ya estaba guardado
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		t.ID = *in.ID
	}
	if err := s.applyTx(r, id, &in, t, true); err != nil {
		return err
	}
	if err := s.store.CreateTransaction(r.Context(), id.user.ID, id.seal(), t); err != nil {
		if errors.Is(err, store.ErrDuplicate) { // un gasto manual sin hash solo choca por el id (de otro usuario)
			return errf(409, "conflict", "Ese identificador ya está en uso.")
		}
		return err
	}
	s.recheck(r, id, []string{t.CategoryID}, []time.Time{t.OccurredAt})
	writeJSON(w, 201, t)
	return nil
}

func (s *Server) getTransaction(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	t, err := s.store.Transaction(r.Context(), id.user.ID, id.seal(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, t)
	return nil
}

// conditional lee If-Match e Idempotency-Key de una edición o un borrado.
func conditional(r *http.Request) (expect int64, op string, err error) {
	if expect, err = ifMatch(r); err != nil {
		return
	}
	op, err = opID(r)
	return
}

// currentTx devuelve lo que hay en el servidor tras un conflicto de versión (o el error original).
func (s *Server) currentTx(r *http.Request, id *identity, txID string, err error) error {
	var vc *store.VersionConflict
	if !errors.As(err, &vc) {
		return err
	}
	cur, cerr := s.store.Transaction(r.Context(), id.user.ID, id.seal(), txID)
	if cerr != nil {
		return cerr
	}
	return versionConflict(cur)
}

func (s *Server) updateTransaction(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	t, err := s.store.Transaction(r.Context(), id.user.ID, id.seal(), r.PathValue("id"))
	if err != nil {
		return err
	}
	expect, op, err := conditional(r)
	if err != nil {
		return err
	}
	var in txInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if op != "" && t.LastOp == op { // reenvío de una edición que ya se aplicó (se perdió la respuesta)
		writeJSON(w, 200, t)
		return nil
	}
	if expect != 0 && t.Version != expect {
		return versionConflict(t)
	}
	oldCat, oldWhen := t.CategoryID, t.OccurredAt
	before := *t
	if err := s.applyTx(r, id, &in, t, false); err != nil {
		return err
	}
	if err := s.store.UpdateTransactionIf(r.Context(), id.user.ID, id.seal(), t, expect, op); err != nil {
		return s.currentTx(r, id, t.ID, err)
	}
	if before.Status == "pending_review" && t.Status == "confirmed" {
		if err := s.store.SetReview(r.Context(), id.user.ID, t.ID, reviewOf(&before, t, s.loc(id))); err != nil {
			return err
		}
	}
	if in.LearnRule && t.Merchant != "" {
		if _, err := importer.LearnRule(r.Context(), s.store, s.keys, id.user, id.seal(), t.Merchant, t.CategoryID); err != nil {
			return badRequest(err.Error())
		}
	}
	s.recheck(r, id, []string{oldCat, t.CategoryID}, []time.Time{oldWhen, t.OccurredAt})
	writeJSON(w, 200, t)
	return nil
}

func (s *Server) confirmTransaction(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	t, err := s.store.Transaction(r.Context(), id.user.ID, id.seal(), r.PathValue("id"))
	if err != nil {
		return err
	}
	expect, op, err := conditional(r)
	if err != nil {
		return err
	}
	if t.Status == "confirmed" { // confirmar dos veces (o reenviar desde la cola) no cambia nada
		writeJSON(w, 200, t)
		return nil
	}
	if expect != 0 && t.Version != expect {
		return versionConflict(t)
	}
	pending := t.Status == "pending_review"
	t.Status = "confirmed"
	if err := s.store.UpdateTransactionIf(r.Context(), id.user.ID, id.seal(), t, expect, op); err != nil {
		return s.currentTx(r, id, t.ID, err)
	}
	if pending {
		if err := s.store.SetReview(r.Context(), id.user.ID, t.ID, "ok"); err != nil {
			return err
		}
	}
	writeJSON(w, 200, t)
	return nil
}

// deleteTransaction: con If-Match solo borra si nadie lo cambió; 404 si ya no existe (otro dispositivo lo borró),
// que la cola sin conexión trata como "hecho".
func (s *Server) deleteTransaction(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	expect, _, err := conditional(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteTransactionIf(r.Context(), id.user.ID, r.PathValue("id"), expect); err != nil {
		return s.currentTx(r, id, r.PathValue("id"), err)
	}
	w.WriteHeader(204)
	return nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// reviewOf decide si el parser acertó: la categoría y el comercio no cuentan (son clasificación),
// el monto, el tipo, la cuenta y el día sí.
func reviewOf(before, after *store.Transaction, loc *time.Location) string {
	if before.AmountCents != after.AmountCents || before.Kind != after.Kind || before.AccountID != after.AccountID ||
		before.OccurredAt.In(loc).Format("2006-01-02") != after.OccurredAt.In(loc).Format("2006-01-02") {
		return "fixed"
	}
	return "ok"
}

// fold pasa a minúsculas y quita tildes para buscar "cafe" y encontrar "Café".
func fold(s string) string {
	return strings.ToLower(strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
		"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "Ñ", "n").Replace(s))
}

func (s *Server) txFilter(r *http.Request, id *identity) (store.TxFilter, error) {
	q := r.URL.Query()
	loc := s.loc(id)
	var f store.TxFilter
	if m := q.Get("month"); m != "" {
		from, to, err := budget.MonthRange(m, loc)
		if err != nil {
			return f, badRequest("Mes inválido.")
		}
		f.From, f.To = from, to
	}
	if v := q.Get("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return f, badRequest("Fecha 'desde' inválida.")
		}
		f.From = t.UTC()
	}
	if v := q.Get("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return f, badRequest("Fecha 'hasta' inválida.")
		}
		f.To = t.AddDate(0, 0, 1).UTC() // "hasta" incluye ese día completo
	}
	f.AccountID, f.Status, f.Source, f.Kind = q.Get("account_id"), q.Get("status"), q.Get("source"), q.Get("kind")
	if c := q.Get("category_id"); c != "" {
		cats, err := s.store.Categories(r.Context(), id.user.ID)
		if err != nil {
			return f, err
		}
		f.CategoryIDs = []string{c}
		for _, x := range cats {
			if x.ParentID == c {
				f.CategoryIDs = append(f.CategoryIDs, x.ID)
			}
		}
	}
	return f, nil
}

func (s *Server) listTransactions(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	f, err := s.txFilter(r, id)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	all, err := s.store.Transactions(r.Context(), id.user.ID, id.seal(), f)
	if err != nil {
		return err
	}
	// La búsqueda de texto se hace aquí: comercio y notas están cifrados en la BD.
	if term := fold(strings.TrimSpace(q.Get("q"))); term != "" {
		filtered := all[:0]
		for _, t := range all {
			if strings.Contains(fold(t.Merchant+" "+t.Description+" "+t.Notes), term) {
				filtered = append(filtered, t)
			}
		}
		all = filtered
	}
	var sum int64
	for _, t := range all {
		if t.Kind == "refund" {
			sum -= t.AmountCents
		} else {
			sum += t.AmountCents
		}
	}
	page := []store.Transaction{}
	if offset < len(all) {
		page = all[offset:min(offset+limit, len(all))]
	}
	writeJSON(w, 200, map[string]any{"items": page, "total": len(all), "sum_cents": sum, "limit": limit, "offset": offset})
	return nil
}

// ---------------- Presupuestos ----------------

type budgetInput struct {
	CategoryID   *string `json:"category_id"`
	AccountID    *string `json:"account_id"`
	AmountCents  *int64  `json:"amount_cents"`
	Thresholds   *string `json:"thresholds"`
	Active       *bool   `json:"active"`
	Rollover     *bool   `json:"rollover"`
	RolloverFrom *string `json:"rollover_from"` // AAAA-MM; por defecto, el mes actual
}

func (s *Server) applyBudget(in *budgetInput, b *store.Budget, id *identity) error {
	if in.AmountCents != nil {
		b.AmountCents = *in.AmountCents
	}
	if in.Thresholds != nil {
		th, err := budget.ParseThresholds(*in.Thresholds)
		if err != nil {
			return badRequest(err.Error())
		}
		b.Thresholds = budget.FormatThresholds(th)
	}
	if in.Active != nil {
		b.Active = *in.Active
	}
	current := budget.MonthOf(s.clock.Now(), s.loc(id))
	if in.Rollover != nil && *in.Rollover != b.Rollover {
		b.Rollover, b.RolloverFrom = *in.Rollover, ""
		if b.Rollover {
			b.RolloverFrom = current // lo que sobre (o falte) este mes pasa al siguiente
		}
	}
	if in.RolloverFrom != nil {
		if !b.Rollover {
			return badRequest("Activa el arrastre para elegir desde qué mes cuenta.")
		}
		oldest, _ := budget.ShiftMonth(current, -12)
		if _, _, err := budget.MonthRange(*in.RolloverFrom, s.loc(id)); err != nil || *in.RolloverFrom > current || *in.RolloverFrom < oldest {
			return badRequest("El arrastre puede empezar entre hace 12 meses y el mes actual.")
		}
		b.RolloverFrom = *in.RolloverFrom
	}
	if b.AmountCents <= 0 || b.AmountCents > maxAmount {
		return badRequest("El monto del presupuesto debe ser mayor a cero.")
	}
	return nil
}

func (s *Server) listBudgets(w http.ResponseWriter, r *http.Request) error {
	bs, err := s.store.Budgets(r.Context(), ident(r).user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, bs)
	return nil
}

func (s *Server) createBudget(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ctx := r.Context()
	var in budgetInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	b := &store.Budget{ID: auth.NewID(), Thresholds: "50,80,100", Active: true}
	switch {
	case in.CategoryID != nil && in.AccountID != nil:
		return badRequest("Un presupuesto es de una categoría o de una cuenta, no de ambas.")
	case in.CategoryID != nil:
		if _, err := s.store.Category(ctx, id.user.ID, *in.CategoryID); errors.Is(err, store.ErrNotFound) {
			return badRequest("La categoría no existe.")
		} else if err != nil {
			return err
		}
		b.CategoryID = *in.CategoryID
	case in.AccountID != nil:
		if a, err := s.store.Account(ctx, id.user.ID, *in.AccountID); errors.Is(err, store.ErrNotFound) {
			return badRequest("La cuenta no existe.")
		} else if err != nil {
			return err
		} else if a.Archived {
			return badRequest("La cuenta está archivada.")
		}
		b.AccountID = *in.AccountID
	default:
		return badRequest("Elige una categoría o una cuenta.")
	}
	if err := s.applyBudget(&in, b, id); err != nil {
		return err
	}
	if err := s.store.CreateBudget(ctx, id.user.ID, b); err != nil {
		return err
	}
	s.recheck(r, id, nil, []time.Time{s.clock.Now()})
	writeJSON(w, 201, b)
	return nil
}

func (s *Server) updateBudget(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	b, err := s.store.Budget(r.Context(), id.user.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	var in budgetInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.CategoryID != nil && *in.CategoryID != b.CategoryID || in.AccountID != nil && *in.AccountID != b.AccountID {
		return badRequest("No se puede cambiar la categoría o la cuenta de un presupuesto; crea uno nuevo.")
	}
	if err := s.applyBudget(&in, b, id); err != nil {
		return err
	}
	if err := s.store.UpdateBudget(r.Context(), id.user.ID, b); err != nil {
		return err
	}
	s.recheck(r, id, nil, []time.Time{s.clock.Now()})
	writeJSON(w, 200, b)
	return nil
}

func (s *Server) deleteBudget(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteBudget(r.Context(), ident(r).user.ID, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

// putBudgetMonth fija el tope de un mes concreto (por ejemplo, diciembre); null vuelve al tope general.
func (s *Server) putBudgetMonth(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	month := r.PathValue("month")
	if _, _, err := budget.MonthRange(month, s.loc(id)); err != nil {
		return badRequest("Mes inválido.")
	}
	var in struct {
		AmountCents *int64 `json:"amount_cents"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var amount int64
	if in.AmountCents != nil {
		if amount = *in.AmountCents; amount <= 0 || amount > maxAmount {
			return badRequest("El tope del mes debe ser mayor a cero.")
		}
	}
	if err := s.store.SetBudgetMonth(r.Context(), id.user.ID, r.PathValue("id"), month, amount); err != nil {
		return err
	}
	s.recheck(r, id, nil, []time.Time{s.clock.Now()})
	b, err := s.store.Budget(r.Context(), id.user.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, b)
	return nil
}

// copyBudgets copia el plan de un mes a otro: los topes de cada presupuesto activo en el mes origen
// pasan a ser los del mes destino (los que coinciden con el tope general no necesitan tope propio).
func (s *Server) copyBudgets(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	loc := s.loc(id)
	if _, _, err := budget.MonthRange(in.From, loc); err != nil {
		return badRequest("Mes de origen inválido.")
	}
	if _, _, err := budget.MonthRange(in.To, loc); err != nil || in.To == in.From {
		return badRequest("Mes de destino inválido.")
	}
	bs, err := s.store.Budgets(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	copied := 0
	for _, b := range bs {
		if !b.Active {
			continue
		}
		plan := budget.Plan{BaseCents: b.AmountCents, Months: b.Months}
		amount := plan.CapFor(in.From)
		if amount == b.AmountCents {
			amount = 0 // igual al general: se quita el tope propio del destino
		}
		if err := s.store.SetBudgetMonth(r.Context(), id.user.ID, b.ID, in.To, amount); err != nil {
			return err
		}
		copied++
	}
	s.recheck(r, id, nil, []time.Time{s.clock.Now()})
	writeJSON(w, 200, map[string]any{"copied": copied})
	return nil
}

// planCalc calcula topes efectivos (tope del mes + arrastre) y gasto por mes, con caché por mes.
type planCalc struct {
	s     *Server
	ctx   context.Context
	user  string
	loc   *time.Location
	cats  []store.Category
	memo  map[string]monthSpend
	plans map[string]budget.Plan
}

type monthSpend struct{ byCat, byAcc map[string]int64 }

func (s *Server) newPlanCalc(ctx context.Context, u *store.User, cats []store.Category) *planCalc {
	return &planCalc{s: s, ctx: ctx, user: u.ID, loc: s.userLoc(u), cats: cats, memo: map[string]monthSpend{}}
}

func (c *planCalc) month(m string) (monthSpend, error) {
	if v, ok := c.memo[m]; ok {
		return v, nil
	}
	from, to, err := budget.MonthRange(m, c.loc)
	if err != nil {
		return monthSpend{}, err
	}
	cat, err := c.s.store.SpentByCategory(c.ctx, c.user, from, to)
	if err != nil {
		return monthSpend{}, err
	}
	acc, err := c.s.store.SpentByAccount(c.ctx, c.user, from, to)
	if err != nil {
		return monthSpend{}, err
	}
	v := monthSpend{rollup(c.cats, cat), acc}
	c.memo[m] = v
	return v, nil
}

// spent: lo gastado en el mes por el presupuesto (categoría con subcategorías, o cuenta), reversos descontados.
func (c *planCalc) spent(b store.Budget, m string) (int64, error) {
	v, err := c.month(m)
	if b.AccountID != "" {
		return v.byAcc[b.AccountID], err
	}
	return v.byCat[b.CategoryID], err
}

// effective devuelve el tope del mes (general o propio) y el arrastre que llega de meses anteriores.
func (c *planCalc) effective(b store.Budget, m string) (capCents, carry int64, err error) {
	plan := budget.Plan{BaseCents: b.AmountCents, Months: b.Months, Rollover: b.Rollover, RolloverFrom: b.RolloverFrom}
	carry, err = plan.Carry(m, func(x string) (int64, error) { return c.spent(b, x) })
	return plan.CapFor(m), carry, err
}

// rollup suma lo gastado de cada categoría más sus subcategorías.
func rollup(cats []store.Category, spent map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for _, c := range cats {
		out[c.ID] += spent[c.ID]
		if c.ParentID != "" {
			out[c.ParentID] += spent[c.ID]
		}
	}
	return out
}

// recheck registra los umbrales cruzados en los presupuestos afectados por un cambio.
// Un fallo aquí no debe romper el guardado del gasto: se registra y sigue.
func (s *Server) recheck(r *http.Request, id *identity, catIDs []string, whens []time.Time) {
	if err := s.RecheckBudgets(r.Context(), id.user, catIDs, whens); err != nil {
		s.log.Error("recalculando presupuestos", "err", err.Error())
	}
}

// RecheckBudgets registra los umbrales cruzados (las alertas push salen de ahí). Revisa todos los
// presupuestos activos en los meses de whens y en el mes actual: un cambio en un mes pasado mueve el
// arrastre de los siguientes, y los presupuestos por cuenta no dependen de la categoría. Cada umbral
// se registra una sola vez por presupuesto y mes (clave primaria), aunque luego cambie el tope.
// También la usa la sincronización de Gmail, fuera de una petición HTTP; catIDs se conserva por compatibilidad.
func (s *Server) RecheckBudgets(ctx context.Context, u *store.User, catIDs []string, whens []time.Time) error {
	bs, err := s.store.Budgets(ctx, u.ID)
	if err != nil || len(bs) == 0 {
		return err
	}
	cats, err := s.store.Categories(ctx, u.ID)
	if err != nil {
		return err
	}
	calc := s.newPlanCalc(ctx, u, cats)
	months := map[string]bool{budget.MonthOf(s.clock.Now(), calc.loc): true}
	for _, w := range whens {
		months[budget.MonthOf(w, calc.loc)] = true
	}
	for m := range months {
		for _, b := range bs {
			if !b.Active {
				continue
			}
			th, err := budget.ParseThresholds(b.Thresholds)
			if err != nil {
				continue
			}
			capCents, carry, err := calc.effective(b, m)
			if err != nil {
				return err
			}
			spent, err := calc.spent(b, m)
			if err != nil {
				return err
			}
			if _, err := s.store.InsertAlerts(ctx, b.ID, m, budget.Crossed(th, capCents+carry, spent), spent); err != nil {
				return err
			}
		}
	}
	return nil
}

type budgetLine struct {
	BudgetID   string `json:"budget_id"`
	Kind       string `json:"kind"` // category | account
	CategoryID string `json:"category_id"`
	AccountID  string `json:"account_id"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	Icon       string `json:"icon"`
	Thresholds string `json:"thresholds"`
	BaseCents  int64  `json:"base_cents"`  // tope general
	CapCents   int64  `json:"cap_cents"`   // tope de este mes (general o propio)
	MonthCap   bool   `json:"month_cap"`   // este mes tiene tope propio
	CarryCents int64  `json:"carry_cents"` // + sobrante / − exceso que llega del mes anterior
	Rollover   bool   `json:"rollover"`
	RollFrom   string `json:"rollover_from"`
	budget.Progress
}

type summary struct {
	Month           string          `json:"month"`
	Lines           []budgetLine    `json:"lines"`
	Totals          budget.Progress `json:"totals"`
	UnbudgetedCents int64           `json:"unbudgeted_cents"`
	SpentCents      int64           `json:"spent_cents"`
}

func (s *Server) monthParam(r *http.Request, id *identity) (string, error) {
	m := r.URL.Query().Get("month")
	if m == "" {
		return budget.MonthOf(s.clock.Now(), s.loc(id)), nil
	}
	if _, _, err := budget.MonthRange(m, s.loc(id)); err != nil {
		return "", badRequest("Mes inválido.")
	}
	return m, nil
}

func (s *Server) buildSummary(r *http.Request, id *identity, month string) (*summary, []store.Category, map[string]int64, error) {
	ctx := r.Context()
	loc := s.loc(id)
	from, to, _ := budget.MonthRange(month, loc)
	cats, err := s.store.Categories(ctx, id.user.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	spent, err := s.store.SpentByCategory(ctx, id.user.ID, from, to)
	if err != nil {
		return nil, nil, nil, err
	}
	bs, err := s.store.Budgets(ctx, id.user.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	accs, err := s.store.Accounts(ctx, id.user.ID, true)
	if err != nil {
		return nil, nil, nil, err
	}
	byAcc, err := s.store.SpentByAccount(ctx, id.user.ID, from, to)
	if err != nil {
		return nil, nil, nil, err
	}
	calc := s.newPlanCalc(ctx, id.user, cats)
	calc.memo[month] = monthSpend{rollup(cats, spent), byAcc}
	catByID, accByID := map[string]store.Category{}, map[string]store.Account{}
	for _, c := range cats {
		catByID[c.ID] = c
	}
	for _, a := range accs {
		accByID[a.ID] = a
	}
	budgeted := map[string]bool{}
	for _, b := range bs {
		if b.Active && b.CategoryID != "" {
			budgeted[b.CategoryID] = true
		}
	}
	now := s.clock.Now()
	out := &summary{Month: month, Lines: []budgetLine{}}
	var sumBudget, sumSpent int64
	for _, b := range bs {
		if !b.Active {
			continue
		}
		capCents, carry, err := calc.effective(b, month)
		if err != nil {
			return nil, nil, nil, err
		}
		used, _ := calc.spent(b, month)
		_, own := b.Months[month]
		line := budgetLine{BudgetID: b.ID, CategoryID: b.CategoryID, AccountID: b.AccountID, Thresholds: b.Thresholds,
			BaseCents: b.AmountCents, CapCents: capCents, MonthCap: own, CarryCents: carry, Rollover: b.Rollover, RollFrom: b.RolloverFrom,
			Progress: budget.Compute(capCents+carry, used, month, now, loc)}
		if b.AccountID != "" {
			a := accByID[b.AccountID]
			line.Kind, line.Name, line.Color, line.Icon = "account", a.Name, a.Color, a.Icon
		} else {
			c := catByID[b.CategoryID]
			line.Kind, line.Name, line.Color, line.Icon = "category", c.Name, c.Color, c.Icon
			// Si la categoría padre también tiene presupuesto, no se cuenta dos veces. Las cuentas
			// se cruzan con las categorías, así que no entran en el total del plan.
			if c.ParentID == "" || !budgeted[c.ParentID] {
				sumBudget += capCents + carry
				sumSpent += used
			}
		}
		out.Lines = append(out.Lines, line)
	}
	sort.SliceStable(out.Lines, func(i, j int) bool { return out.Lines[i].Percent > out.Lines[j].Percent })
	var all int64
	for _, v := range spent {
		all += v
	}
	out.SpentCents = all
	out.UnbudgetedCents = all - sumSpent
	out.Totals = budget.Compute(sumBudget, sumSpent, month, now, loc)
	return out, cats, spent, nil
}

func (s *Server) budgetSummary(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	m, err := s.monthParam(r, id)
	if err != nil {
		return err
	}
	sum, _, _, err := s.buildSummary(r, id, m)
	if err != nil {
		return err
	}
	writeJSON(w, 200, sum)
	return nil
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	m, err := s.monthParam(r, id)
	if err != nil {
		return err
	}
	as, err := s.store.Alerts(r.Context(), id.user.ID, m)
	if err != nil {
		return err
	}
	writeJSON(w, 200, as)
	return nil
}

// ---------------- Inicio ----------------

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ctx := r.Context()
	m, err := s.monthParam(r, id)
	if err != nil {
		return err
	}
	sum, cats, spent, err := s.buildSummary(r, id, m)
	if err != nil {
		return err
	}
	loc := s.loc(id)
	from, to, _ := budget.MonthRange(m, loc)
	txs, err := s.store.Transactions(ctx, id.user.ID, id.seal(), store.TxFilter{From: from, To: to})
	if err != nil {
		return err
	}
	days := budget.DaysIn(m, loc)
	daily := make([]int64, days)
	for _, t := range txs {
		d := t.OccurredAt.In(loc).Day() - 1
		if d >= 0 && d < days {
			if t.Kind == "refund" {
				daily[d] -= t.AmountCents
			} else {
				daily[d] += t.AmountCents
			}
		}
	}
	type catSpend struct {
		CategoryID string `json:"category_id"`
		Name       string `json:"name"`
		Color      string `json:"color"`
		Icon       string `json:"icon"`
		SpentCents int64  `json:"spent_cents"`
	}
	total := rollup(cats, spent)
	byCat := []catSpend{}
	for _, c := range cats {
		if c.ParentID == "" && total[c.ID] != 0 {
			byCat = append(byCat, catSpend{c.ID, c.Name, c.Color, c.Icon, total[c.ID]})
		}
	}
	sort.Slice(byCat, func(i, j int) bool { return byCat[i].SpentCents > byCat[j].SpentCents })
	recent := txs
	if len(recent) > 6 {
		recent = recent[:6]
	}
	alerts, err := s.store.Alerts(ctx, id.user.ID, m)
	if err != nil {
		return err
	}
	pending, err := s.store.CountPending(ctx, id.user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{
		"month": m, "spent_cents": sum.SpentCents, "count": len(txs), "daily_cents": daily,
		"by_category": byCat, "budget": sum.Totals, "has_budgets": hasCategoryBudget(sum.Lines),
		"recent": recent, "alerts": alerts, "pending_review": pending,
	})
	return nil
}

// hasCategoryBudget: Inicio muestra el total del plan, que solo suma presupuestos por categoría.
func hasCategoryBudget(lines []budgetLine) bool {
	for _, l := range lines {
		if l.Kind == "category" {
			return true
		}
	}
	return false
}
