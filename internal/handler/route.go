package handler

import (
	"github.com/MicahParks/keyfunc/v3"
	"github.com/appraisal-crm/review-service/internal/middleware"
	"github.com/appraisal-crm/review-service/internal/service"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	httpSwagger "github.com/swaggo/http-swagger"
)

func NewRouter(svc service.AppraisalService, jwks keyfunc.Keyfunc, allowedOrigins []string) *chi.Mux {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
	}))
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// Public.
	r.Get("/health", Health)
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	// Authenticated. Appraisal work is internal: appraiser and admin only —
	// clients never talk to review-service directly.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(jwks))
		r.Use(middleware.RequireRoles("appraiser", "admin"))

		ah := newAppraisalHandler(svc)
		apth := newApartmentHandler(svc)

		r.Get("/appraisals", ah.List)
		r.Get("/appraisals/{id}", ah.GetByID)
		r.Patch("/appraisals/{id}", ah.Update)
		r.Post("/appraisals/{id}/comparables", ah.AddComparable)
		r.Delete("/appraisals/{id}/comparables/{comparableID}", ah.DeleteComparable)
		r.Post("/appraisals/{id}/report", ah.UploadReport)
		r.Post("/appraisals/{id}/complete", ah.Complete)

		// Calculation endpoints
		r.Post("/calculate/apartment", apth.Calculate)
		r.Post("/appraisals/{id}/calculate-apartment", apth.ApplyToAppraisal)
		r.Get("/settings/apartment-formula", apth.GetFormulaConfig)

		// Admin-only formula configuration endpoints
		r.Group(func(adminRouter chi.Router) {
			adminRouter.Use(middleware.RequireRoles("admin"))
			adminRouter.Put("/settings/apartment-formula", apth.UpdateFormulaConfig)
			adminRouter.Post("/settings/apartment-formula/reset", apth.ResetFormulaConfig)
		})
	})

	return r
}
