package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

// jsonResponse writes a JSON response with the given status code.
func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, message string, status int) {
	jsonResponse(w, status, map[string]string{"error": message})
}

// jsonErrorWithCode writes a JSON error response with a machine-readable error code.
func jsonErrorWithCode(w http.ResponseWriter, message, code string, status int) {
	jsonResponse(w, status, map[string]string{"error": message, "code": code})
}

// jsonAntdError maps an antd SDK error to the appropriate HTTP status and error code.
// Logs the full error server-side but returns a safe message to the client.
func jsonAntdError(w http.ResponseWriter, context string, err error) {
	slog.Error(context, "error", err)

	var badReq *antd.BadRequestError
	var payment *antd.PaymentError
	var notFound *antd.NotFoundError
	var tooLarge *antd.TooLargeError
	var netErr *antd.NetworkError
	var unavail *antd.ServiceUnavailableError

	switch {
	case errors.As(err, &badReq):
		jsonErrorWithCode(w, context+": invalid request", "antd_bad_request", http.StatusBadRequest)
	case errors.As(err, &payment):
		jsonErrorWithCode(w, context+": payment failed", "antd_payment_error", http.StatusPaymentRequired)
	case errors.As(err, &notFound):
		jsonErrorWithCode(w, context+": not found on network", "antd_not_found", http.StatusNotFound)
	case errors.As(err, &tooLarge):
		jsonErrorWithCode(w, context+": file too large for network", "antd_too_large", http.StatusRequestEntityTooLarge)
	case errors.As(err, &netErr):
		jsonErrorWithCode(w, context+": network unreachable", "antd_network_error", http.StatusBadGateway)
	case errors.As(err, &unavail):
		jsonErrorWithCode(w, context+": storage daemon unavailable", "antd_unavailable", http.StatusServiceUnavailable)
	default:
		jsonErrorWithCode(w, context+": operation failed", "antd_error", http.StatusBadGateway)
	}
}
