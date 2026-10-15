package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

func parseTaskQuery(w http.ResponseWriter, r *http.Request) (domain.TaskQuery, bool) {
	q := domain.TaskQuery{Page: 1, PageSize: 20, Sort: "id", Order: "asc"}
	values, err := url.ParseQuery(r.URL.RawQuery)
	invalid := func() (domain.TaskQuery, bool) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "Invalid, unsupported, or repeated task query parameter", nil)
		return domain.TaskQuery{}, false
	}
	if err != nil {
		return invalid()
	}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return invalid()
		}
		value := entries[0]
		switch key {
		case "page", "page_size", "assignee_id", "creator_id":
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number < 1 {
				return invalid()
			}
			switch key {
			case "page":
				if number > 2147483647 {
					return invalid()
				}
				q.Page = int(number)
			case "page_size":
				if number > 100 {
					return invalid()
				}
				q.PageSize = int(number)
			case "assignee_id":
				q.AssigneeID = &number
			case "creator_id":
				q.CreatorID = &number
			}
		case "status":
			q.Status = domain.TaskStatus(value)
		case "priority":
			q.Priority = domain.TaskPriority(value)
		case "search":
			q.Search = value
		case "sort":
			q.Sort = value
		case "order":
			q.Order = value
		case "due_from", "due_to":
			date, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return invalid()
			}
			if key == "due_from" {
				q.DueFrom = &date
			} else {
				q.DueTo = &date
			}
		default:
			return invalid()
		}
	}
	return q, true
}
