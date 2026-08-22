package handler

import (
	"fmt"
	"net/http"
)

const AgentVersion = 32

func VersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%d\n", AgentVersion)
	}
}
