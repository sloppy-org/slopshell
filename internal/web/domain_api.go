package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func parseURLInt64Param(r *http.Request, key string) (int64, error) {
	value := strings.TrimSpace(chi.URLParam(r, key))
	if value == "" {
		return 0, errors.New("missing " + key)
	}
	return strconv.ParseInt(value, 10, 64)
}

func isForeignKeyConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "foreign key")
}

func domainResponseErrorStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound
	case isUniqueConstraint(err), isForeignKeyConstraint(err):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func writeDomainStoreError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	http.Error(w, err.Error(), domainResponseErrorStatus(err))
}
