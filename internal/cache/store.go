package cache

type Store struct {
	exact   *ExactCache
	l1Enabled bool
}

func NewStore(exact *ExactCache, l1Enabled bool) *Store {
	return &Store{
		exact:     exact,
		l1Enabled: l1Enabled,
	}
}

func (s *Store) Lookup(prompt string, model string) ([]byte, bool, string) {
	if s.l1Enabled && s.exact != nil {
		key := HashKey(prompt, model)
		if data, ok := s.exact.Get(key); ok {
			return data, true, "l1_exact"
		}
	}
	return nil, false, ""
}

func (s *Store) StoreResponse(prompt string, model string, response []byte) {
	if s.l1Enabled && s.exact != nil {
		key := HashKey(prompt, model)
		s.exact.Set(key, response)
	}
}

func (s *Store) Stats() (hits, misses int64, size int) {
	if s.exact != nil {
		return s.exact.Stats()
	}
	return 0, 0, 0
}
