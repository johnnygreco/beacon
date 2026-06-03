package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSessionsAPILimit      = 50
	maxSessionsAPILimit          = 200
	defaultSessionEventsAPILimit = 200
	maxSessionEventsAPILimit     = 500
	defaultSearchEventsAPILimit  = 20
	maxSearchEventsAPILimit      = 240
	maxDashboardSessionsAPILimit = 200
	maxDashboardSearchAPILimit   = 240
)

type apiIntParam struct {
	Name    string
	Default int
	Min     int
	Max     int
}

func parseAPIIntParam(values url.Values, spec apiIntParam) (int, error) {
	raw := strings.TrimSpace(values.Get(spec.Name))
	if raw == "" {
		return spec.Default, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", spec.Name)
	}
	if value < spec.Min {
		return spec.Default, nil
	}
	if spec.Max > 0 && value > spec.Max {
		return spec.Max, nil
	}
	return value, nil
}

func parseAPIRangeParam(values url.Values, defaultRange string, names ...string) string {
	if len(names) == 0 {
		names = []string{"range"}
	}
	for _, name := range names {
		rangeValues, ok := values[name]
		if !ok {
			continue
		}
		if len(rangeValues) == 0 {
			return ""
		}
		value := strings.TrimSpace(rangeValues[0])
		if value == "" || strings.EqualFold(value, "all") {
			return ""
		}
		switch value {
		case "1h", "24h", "7d", "30d":
			return value
		default:
			return defaultRange
		}
	}
	return defaultRange
}

func parseAPICSVParam(values url.Values, name string) []string {
	var parsed []string
	for _, raw := range values[name] {
		for _, part := range strings.Split(raw, ",") {
			if value := strings.TrimSpace(part); value != "" {
				parsed = append(parsed, value)
			}
		}
	}
	return parsed
}

type sessionsAPIRequest struct {
	Limit int
}

func parseSessionsAPIRequest(values url.Values) (sessionsAPIRequest, error) {
	limit, err := parseAPIIntParam(values, apiIntParam{
		Name:    "limit",
		Default: defaultSessionsAPILimit,
		Min:     1,
		Max:     maxSessionsAPILimit,
	})
	return sessionsAPIRequest{Limit: limit}, err
}

type dashboardSessionsAPIRequest struct {
	State     string
	Range     string
	Query     string
	SessionID string
	SortKey   string
	SortAsc   bool
	Offset    int
	Limit     int
}

func parseDashboardSessionsAPIRequest(values url.Values) (dashboardSessionsAPIRequest, error) {
	offset, err := parseAPIIntParam(values, apiIntParam{Name: "offset", Default: 0, Min: 0})
	if err != nil {
		return dashboardSessionsAPIRequest{}, err
	}
	limit, err := parseAPIIntParam(values, apiIntParam{
		Name:    "limit",
		Default: defaultSessionPageSize,
		Min:     1,
		Max:     maxDashboardSessionsAPILimit,
	})
	if err != nil {
		return dashboardSessionsAPIRequest{}, err
	}

	state := strings.TrimSpace(values.Get("state"))
	if state == "" {
		state = "completed"
	}
	return dashboardSessionsAPIRequest{
		State:     state,
		Range:     parseAPIRangeParam(values, "", "completed_range", "range", "search_range"),
		Query:     strings.TrimSpace(values.Get("q")),
		SessionID: strings.TrimSpace(values.Get("session_id")),
		SortKey:   strings.TrimSpace(values.Get("sort")),
		SortAsc:   strings.EqualFold(strings.TrimSpace(values.Get("direction")), "asc"),
		Offset:    offset,
		Limit:     limit,
	}, nil
}

type dashboardSearchAPIRequest struct {
	Query     string
	Range     string
	EventKind string
	SessionID string
	SortBy    string
	Limit     int
}

func parseDashboardSearchAPIRequest(values url.Values) (dashboardSearchAPIRequest, error) {
	limit, err := parseAPIIntParam(values, apiIntParam{
		Name:    "limit",
		Default: defaultSearchPageSize,
		Min:     1,
		Max:     maxDashboardSearchAPILimit,
	})
	if err != nil {
		return dashboardSearchAPIRequest{}, err
	}

	sortBy := strings.TrimSpace(values.Get("sort"))
	if sortBy == "" {
		sortBy = "relevance"
	}
	return dashboardSearchAPIRequest{
		Query:     strings.TrimSpace(values.Get("q")),
		Range:     parseAPIRangeParam(values, "", "completed_range", "range", "search_range"),
		EventKind: strings.TrimSpace(values.Get("event_kind")),
		SessionID: strings.TrimSpace(values.Get("session_id")),
		SortBy:    sortBy,
		Limit:     limit,
	}, nil
}

func (r dashboardSearchAPIRequest) active() bool {
	return r.Query != "" || r.Range != "" || r.EventKind != "" || r.SessionID != ""
}

type activityAPIRequest struct {
	Range      string
	Since      *time.Time
	EventKinds []string
}

func parseActivityAPIRequest(values url.Values) activityAPIRequest {
	rangeVal := parseAPIRangeParam(values, "24h", "activity_range", "range")
	return activityAPIRequest{
		Range:      rangeVal,
		Since:      parseRange(rangeVal),
		EventKinds: parseAPICSVParam(values, "event_kind"),
	}
}

type dashboardChartsAPIRequest struct {
	Range string
}

func parseDashboardChartsAPIRequest(values url.Values) dashboardChartsAPIRequest {
	return dashboardChartsAPIRequest{Range: parseAPIRangeParam(values, "", "chart_range", "range")}
}

type sessionEventsAPIRequest struct {
	Limit  int
	Offset int
}

func parseSessionEventsAPIRequest(values url.Values) (sessionEventsAPIRequest, error) {
	limit, err := parseAPIIntParam(values, apiIntParam{
		Name:    "limit",
		Default: defaultSessionEventsAPILimit,
		Min:     1,
		Max:     maxSessionEventsAPILimit,
	})
	if err != nil {
		return sessionEventsAPIRequest{}, err
	}
	offset, err := parseAPIIntParam(values, apiIntParam{Name: "offset", Default: 0, Min: 0})
	if err != nil {
		return sessionEventsAPIRequest{}, err
	}
	return sessionEventsAPIRequest{Limit: limit, Offset: offset}, nil
}

type searchEventsAPIRequest struct {
	Query string
	Limit int
}

func parseSearchEventsAPIRequest(values url.Values) (searchEventsAPIRequest, error) {
	query := strings.TrimSpace(values.Get("q"))
	if query == "" {
		return searchEventsAPIRequest{}, fmt.Errorf("missing query parameter 'q'")
	}
	limit, err := parseAPIIntParam(values, apiIntParam{
		Name:    "limit",
		Default: defaultSearchEventsAPILimit,
		Min:     1,
		Max:     maxSearchEventsAPILimit,
	})
	if err != nil {
		return searchEventsAPIRequest{}, err
	}
	return searchEventsAPIRequest{Query: query, Limit: limit}, nil
}
