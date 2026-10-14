package api

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"pulse/internal/importer"
	"pulse/internal/mailparse"
	"pulse/internal/store"
)

// POST /api/import/eml — sube un correo (.eml) y lo convierte en un gasto por revisar.
// El correo se procesa en memoria y se descarta: no se guarda en ningún lado.
func (s *Server) importEML(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	if !s.limImport.Allow(id.user.ID) {
		return errRateLimited
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "message/rfc822" && mt != "application/octet-stream" {
		return errf(415, "unsupported_media_type", "Sube el correo como archivo .eml.")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		return errf(413, "too_large", "El correo es demasiado grande (máx. 2 MB).")
	}
	e, err := mailparse.ParseEML(bytes.NewReader(body))
	if err != nil {
		return badRequest("No se pudo leer el archivo como correo (.eml).")
	}
	out, err := s.importer().Import(r.Context(), id.user, e, "email")
	if err != nil {
		return err
	}
	s.audit(r, id.user.ID, "email_imported", map[string]string{"status": out.Status, "parser": out.Parser})
	if out.Status == "created" {
		s.recheck(r, id, []string{out.Transaction.CategoryID}, []time.Time{out.Transaction.OccurredAt})
	}
	writeJSON(w, 200, out)
	return nil
}

type ruleOut struct {
	ID         string    `json:"id"`
	Pattern    string    `json:"pattern"`
	CategoryID string    `json:"category_id"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	rules, err := s.store.Rules(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	out := []ruleOut{}
	for _, x := range rules {
		p, err := id.seal().Open("rule.pattern", x.PatternEnc)
		if err != nil {
			return err
		}
		out = append(out, ruleOut{x.ID, p, x.CategoryID, x.CreatedAt})
	}
	writeJSON(w, 200, out)
	return nil
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Pattern    string `json:"pattern"`
		CategoryID string `json:"category_id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	pattern, err := cleanText(in.Pattern, 120, "El comercio")
	if err != nil {
		return err
	}
	if _, err := s.store.Category(r.Context(), id.user.ID, in.CategoryID); errors.Is(err, store.ErrNotFound) {
		return badRequest("La categoría no existe.")
	} else if err != nil {
		return err
	}
	rid, err := importer.LearnRule(r.Context(), s.store, s.keys, id.user, id.seal(), pattern, in.CategoryID)
	if err != nil {
		return badRequest(err.Error())
	}
	writeJSON(w, 201, map[string]string{"id": rid, "pattern": mailparse.MerchantKey(pattern)})
	return nil
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteRule(r.Context(), ident(r).user.ID, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}
