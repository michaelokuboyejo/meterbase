package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/alerts"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/customers"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/dashauth"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/ingest"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/meters"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/plans"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/usage"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/webhooks"
)

func NewRouter(
	pool *pgxpool.Pool,
	corsOrigin string,
	resolver auth.KeyResolver,
	dashStore *dashauth.Store,
	customerRepo customers.Repository,
	meterRepo meters.Repository,
	eventStore ingest.EventStore,
	eventListStore ingest.EventListStore,
	usageRepo usage.UsageQuerier,
	planRepo plans.Repository,
	alertRepo alerts.AlertRepository,
	alertMeterRepo alerts.MeterResolver,
	webhookRepo webhooks.Repository,
) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(corsMiddleware(corsOrigin))

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(pool))

	dashHandler := dashauth.NewHandler(dashStore)
	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", dashHandler.Login)
		r.Group(func(r chi.Router) {
			r.Use(dashauth.DashMiddleware(dashStore))
			r.Post("/logout", dashHandler.Logout)
			r.Get("/me", dashHandler.Me)
		})
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.Middleware(resolver))
		r.Route("/customers", customers.NewHandler(customerRepo).Routes)
		r.Route("/meters", meters.NewHandler(meterRepo, usageRepo).Routes)
		r.Route("/events", ingest.NewHandler(eventStore, eventListStore).Routes)
		r.Route("/plans", plans.NewHandler(planRepo, customerRepo, meterRepo, usageRepo).Routes)
		r.Route("/alert-rules", alerts.NewHandler(alertRepo, alertMeterRepo).Routes)
		r.Route("/webhooks", webhooks.NewHandler(webhookRepo).Routes)
	})

	return r
}
