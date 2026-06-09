package middleware

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
)

func SentryMiddleware(dsn string) func(http.Handler) http.Handler {
	if dsn == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	_ = sentry.Init(sentry.ClientOptions{
		Dsn: dsn,
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			hub := sentry.GetHubFromContext(ctx)
			if hub == nil {
				hub = sentry.CurrentHub().Clone()
			}
			hub.ConfigureScope(func(scope *sentry.Scope) {
				scope.SetTag("transaction", r.URL.Path)
			})

			transaction := sentry.StartTransaction(ctx, r.URL.Path)
			defer transaction.Finish()

			sw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			func() {
				defer func() {
					if rec := recover(); rec != nil {
						transaction.Status = sentry.SpanStatusInternalError
						hub.CaptureException(fmt.Errorf("panic: %v", rec))
						sw.WriteHeader(http.StatusInternalServerError)
						sw.Write([]byte("Internal Server Error"))
					}
				}()
				next.ServeHTTP(sw, r)
			}()

			transaction.SetTag("http.status_code", fmt.Sprintf("%d", sw.statusCode))
			if sw.statusCode >= 500 {
				transaction.Status = sentry.SpanStatusInternalError
			} else if sw.statusCode >= 400 {
				transaction.Status = sentry.SpanStatusInvalidArgument
			} else {
				transaction.Status = sentry.SpanStatusOK
			}
		})
	}
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}
