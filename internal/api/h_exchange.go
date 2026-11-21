package api

import (
	"net/http"
	"strings"
)

func (s *Server) exchangeStatus(w http.ResponseWriter, r *http.Request) error {
	key, err := s.store.ExchangeKey(r.Context(), ident(r).user.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"configured": len(key) > 0})
	return nil
}

func (s *Server) exchangeSet(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		APIKey string `json:"api_key"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	key := strings.TrimSpace(in.APIKey)
	if len(key) < 8 || len(key) > 256 || strings.ContainsAny(key, " \t\n\r") {
		return badRequest("Ingresa una clave de API válida.")
	}
	id := ident(r)
	if err := s.store.SetExchangeKey(r.Context(), id.user.ID, id.seal().Seal("exchange.api_key", key)); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "exchange_key_updated", nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) exchangeDelete(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	if err := s.store.DeleteExchangeKey(r.Context(), id.user.ID); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "exchange_key_deleted", nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
