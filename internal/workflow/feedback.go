package workflow

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type FeedbackRequest struct {
	WorkflowID string `json:"workflow_id"`
	Step       int    `json:"step"`
	Outcome    string `json:"outcome"` // success, failure, or 0.0-1.0
	Details    string `json:"details,omitempty"`
}

type FeedbackResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type FeedbackHandler struct {
	tracker *Tracker
	logger  *slog.Logger
}

func NewFeedbackHandler(tracker *Tracker, logger *slog.Logger) *FeedbackHandler {
	return &FeedbackHandler{tracker: tracker, logger: logger}
}

func (h *FeedbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, FeedbackResponse{
			Status:  "error",
			Message: "invalid request body",
		})
		return
	}

	if req.WorkflowID == "" || req.Step == 0 || req.Outcome == "" {
		writeJSON(w, http.StatusBadRequest, FeedbackResponse{
			Status:  "error",
			Message: "workflow_id, step, and outcome are required",
		})
		return
	}

	ok := h.tracker.RecordFeedback(req.WorkflowID, req.Step, req.Outcome)
	if !ok {
		writeJSON(w, http.StatusNotFound, FeedbackResponse{
			Status:  "error",
			Message: "workflow or step not found",
		})
		return
	}

	h.logger.Info("feedback recorded",
		"workflow_id", req.WorkflowID,
		"step", req.Step,
		"outcome", req.Outcome,
	)

	writeJSON(w, http.StatusOK, FeedbackResponse{Status: "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
