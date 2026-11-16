package httpapi

import (
	"net/http"

	"github.com/sultanrasulov/subscription-service/internal/platform/jsonbody"
	"github.com/sultanrasulov/subscription-service/internal/platform/problem"
	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// create stores a new subscription.
//
//	@Summary		Create a subscription
//	@Description	Every field error is reported in one response, in the schema's field order, then unknown and duplicated fields.
//	@Tags			subscriptions
//	@Accept			json
//	@Produce		json
//	@Param			subscription	body		createRequest			true	"The subscription; omit end_date for an open-ended one"
//	@Success		201				{object}	subscriptionResponse	"Created; Location holds its URL"
//	@Header			201				{string}	Location				"/api/v1/subscriptions/{id}"
//	@Failure		400				{object}	problem.Details			"Invalid fields or a malformed body"
//	@Failure		413				{object}	problem.Details			"Body over 1 MiB"
//	@Failure		415				{object}	problem.Details			"Content-Type is not application/json"
//	@Failure		500				{object}	problem.Details
//	@Failure		503				{object}	problem.Details	"The request timed out"
//	@Router			/subscriptions [post]
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	obj, err := jsonbody.Read(w, r, maxBodyBytes)
	if err != nil {
		a.unreadable(w, r, err)
		return
	}
	in, fieldErrors := readCreate(obj)
	if len(fieldErrors) > 0 {
		invalid(w, fieldErrors)
		return
	}

	s, err := a.svc.Create(r.Context(), in.userID, in.name, in.price, in.period)
	if err != nil {
		a.fail(w, r, "create subscription", err)
		return
	}
	w.Header().Set("Location", basePath+"/subscriptions/"+s.ID().UUID().String())
	writeJSON(w, http.StatusCreated, toResponse(s))
}

// get returns one subscription.
//
//	@Summary	Get a subscription
//	@Tags		subscriptions
//	@Produce	json
//	@Param		id	path		string	true	"Subscription ID, a canonical UUID"	format(uuid)
//	@Success	200	{object}	subscriptionResponse
//	@Failure	400	{object}	problem.Details	"Malformed ID"
//	@Failure	404	{object}	problem.Details	"No such subscription"
//	@Failure	500	{object}	problem.Details
//	@Failure	503	{object}	problem.Details	"The request timed out"
//	@Router		/subscriptions/{id} [get]
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, fieldErr, ok := pathID(r)
	if !ok {
		invalid(w, []problem.FieldError{fieldErr})
		return
	}
	s, err := a.svc.Get(r.Context(), id)
	if err != nil {
		a.fail(w, r, "get subscription", err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(s))
}

// update replaces a subscription's service name, price and period. Its ID and
// user never change.
//
//	@Summary		Replace a subscription
//	@Description	A full replacement: omit end_date to make the subscription open-ended. user_id cannot change, so it is an unknown field here. A malformed ID is reported alone, before the body is read.
//	@Tags			subscriptions
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string					true	"Subscription ID, a canonical UUID"	format(uuid)
//	@Param			subscription	body		updateRequest			true	"The new service name, price and period"
//	@Success		200				{object}	subscriptionResponse
//	@Failure		400				{object}	problem.Details	"Malformed ID, invalid fields or a malformed body"
//	@Failure		404				{object}	problem.Details	"No such subscription"
//	@Failure		413				{object}	problem.Details	"Body over 1 MiB"
//	@Failure		415				{object}	problem.Details	"Content-Type is not application/json"
//	@Failure		500				{object}	problem.Details
//	@Failure		503				{object}	problem.Details	"The request timed out"
//	@Router			/subscriptions/{id} [put]
func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, fieldErr, ok := pathID(r)
	if !ok {
		invalid(w, []problem.FieldError{fieldErr})
		return
	}
	obj, err := jsonbody.Read(w, r, maxBodyBytes)
	if err != nil {
		a.unreadable(w, r, err)
		return
	}
	in, fieldErrors := readUpdate(obj)
	if len(fieldErrors) > 0 {
		invalid(w, fieldErrors)
		return
	}

	s, err := a.svc.Update(r.Context(), id, in.name, in.price, in.period)
	if err != nil {
		a.fail(w, r, "update subscription", err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(s))
}

// remove deletes a subscription.
//
//	@Summary	Delete a subscription
//	@Tags		subscriptions
//	@Param		id	path	string	true	"Subscription ID, a canonical UUID"	format(uuid)
//	@Success	204	"Deleted"
//	@Failure	400	{object}	problem.Details	"Malformed ID"
//	@Failure	404	{object}	problem.Details	"No such subscription"
//	@Failure	500	{object}	problem.Details
//	@Failure	503	{object}	problem.Details	"The request timed out"
//	@Router		/subscriptions/{id} [delete]
func (a *API) remove(w http.ResponseWriter, r *http.Request) {
	id, fieldErr, ok := pathID(r)
	if !ok {
		invalid(w, []problem.FieldError{fieldErr})
		return
	}
	if err := a.svc.Delete(r.Context(), id); err != nil {
		a.fail(w, r, "delete subscription", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// list returns one page of subscriptions.
//
//	@Summary		List subscriptions
//	@Description	Filters combine with AND; the service name matches case-insensitively. Results are ordered by ID. Unknown parameters are ignored; a malformed query string, an empty or a repeated parameter is a 400.
//	@Tags			subscriptions
//	@Produce		json
//	@Param			user_id			query		string			false	"Only this user's subscriptions; a canonical UUID"	format(uuid)
//	@Param			service_name	query		string			false	"Only this service, in any case"
//	@Param			limit			query		int				false	"Page size"						minimum(1)	maximum(100)	default(20)
//	@Param			offset			query		int				false	"Subscriptions to skip"			minimum(0)	default(0)
//	@Success		200				{object}	listResponse
//	@Failure		400				{object}	problem.Details	"Malformed query or invalid parameters"
//	@Failure		500				{object}	problem.Details
//	@Failure		503				{object}	problem.Details	"The request timed out"
//	@Router			/subscriptions [get]
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	values, ok := parseQuery(r.URL.RawQuery)
	if !ok {
		problem.Write(w, http.StatusBadRequest, "query string is malformed")
		return
	}
	q, fieldErrors := readList(values)
	if len(fieldErrors) > 0 {
		invalid(w, fieldErrors)
		return
	}

	items, total, err := a.svc.List(r.Context(), q)
	if err != nil {
		a.fail(w, r, "list subscriptions", err)
		return
	}
	writeJSON(w, http.StatusOK, toListResponse(items, q, total))
}

// totalCost reports the total cost of subscriptions over a calculation period.
//
//	@Summary		Calculate the total cost
//	@Description	The sum, over the subscriptions that match the filters, of each one's monthly price times the months its period shares with the calculation period. Both ends of both periods are inclusive; an open-ended subscription counts until to; no match gives 0. Filters combine with AND; the service name matches case-insensitively. Unknown parameters are ignored; a malformed query string, an empty or a repeated parameter is a 400.
//	@Tags			subscriptions
//	@Produce		json
//	@Param			from			query		string				true	"First month of the calculation period, MM-YYYY, inclusive"	extensions(x-example=01-2025)
//	@Param			to				query		string				true	"Last month of the calculation period, MM-YYYY, inclusive; not before from"	extensions(x-example=12-2025)
//	@Param			user_id			query		string				false	"Only this user's subscriptions; a canonical UUID"	format(uuid)
//	@Param			service_name	query		string				false	"Only this service, in any case"
//	@Success		200				{object}	totalCostResponse
//	@Failure		400				{object}	problem.Details	"Malformed query or invalid parameters"
//	@Failure		500				{object}	problem.Details
//	@Failure		503				{object}	problem.Details	"The request timed out"
//	@Router			/subscriptions/total-cost [get]
func (a *API) totalCost(w http.ResponseWriter, r *http.Request) {
	values, ok := parseQuery(r.URL.RawQuery)
	if !ok {
		problem.Write(w, http.StatusBadRequest, "query string is malformed")
		return
	}
	q, fieldErrors := readTotalCost(values)
	if len(fieldErrors) > 0 {
		invalid(w, fieldErrors)
		return
	}

	total, err := a.svc.TotalCost(r.Context(), q)
	if err != nil {
		a.fail(w, r, "calculate total cost", err)
		return
	}
	writeJSON(w, http.StatusOK, totalCostResponse{TotalCost: total})
}

// pathID reads the {id} path value as a subscription ID.
func pathID(r *http.Request) (domain.SubscriptionID, problem.FieldError, bool) {
	raw, ok := parseUUID(r.PathValue("id"))
	if !ok {
		return domain.SubscriptionID{}, problem.FieldError{Field: "id", Message: "must be a valid UUID"}, false
	}
	id, err := domain.NewSubscriptionID(raw)
	if err != nil {
		return domain.SubscriptionID{}, problem.FieldError{Field: "id", Message: "must not be the nil UUID"}, false
	}
	return id, problem.FieldError{}, true
}
