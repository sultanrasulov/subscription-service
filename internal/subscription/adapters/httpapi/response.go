package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// subscriptionResponse is a subscription as the API returns it. An open-ended
// subscription has no end_date: Swagger 2.0 cannot describe a nullable field,
// but it describes an optional one exactly, and a request omits the field for
// the same meaning.
type subscriptionResponse struct {
	ID          string `json:"id" format:"uuid" example:"01963b4e-7c1a-7d3e-9f2b-1a2b3c4d5e6f"`
	ServiceName string `json:"service_name" example:"Yandex Plus"`
	Price       int64  `json:"price" example:"400"`
	UserID      string `json:"user_id" format:"uuid" example:"60601fee-2bf1-4721-ae6f-7636e79a0cba"`
	StartDate   string `json:"start_date" example:"07-2025"`
	EndDate     string `json:"end_date,omitempty" example:"12-2025"`
}

func toResponse(s domain.Subscription) subscriptionResponse {
	resp := subscriptionResponse{
		ID:          s.ID().UUID().String(),
		ServiceName: s.ServiceName().String(),
		Price:       s.Price().Rubles(),
		UserID:      s.UserID().UUID().String(),
		StartDate:   formatMonth(s.Period().Start()),
	}
	if end, ok := s.Period().End(); ok {
		resp.EndDate = formatMonth(end)
	}
	return resp
}

// listResponse is one page of subscriptions. Items is always an array, empty
// when nothing matches; Limit and Offset are the values applied, defaults
// included; Total counts every match, so a client can tell how many pages
// there are.
type listResponse struct {
	Items  []subscriptionResponse `json:"items"`
	Limit  int                    `json:"limit" example:"20"`
	Offset int                    `json:"offset" example:"0"`
	Total  int                    `json:"total" example:"57"`
}

func toListResponse(items []domain.Subscription, q domain.ListQuery, total int) listResponse {
	// Made, not declared: a nil slice would encode as null.
	resp := listResponse{Items: make([]subscriptionResponse, 0, len(items)), Limit: q.Limit, Offset: q.Offset, Total: total}
	for _, s := range items {
		resp.Items = append(resp.Items, toResponse(s))
	}
	return resp
}

// formatMonth writes a month as MM-YYYY.
func formatMonth(m domain.MonthYear) string {
	return fmt.Sprintf("%02d-%04d", int(m.Month()), m.Year())
}

// writeJSON writes v as a JSON response with the given status. Like every
// problem response, it forbids content sniffing, so a browser never treats the
// body as another type.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	// Once the status line is sent, an encoding or write error cannot reach
	// the client, and the caller has nothing left to do with it.
	_ = json.NewEncoder(w).Encode(v)
}
