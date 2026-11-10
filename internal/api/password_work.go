package api

// Limita globalmente el trabajo PBKDF2 aunque el atacante cambie de dirección IP.
// No se acumulan goroutines esperando una plaza.
func (s *Server) acquirePasswordWork() bool {
	select {
	case s.passwordWork <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releasePasswordWork() { <-s.passwordWork }
