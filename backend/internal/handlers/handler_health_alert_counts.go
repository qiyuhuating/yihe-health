package handlers

import (
	"net/http"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/operationsrepo"
	"yihe-care/internal/response"
)

// readSupervisorHealthAlertCounts keeps the live duty lock through the
// aggregate query; handlers commit before sending any count.
func (h *Handler) readSupervisorHealthAlertCounts(w http.ResponseWriter, r *http.Request) (operationsrepo.HealthAlerts, bool) {
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadAlerts)
	if !ok {
		return operationsrepo.HealthAlerts{}, false
	}
	defer tx.Rollback()
	counts, err := operationsrepo.ReadHealthAlerts(r.Context(), tx)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return operationsrepo.HealthAlerts{}, false
	}
	return counts, true
}

func (h *Handler) countVitalAlertDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := optBody(w, r); !ok {
		return
	}
	counts, ok := h.readSupervisorHealthAlertCounts(w, r)
	if !ok {
		return
	}
	response.OK(w, map[string]any{"heartRate": counts.HeartRate, "bloodPressure": counts.BloodPressure})
}

func (h *Handler) countSleepAlertDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := optBody(w, r); !ok {
		return
	}
	counts, ok := h.readSupervisorHealthAlertCounts(w, r)
	if !ok {
		return
	}
	response.OK(w, map[string]any{"sleep": counts.Sleep})
}

func (h *Handler) countHealthAlertDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := optBody(w, r); !ok {
		return
	}
	counts, ok := h.readSupervisorHealthAlertCounts(w, r)
	if !ok {
		return
	}
	response.OK(w, map[string]any{"heartRate": counts.HeartRate, "bloodPressure": counts.BloodPressure, "sleep": counts.Sleep})
}
