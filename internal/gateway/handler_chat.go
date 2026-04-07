package gateway

import (
	"net/http"
	"time"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeNexusError(w, errMethodNotAllowed(), http.StatusMethodNotAllowed)
		return
	}

	// Admission control: limit concurrent requests
	select {
	case s.requestSem <- struct{}{}:
		defer func() { <-s.requestSem }()
	default:
		writeNexusError(w, errServiceOverloaded(), http.StatusServiceUnavailable)
		return
	}

	ctx := &chatContext{start: time.Now(), httpReq: r}

	if err := s.parseRequest(ctx, r); err != nil {
		writeNexusError(w, errInvalidRequest(err.Error()), http.StatusBadRequest)
		return
	}

	s.compressMessages(ctx)

	if s.checkPromptGuard(ctx) {
		writeNexusError(w, errPromptBlocked(), http.StatusBadRequest)
		return
	}

	if s.checkCache(ctx, w) {
		return
	}

	s.routeRequest(ctx)
	s.handleCascade(ctx)

	p := s.selectProvider(ctx, w)
	if p == nil {
		return
	}

	if ctx.req.Stream {
		s.handleStreaming(ctx, w, p)
	} else {
		s.handleNonStreaming(ctx, w, p)
	}
}
