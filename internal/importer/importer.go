// Package importer convierte un correo bancario en un gasto pendiente de revisión:
//
//	correo ─► mailparse.Analyze ─► cuenta (banco + últimos dígitos; se crea si no existe)
//	       ─► categoría (reglas del usuario ▸ diccionario de comercios conocidos ▸ "Otros")
//	       ─► transacción "pending_review" (modo sombra) con hash del mensaje para no duplicar
//
// La usarán tanto la importación manual de .eml como la sincronización con Gmail (fase 7).
package importer

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"time"

	"pulse/internal/auth"
	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/mailparse"
	"pulse/internal/store"
)

type Importer struct {
	Store     *store.Store
	Keys      *crypto.Keyring
	Clock     clock.Clock
	DefaultTZ string
}

type Outcome struct {
	Status      string             `json:"status"` // created | duplicate | ignored | unrecognized | unknown
	Reason      string             `json:"reason,omitempty"`
	Parser      string             `json:"parser,omitempty"`
	Transaction *store.Transaction `json:"transaction,omitempty"`
	Account     string             `json:"account,omitempty"`
	Category    string             `json:"category,omitempty"`
	CategoryBy  string             `json:"category_by,omitempty"` // rule | builtin | default
	NewAccount  bool               `json:"new_account,omitempty"`
}

// Import procesa un correo para el usuario. source: "email" (subido a mano) o "gmail".
func (im *Importer) Import(ctx context.Context, u *store.User, e *mailparse.Email, source string) (*Outcome, error) {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		loc, _ = time.LoadLocation(im.DefaultTZ)
	}
	r := mailparse.Analyze(e, loc)
	if r.Status != mailparse.StatusExpense {
		return &Outcome{Status: r.Status, Reason: r.Reason, Parser: r.Parser}, nil
	}
	return im.ImportExpense(ctx, u, e, r.Expense, source)
}

// ImportExpense registra un gasto ya extraído (por un parser o por Gemini) como pendiente de revisión.
func (im *Importer) ImportExpense(ctx context.Context, u *store.User, e *mailparse.Email, x *mailparse.Expense, source string) (*Outcome, error) {
	out := &Outcome{Status: mailparse.StatusExpense, Parser: x.Parser}
	sl, err := im.Keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		return nil, err
	}
	acc, created, err := im.account(ctx, u.ID, x)
	if err != nil {
		return nil, err
	}
	cat, by, err := im.category(ctx, u.ID, sl, x.Merchant)
	if err != nil {
		return nil, err
	}
	conf := x.Confidence
	switch by {
	case "builtin":
		conf -= 0.05
	case "default":
		conf -= 0.15
	}
	t := &store.Transaction{
		ID: auth.NewID(), AccountID: acc.ID, CategoryID: cat.ID, AmountCents: x.AmountCents, Currency: x.Currency,
		OccurredAt: x.OccurredAt, Kind: x.Kind, Merchant: x.Merchant, Source: source,
		Status:      "pending_review", // modo sombra: todo lo importado se revisa al principio
		MessageHash: im.messageHash(u.ID, e), Parser: x.Parser, Confidence: &conf,
	}
	if err := im.Store.CreateTransaction(ctx, u.ID, sl, t); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return &Outcome{Status: "duplicate", Reason: "ese correo ya fue importado", Parser: x.Parser}, nil
		}
		return nil, err
	}
	out.Status, out.Transaction, out.Account, out.Category, out.CategoryBy, out.NewAccount = "created", t, acc.Name, cat.Name, by, created
	return out, nil
}

// messageHash identifica el correo sin guardar su Message-ID: HMAC por usuario.
func (im *Importer) messageHash(userID string, e *mailparse.Email) []byte {
	id := e.MessageID
	if id == "" { // sin Message-ID: remitente + fecha + contenido
		h := sha256.Sum256([]byte(e.From + "|" + e.Date.UTC().String() + "|" + e.Text))
		id = string(h[:])
	}
	return im.Keys.Lookup("message", []byte(userID+"|"+id))
}

func (im *Importer) account(ctx context.Context, userID string, x *mailparse.Expense) (*store.Account, bool, error) {
	accs, err := im.Store.Accounts(ctx, userID, true)
	if err != nil {
		return nil, false, err
	}
	if a := MatchAccount(accs, x.Institution, x.CardSuffix); a != nil {
		return a, false, nil
	}
	name := x.Institution
	if x.CardSuffix != "" {
		name += " …" + x.CardSuffix
	}
	typ := "credit_card"
	if x.CardType == "debit" {
		typ = "debit_card"
	}
	a := &store.Account{ID: auth.NewID(), Name: name, Type: typ, Institution: x.Institution, CardSuffix: x.CardSuffix, Currency: "USD", Color: "#64748b", Icon: "💳"}
	return a, true, im.Store.CreateAccount(ctx, userID, a)
}

// MatchAccount busca la cuenta por últimos dígitos; si hay varias con los mismos dígitos,
// desempata por institución. Las cuentas archivadas solo se usan si no hay otra.
func MatchAccount(accs []store.Account, institution, suffix string) *store.Account {
	if suffix == "" {
		return nil
	}
	var cands []store.Account
	for _, a := range accs {
		if a.CardSuffix == suffix {
			cands = append(cands, a)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return !cands[i].Archived && cands[j].Archived })
	inst := mailparse.Fold(institution)
	for i := range cands {
		ai := mailparse.Fold(cands[i].Institution)
		if ai != "" && (ai == inst || sharesWord(ai, inst)) {
			return &cands[i]
		}
	}
	for i := range cands {
		if cands[i].Institution == "" {
			return &cands[i]
		}
	}
	if len(cands) == 1 {
		return &cands[0]
	}
	return nil
}

// Palabras que no identifican a un banco ("Banco Pichincha" y "Banco Guayaquil" no son el mismo).
var genericWords = map[string]bool{"banco": true, "bank": true, "club": true, "tarjeta": true, "tarjetas": true, "credito": true, "del": true, "de": true, "la": true}

func sharesWord(a, b string) bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(b, func(r rune) bool { return r == ' ' || r == '(' || r == ')' }) {
		words[w] = true
	}
	for _, w := range strings.FieldsFunc(a, func(r rune) bool { return r == ' ' || r == '(' || r == ')' }) {
		if len(w) >= 4 && !genericWords[w] && words[w] {
			return true
		}
	}
	return false
}

// Diccionario inicial de comercios frecuentes en Ecuador → nombre de categoría por defecto.
var builtin = []struct {
	keys     []string
	category string
}{
	{[]string{"uber eats", "pedidosya", "rappi", "supermaxi", "megamaxi", "mi comisariato", "tia", "aki", "gran aki", "coral", "sweet coffee", "kfc", "mcdonald", "burger king", "carl s", "pizza hut", "dominos", "juan valdez"}, "Comida"},
	{[]string{"uber", "cabify", "indriver", "didi", "primax", "petroecuador", "mobil", "terpel", "puma", "metrovia", "aerolinea", "latam", "avianca"}, "Transporte"},
	{[]string{"netflix", "spotify", "disney", "hbo", "max com", "prime video", "youtube", "apple com", "icloud", "google one", "chatgpt", "openai"}, "Suscripciones"},
	{[]string{"fybeca", "sana sana", "pharmacys", "cruz azul", "medicity", "veris", "hospital", "clinica"}, "Salud"},
	{[]string{"cnel", "interagua", "claro", "movistar", "cnt", "netlife", "puntonet", "tvcable", "directv"}, "Servicios"},
	{[]string{"supercines", "cinemark", "steam", "playstation", "xbox", "nintendo"}, "Ocio"},
	{[]string{"de prati", "etafashion", "rm", "amazon", "aliexpress", "shein", "temu", "mercadolibre", "kywi", "sukasa", "pycca"}, "Compras"},
}

func containsKey(merchantKey, key string) bool {
	return merchantKey == key || strings.HasPrefix(merchantKey, key+" ") || strings.HasSuffix(merchantKey, " "+key) || strings.Contains(merchantKey, " "+key+" ")
}

func (im *Importer) category(ctx context.Context, userID string, sl *crypto.Sealer, merchant string) (*store.Category, string, error) {
	cats, err := im.Store.Categories(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	byID := map[string]*store.Category{}
	for i := range cats {
		byID[cats[i].ID] = &cats[i]
	}
	key := mailparse.MerchantKey(merchant)
	// 1) reglas del usuario: gana la más específica (patrón más largo)
	rules, err := im.Store.Rules(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	best, bestLen := "", 0
	for _, r := range rules {
		p, err := sl.Open("rule.pattern", r.PatternEnc)
		if err != nil || p == "" {
			continue
		}
		if containsKey(key, p) && len(p) > bestLen && byID[r.CategoryID] != nil {
			best, bestLen = r.CategoryID, len(p)
		}
	}
	if best != "" {
		return byID[best], "rule", nil
	}
	// 2) diccionario incorporado, si el usuario conserva esa categoría
	for _, b := range builtin {
		for _, k := range b.keys {
			if containsKey(key, k) {
				for i := range cats {
					if cats[i].ParentID == "" && mailparse.Fold(cats[i].Name) == mailparse.Fold(b.category) {
						return &cats[i], "builtin", nil
					}
				}
			}
		}
	}
	// 3) "Otros"
	sys, err := im.Store.SystemCategory(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	return sys, "default", nil
}

// LearnRule guarda "este comercio siempre va a esta categoría".
func LearnRule(ctx context.Context, st *store.Store, keys *crypto.Keyring, u *store.User, sl *crypto.Sealer, merchant, categoryID string) (string, error) {
	key := mailparse.MerchantKey(merchant)
	if len(key) < 3 {
		return "", errors.New("el comercio es demasiado corto para crear una regla")
	}
	return st.UpsertRule(ctx, u.ID, auth.NewID(), sl.Seal("rule.pattern", key), keys.Lookup("rule", []byte(u.ID+"|"+key)), categoryID)
}
