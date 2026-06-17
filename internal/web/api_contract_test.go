package web

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

type apiContract struct {
	Required map[string]string `json:"required"`
	Optional map[string]string `json:"optional"`
}

func TestAPIContractsMatchGoStructTags(t *testing.T) {
	contracts := loadAPIContracts(t)
	types := map[string]reflect.Type{
		"APISessionSummary":              reflect.TypeOf(APISessionSummary{}),
		"APIDashboardSessionsResponse":   reflect.TypeOf(APIDashboardSessionsResponse{}),
		"APIDashboardSearchResult":       reflect.TypeOf(APIDashboardSearchResult{}),
		"APIDashboardSearchResponse":     reflect.TypeOf(APIDashboardSearchResponse{}),
		"APIScopeMetadata":               reflect.TypeOf(APIScopeMetadata{}),
		"APIScopeFilters":                reflect.TypeOf(APIScopeFilters{}),
		"APIActivityItem":                reflect.TypeOf(APIActivityItem{}),
		"APIDashboardCharts":             reflect.TypeOf(APIDashboardCharts{}),
		"ModelSeriesChart":               reflect.TypeOf(views.ModelSeriesChart{}),
		"ModelMetricChart":               reflect.TypeOf(views.ModelMetricChart{}),
		"ModelMetricSeries":              reflect.TypeOf(views.ModelMetricSeries{}),
		"ModelSeriesDataset":             reflect.TypeOf(views.ModelSeriesDataset{}),
		"ModelAnalyticsSummary":          reflect.TypeOf(views.ModelAnalyticsSummary{}),
		"APISessionDetail":               reflect.TypeOf(APISessionDetail{}),
		"APISessionEvent":                reflect.TypeOf(APISessionEvent{}),
		"APIToolPayload":                 reflect.TypeOf(APIToolPayload{}),
		"APITraceAnnotation":             reflect.TypeOf(APITraceAnnotation{}),
		"APITraceAnnotationListResponse": reflect.TypeOf(APITraceAnnotationListResponse{}),
	}

	for name, contract := range contracts {
		typ, ok := types[name]
		if !ok {
			t.Fatalf("contract %s has no Go type mapping", name)
		}
		t.Run(name, func(t *testing.T) {
			required, optional := jsonContractFields(t, typ)
			if !reflect.DeepEqual(required, contract.Required) {
				t.Fatalf("required fields mismatch\n got: %v\nwant: %v", sortedMap(required), sortedMap(contract.Required))
			}
			if !reflect.DeepEqual(optional, contract.Optional) {
				t.Fatalf("optional fields mismatch\n got: %v\nwant: %v", sortedMap(optional), sortedMap(contract.Optional))
			}
		})
	}
	for name := range types {
		if _, ok := contracts[name]; !ok {
			t.Fatalf("Go type mapping %s has no shared contract", name)
		}
	}
}

func TestAPISessionDetailFromViewUsesStableJSONContract(t *testing.T) {
	payload := apiSessionDetailFromView(views.SessionDetailData{
		Session: views.SessionSummary{
			ID:          "session-contract",
			Actor:       "codex",
			Provider:    "openai",
			Status:      "completed",
			ActiveModel: "gpt-5.4",
			WorkingDir:  "/tmp/beacon",
		},
	})
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal session detail contract: %v", err)
	}
	got := string(body)
	for _, want := range []string{`"session":`, `"id":"session-contract"`, `"last_model":"gpt-5.4"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("session detail payload missing %s: %s", want, got)
		}
	}
	for _, legacy := range []string{`"Session"`, `"ActiveModel"`, `"LastModel"`} {
		if strings.Contains(got, legacy) {
			t.Fatalf("session detail payload contains legacy field %s: %s", legacy, got)
		}
	}
}

func loadAPIContracts(t *testing.T) map[string]apiContract {
	t.Helper()
	body, err := os.ReadFile("../../tests/contracts/api-contracts.json")
	if err != nil {
		t.Fatalf("read api contracts: %v", err)
	}
	var contracts map[string]apiContract
	if err := json.Unmarshal(body, &contracts); err != nil {
		t.Fatalf("parse api contracts: %v", err)
	}
	return contracts
}

func jsonContractFields(t *testing.T, typ reflect.Type) (map[string]string, map[string]string) {
	t.Helper()
	required := map[string]string{}
	optional := map[string]string{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name, omitempty, ok := jsonFieldName(field)
		if !ok {
			continue
		}
		target := required
		if omitempty {
			target = optional
		}
		target[name] = jsonContractType(t, field.Type)
	}
	return required, optional
}

func jsonFieldName(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}
	if tag == "" {
		return field.Name, false, true
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	omitempty := false
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, true
}

func jsonContractType(t *testing.T, typ reflect.Type) string {
	t.Helper()
	if typ == reflect.TypeOf(time.Time{}) {
		return "string"
	}
	switch typ.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Pointer:
		return jsonContractType(t, typ.Elem())
	case reflect.Slice, reflect.Array:
		return jsonContractType(t, typ.Elem()) + "[]"
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			t.Fatalf("unsupported non-string map key type %s", typ)
		}
		return "map:" + jsonContractType(t, typ.Elem())
	case reflect.Struct:
		return typ.Name()
	default:
		t.Fatalf("unsupported contract type %s", typ)
		return ""
	}
}

func sortedMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ", ")
}
