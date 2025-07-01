package api

import (
	"context"
	"net/http"

	"github.com/alexliesenfeld/health"
)

const HealthAPIPath = "/health"

func HandleHealthCheck() {
	http.Handle(HealthAPIPath, healthCheckHandler())
}

func healthCheckHandler() http.Handler {
	return health.NewHandler(health.NewChecker(
		health.WithCheck(health.Check{
			Name: "signer-health",
			Check: func(context.Context) error {
				return nil
			},
		}),
	))
}
