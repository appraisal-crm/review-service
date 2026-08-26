package handler

import (
	"encoding/json"
	"net/http"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/appraisal-crm/review-service/internal/middleware"
	"github.com/appraisal-crm/review-service/internal/service"
)

type apartmentHandler struct {
	svc service.AppraisalService
}

func newApartmentHandler(svc service.AppraisalService) *apartmentHandler {
	return &apartmentHandler{svc: svc}
}

// GetFormulaConfig godoc
// @Summary     Get apartment appraisal formula configuration
// @Tags        formulas
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} domain.ApartmentFormulaConfig
// @Failure     401 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /settings/apartment-formula [get]
func (h *apartmentHandler) GetFormulaConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.svc.GetApartmentFormulaConfig(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get formula config")
		return
	}
	respondJSON(w, http.StatusOK, cfg)
}

// UpdateFormulaConfig godoc
// @Summary     Update apartment appraisal formula configuration
// @Description Accessible to admin role only.
// @Tags        formulas
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       body body domain.ApartmentFormulaConfig true "Formula configuration"
// @Success     200 {object} domain.ApartmentFormulaConfig
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     403 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /settings/apartment-formula [put]
func (h *apartmentHandler) UpdateFormulaConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var cfg domain.ApartmentFormulaConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updatedBy := "admin"
	if uid, ok := middleware.UserIDFromContext(r.Context()); ok {
		updatedBy = uid.String()
	}

	updated, err := h.svc.UpdateApartmentFormulaConfig(r.Context(), &cfg, updatedBy)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update formula config")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// ResetFormulaConfig godoc
// @Summary     Reset apartment formula configuration to defaults
// @Description Accessible to admin role only.
// @Tags        formulas
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} domain.ApartmentFormulaConfig
// @Failure     401 {object} errorResponse
// @Failure     403 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /settings/apartment-formula/reset [post]
func (h *apartmentHandler) ResetFormulaConfig(w http.ResponseWriter, r *http.Request) {
	updatedBy := "admin"
	if uid, ok := middleware.UserIDFromContext(r.Context()); ok {
		updatedBy = uid.String()
	}

	reset, err := h.svc.ResetApartmentFormulaConfig(r.Context(), updatedBy)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reset formula config")
		return
	}
	respondJSON(w, http.StatusOK, reset)
}

// Calculate godoc
// @Summary     Calculate apartment appraisal value on the fly
// @Description Given subject object and 4 analogs, returns complete step-by-step calculation breakdown.
// @Tags        calculations
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       body body domain.ApartmentCalculationInput true "Subject and 4 analog objects"
// @Success     200 {object} domain.ApartmentCalculationResult
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /calculate/apartment [post]
func (h *apartmentHandler) Calculate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in domain.ApartmentCalculationInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	res, err := h.svc.CalculateApartment(r.Context(), in)
	if err != nil {
		if err == service.ErrInvalidCalculationInput {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to perform calculation")
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// ApplyToAppraisal godoc
// @Summary     Calculate and apply apartment calculation to an appraisal
// @Description Calculates valuation and updates appraisal's market_value, calculation_data, and comparables.
// @Tags        appraisals
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path string true "Appraisal ID"
// @Param       body body domain.ApartmentCalculationInput true "Subject and 4 analog objects"
// @Success     200 {object} domain.Appraisal
// @Failure     400 {object} errorResponse
// @Failure     401 {object} errorResponse
// @Failure     404 {object} errorResponse
// @Failure     422 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /appraisals/{id}/calculate-apartment [post]
func (h *apartmentHandler) ApplyToAppraisal(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in domain.ApartmentCalculationInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.svc.ApplyApartmentCalculation(r.Context(), id, in)
	if err != nil {
		if err == service.ErrInvalidCalculationInput {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeServiceError(w, err, "failed to apply calculation to appraisal")
		return
	}
	respondJSON(w, http.StatusOK, updated)
}
