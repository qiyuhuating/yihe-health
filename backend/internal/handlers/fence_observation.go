package handlers

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/devicerepo"
	"yihe-care/internal/fence"
	"yihe-care/internal/fencerepo"
	"yihe-care/internal/notify"
)

type fencePointRequest struct {
	capabilityCredentials
	Point            *fence.Point `json:"point"`
	CoordinateSystem string       `json:"coordinateSystem"`
	Accuracy         float64      `json:"accuracy"`
	OccurredAt       time.Time    `json:"occurredAt"`
}

func advanceFenceObservation(f *fencerepo.Config, state *fence.State, req *fencePointRequest, at time.Time) (string, bool, error) {
	if !at.After(state.Last) {
		return "", false, nil
	}
	loc, e := time.LoadLocation(f.Timezone)
	if e != nil {
		return "", false, e
	}
	local := at.In(loc)
	second := local.Hour()*3600 + local.Minute()*60 + local.Second()
	direction := ""
	if !fence.Active(second, f.TimeBegin, f.TimeEnd) {
		*state = fence.State{Last: at}
	} else if req.Accuracy > f.MaxAccuracy || fence.BoundaryDistance(f.Coordinates, *req.Point) <= req.Accuracy {
		state.Count = 0
		state.Since = time.Time{}
		state.Last = at
	} else {
		direction = state.Observe(fence.Contains(f.Coordinates, *req.Point), at, f.MinPoints, time.Duration(f.DwellSeconds)*time.Second)
	}
	return direction, true, nil
}

func (h *Handler) persistFenceEvent(ctx context.Context, tx *sql.Tx, d *devicerepo.DeviceRecord, f *fencerepo.Config, req *fencePointRequest, direction string, at time.Time) (map[string]any, error) {
	eventID, err := fencerepo.InsertEvent(ctx, tx, fencerepo.NewEvent{
		FenceID: f.ID, FenceRevision: f.Revision,
		ResidentID: *d.OldID, DeviceID: d.ID, Epoch: d.HealthEpoch,
		Direction: direction, Point: *req.Point, Accuracy: req.Accuracy, OccurredAt: at,
	})
	if err != nil {
		return nil, err
	}
	var incidentID *int64
	if direction == "exit" {
		id, err := fencerepo.InsertExitIncident(ctx, tx, *d.OldID, d.ID)
		if err != nil {
			return nil, err
		}
		if err := fencerepo.InsertExitReceipt(ctx, tx, id); err != nil {
			return nil, err
		}
		if _, err := notify.EnqueueIncident(ctx, tx, h.Box, id, *d.OldID); err != nil {
			return nil, err
		}
		if err := fencerepo.LinkIncident(ctx, tx, eventID, id); err != nil {
			return nil, err
		}
		incidentID = &id
	}
	return map[string]any{"id": eventID, "fenceId": f.ID, "direction": direction, "incidentId": incidentID}, nil
}
