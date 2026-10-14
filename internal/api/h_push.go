package api

import (
	"encoding/json"
	"net/http"

	"pulse/internal/auth"
	"pulse/internal/push"
	"pulse/internal/store"
)

func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, map[string]any{"enabled": s.push != nil, "public_key": s.cfg.VAPIDPublicKey})
	return nil
}
func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) error {
	if s.push == nil {
		return errf(503, "push_disabled", "Las notificaciones todavía no están configuradas en el servidor.")
	}
	var in struct {
		Subscription push.Subscription `json:"subscription"`
		DeviceLabel  string            `json:"device_label"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := push.ValidateSubscription(in.Subscription); err != nil {
		return badRequest("Suscripción push inválida.")
	}
	label, err := cleanText(in.DeviceLabel, 60, "El dispositivo")
	if err != nil {
		return err
	}
	if label == "" {
		label = "Navegador"
	}
	id := ident(r)
	raw, err := json.Marshal(in.Subscription)
	if err != nil {
		return err
	}
	sub := store.PushSubscription{ID: auth.NewID(), UserID: id.user.ID, SessionID: id.sess.ID, EndpointHash: s.keys.Lookup("push.endpoint", []byte(in.Subscription.Endpoint)), Enc: id.sealer.SealBytes("push.subscription", raw), DeviceLabel: label}
	if err := s.store.SavePushSubscription(r.Context(), &sub); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "push_subscribed", nil)
	writeJSON(w, 201, map[string]string{"id": sub.ID})
	return nil
}
func (s *Server) pushSubscriptions(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	subs, err := s.store.PushSubscriptions(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	out := []map[string]any{}
	for _, sub := range subs {
		out = append(out, map[string]any{"id": sub.ID, "device_label": sub.DeviceLabel, "created_at": sub.CreatedAt, "current": sub.SessionID == id.sess.ID})
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeletePushSubscription(r.Context(), ident(r).user.ID, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}
func (s *Server) pushUnsubscribeCurrent(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	_, err := s.store.DB.ExecContext(r.Context(), `DELETE FROM push_subscriptions WHERE user_id=? AND session_id=?`, id.user.ID, id.sess.ID)
	if err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}
func (s *Server) pushPreferences(w http.ResponseWriter, r *http.Request) error {
	p, err := s.store.NotificationPrefs(r.Context(), ident(r).user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, p)
	return nil
}
func (s *Server) pushUpdatePreferences(w http.ResponseWriter, r *http.Request) error {
	var p store.NotificationPreferences
	if err := decode(w, r, &p); err != nil {
		return err
	}
	if p.QuietStart < 0 || p.QuietStart > 1439 || p.QuietEnd < 0 || p.QuietEnd > 1439 || p.QuietEnabled && p.QuietStart == p.QuietEnd {
		return badRequest("Horario silencioso inválido.")
	}
	if err := s.store.SetNotificationPrefs(r.Context(), ident(r).user.ID, p); err != nil {
		return err
	}
	writeJSON(w, 200, p)
	return nil
}
func (s *Server) pushTest(w http.ResponseWriter, r *http.Request) error {
	if s.push == nil {
		return errf(503, "push_disabled", "Las notificaciones todavía no están configuradas en el servidor.")
	}
	uid := ident(r).user.ID
	if !s.limPush.Allow(uid) {
		return errRateLimited
	}
	if err := s.push.Test(r.Context(), uid); err != nil {
		return err
	}
	writeJSON(w, 202, map[string]string{"status": "queued"})
	return nil
}
