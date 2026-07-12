package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/appraisal-crm/review-service/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Access control is role-only: per the BRD an appraiser has access to every
// request, so there is no per-row ownership check — RequireRoles on the router
// is the whole story.
type appraisalHandler struct {
	svc service.AppraisalService
}

func newAppraisalHandler(svc service.AppraisalService) *appraisalHandler {
	return &appraisalHandler{svc: svc}
}

// List godoc
// @Summary     List appraisals
// @Description All appraisals (paginated), or filter by appraiser_id.
// @Tags        appraisals
// @Produce     json
// @Security    BearerAuth
// @Param       appraiser_id query string false "Filter by appraiser ID"
// @Param       request_id query string false "Find the appraisal of a request (0 or 1 items)"
// @Param       page  query int false "Page number (default 1)"
// @Param       limit query int false "Page size (default 20, max 100)"
// @Success     200 {object} listAllResponse
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /appraisals [get]
func (h *appraisalHandler) List(w http.ResponseWriter, r *http.Request) {
	// request_id → the one appraisal of that request, as a 0/1-element list so
	// "not created yet" is an empty result, not a 404.
	if raw := r.URL.Query().Get("request_id"); raw != "" {
		requestID, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid request_id query param")
			return
		}
		a, err := h.svc.GetByRequestID(r.Context(), requestID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				respondJSON(w, http.StatusOK, []*domain.Appraisal{})
				return
			}
			respondError(w, http.StatusInternalServerError, "failed to list appraisals")
			return
		}
		respondJSON(w, http.StatusOK, []*domain.Appraisal{a})
		return
	}

	if raw := r.URL.Query().Get("appraiser_id"); raw != "" {
		appraiserID, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid appraiser_id query param")
			return
		}
		appraisals, err := h.svc.ListByAppraiserID(r.Context(), appraiserID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list appraisals")
			return
		}
		respondJSON(w, http.StatusOK, appraisals)
		return
	}

	page := parseIntParam(r.URL.Query().Get("page"), 1)
	limit := parseIntParam(r.URL.Query().Get("limit"), 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	appraisals, err := h.svc.ListAll(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list appraisals")
		return
	}
	respondJSON(w, http.StatusOK, listAllResponse{Data: appraisals, Page: page, Limit: limit})
}

// GetByID godoc
// @Summary     Get appraisal by ID (with comparables)
// @Tags        appraisals
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "Appraisal ID"
// @Success     200 {object} domain.Appraisal
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     404 {object} errorResponse
// @Router      /appraisals/{id} [get]
func (h *appraisalHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	a, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		writeServiceError(w, err, "failed to get appraisal")
		return
	}
	respondJSON(w, http.StatusOK, a)
}

// Update godoc
// @Summary     Update appraisal fields
// @Description Patches appraiser_id, notes and/or market_value. Rejected once the appraisal is completed.
// @Tags        appraisals
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "Appraisal ID"
// @Param       body body updateAppraisalDTO true "Fields to update"
// @Success     200 {object} domain.Appraisal
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     404 {object} errorResponse
// @Failure     409 {object} errorResponse
// @Failure     422 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /appraisals/{id} [patch]
func (h *appraisalHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var dto updateAppraisalDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validate.Struct(dto); err != nil {
		respondError(w, http.StatusBadRequest, firstValidationError(err))
		return
	}

	updated, err := h.svc.Update(r.Context(), id, service.UpdateInput{
		AppraiserID: dto.AppraiserID,
		Notes:       dto.Notes,
		MarketValue: dto.MarketValue,
	})
	if err != nil {
		writeServiceError(w, err, "failed to update appraisal")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// Complete godoc
// @Summary     Complete an appraisal
// @Description Marks the appraisal completed and emits report.ready. The appraisal is frozen afterwards.
// @Tags        appraisals
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "Appraisal ID"
// @Success     200 {object} domain.Appraisal
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     404 {object} errorResponse
// @Failure     409 {object} errorResponse
// @Failure     422 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /appraisals/{id}/complete [post]
func (h *appraisalHandler) Complete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	completed, err := h.svc.Complete(r.Context(), id)
	if err != nil {
		writeServiceError(w, err, "failed to complete appraisal")
		return
	}
	respondJSON(w, http.StatusOK, completed)
}

// UploadReport godoc
// @Summary     Register the appraisal report file
// @Description Returns the object key and a presigned URL to upload the report bytes to.
// @Tags        appraisals
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "Appraisal ID"
// @Param       body body uploadReportDTO true "Report filename"
// @Success     201 {object} reportResponse
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     404 {object} errorResponse
// @Failure     409 {object} errorResponse
// @Failure     422 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /appraisals/{id}/report [post]
func (h *appraisalHandler) UploadReport(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var dto uploadReportDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validate.Struct(dto); err != nil {
		respondError(w, http.StatusBadRequest, firstValidationError(err))
		return
	}

	result, err := h.svc.UploadReport(r.Context(), id, dto.Filename)
	if err != nil {
		writeServiceError(w, err, "failed to register report")
		return
	}
	respondJSON(w, http.StatusCreated, reportResponse{S3Key: result.S3Key, UploadURL: result.UploadURL})
}

// AddComparable godoc
// @Summary     Add a comparable (analog) to an appraisal
// @Description Free-form JSON data — the comparable field set is not formalized yet.
// @Tags        appraisals
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "Appraisal ID"
// @Param       body body addComparableDTO true "Comparable data"
// @Success     201 {object} domain.Comparable
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     404 {object} errorResponse
// @Failure     422 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /appraisals/{id}/comparables [post]
func (h *appraisalHandler) AddComparable(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var dto addComparableDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validate.Struct(dto); err != nil {
		respondError(w, http.StatusBadRequest, firstValidationError(err))
		return
	}
	if !json.Valid(dto.Data) {
		respondError(w, http.StatusBadRequest, "data must be valid JSON")
		return
	}

	comp, err := h.svc.AddComparable(r.Context(), id, dto.Data)
	if err != nil {
		writeServiceError(w, err, "failed to add comparable")
		return
	}
	respondJSON(w, http.StatusCreated, comp)
}

// DeleteComparable godoc
// @Summary     Delete a comparable from an appraisal
// @Tags        appraisals
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "Appraisal ID"
// @Param       comparableID path string true "Comparable ID"
// @Success     204 "No Content"
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     404 {object} errorResponse
// @Failure     422 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /appraisals/{id}/comparables/{comparableID} [delete]
func (h *appraisalHandler) DeleteComparable(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	comparableID, err := uuid.Parse(chi.URLParam(r, "comparableID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid comparable id")
		return
	}

	if err := h.svc.DeleteComparable(r.Context(), id, comparableID); err != nil {
		writeServiceError(w, err, "failed to delete comparable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

// writeServiceError maps domain errors to HTTP status codes — the one place
// where domain vocabulary becomes HTTP.
func writeServiceError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		respondError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrConflict):
		respondError(w, http.StatusConflict, "appraisal was modified concurrently, please retry")
	case errors.Is(err, domain.ErrInvalidStatus):
		respondError(w, http.StatusUnprocessableEntity, "appraisal is completed and frozen")
	default:
		respondError(w, http.StatusInternalServerError, fallback)
	}
}

func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
