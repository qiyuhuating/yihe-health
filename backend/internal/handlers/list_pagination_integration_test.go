package handlers

import (
	"net/http"
	"reflect"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestIntegrationListPageTotals(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedCareAdminDuty(t, pool, 91, "supervisor")
	mustExec(t, pool, `INSERT INTO customer(customer_id,customer_name) VALUES
		(4,'match A'),(7,'match B'),(9,'other'),(12,'match C')`)
	mustExec(t, pool, `INSERT INTO family(family_id,family_name,family_tou_pic) VALUES (2,'match fam',NULL),(3,'other',NULL)`)
	mustExec(t, pool, `INSERT INTO oldperson(old_id,name,family_id) VALUES
		(4,NULL,2),(7,'second',2),(9,'other',3),(12,'third',2),(15,'unbound',NULL)`)
	mustExec(t, pool, `INSERT INTO text(text_id,title,time) VALUES
		(4,'match A','2026-10-19 08:01:02.123'),(7,'match B','2026-10-19 08:01:02.123'),
		(9,'other','2026-10-20 08:01:02.123'),(12,'match C','2026-10-18 08:01:02.123'),(15,NULL,NULL)`)
	textOrder := []int{9, 7, 4, 12, 15}
	for _, endpoint := range []struct {
		name, key, idKey, filterKey string
		handler                     http.HandlerFunc
		all, matched                []int
		nullKeys                    []string
		emptySQL                    string
	}{
		{"customers", "customers", "customerId", "customerName", withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 91}, h.listGuardians),
			[]int{4, 7, 9, 12}, []int{4, 7, 12}, []string{"customerAge", "customerAddress"}, "DELETE FROM customer"},
		{"family", "familyDtos", "oldId", "familyName", withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 91}, h.familySelect),
			[]int{4, 7, 9, 12}, []int{4, 7, 12}, []string{"familyAddress", "familyTouPic"}, "DELETE FROM oldperson"},
		{"texts", "texts", "textId", "title", h.listNotices,
			textOrder, []int{7, 4, 12}, []string{"textMain", "picId"}, "DELETE FROM text"},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, test := range []struct {
				name, filter string
				page, size   int
				total        int
				ids          []int
			}{
				{"first", "", 1, 2, len(endpoint.all), endpoint.all[:2]},
				{"next", "", 2, 2, len(endpoint.all), endpoint.all[2:4]},
				{"filtered", "match", 1, 2, 3, endpoint.matched[:2]},
				{"full_filtered", "match", 1, 3, 3, endpoint.matched},
				{"partial", "match", 2, 2, 3, endpoint.matched[2:]},
				{"exact_boundary", "match", 2, 3, 3, nil},
				{"out_of_range", "match", 100000, 2, 3, nil},
				{"empty_first", "absent", 1, 2, 0, nil},
				{"empty_later", "absent", 2, 2, 0, nil},
			} {
				t.Run(test.name, func(t *testing.T) {
					out := api(t, endpoint.handler, map[string]any{
						endpoint.filterKey: test.filter, "page": test.page, "pageSize": test.size,
					})
					items, ok := out[endpoint.key].([]any)
					if !ok || out["total"] != float64(test.total) || out["page"] != float64(test.page) || out["pageSize"] != float64(test.size) {
						t.Fatalf("page contract: %#v", out)
					}
					var ids []int
					for _, item := range items {
						row := item.(map[string]any)
						ids = append(ids, int(row[endpoint.idKey].(float64)))
						for _, key := range endpoint.nullKeys {
							if value, exists := row[key]; !exists || value != nil {
								t.Fatalf("nullable field %s: %#v", key, row)
							}
						}
						if endpoint.name == "texts" && row["textId"] == float64(7) && row["time"] != "2026-10-19 08:01:02.123" {
							t.Fatalf("formatted timestamp: %#v", row)
						}
					}
					if !reflect.DeepEqual(ids, test.ids) {
						t.Fatalf("IDs=%v want=%v body=%#v", ids, test.ids, out)
					}
				})
			}
			// Real empty tables also return [] rather than a synthetic NULL row.
			mustExec(t, pool, endpoint.emptySQL)
			for _, page := range []int{1, 2} {
				out := api(t, endpoint.handler, map[string]any{"page": page, "pageSize": 2})
				items, ok := out[endpoint.key].([]any)
				if !ok || len(items) != 0 || out["total"] != float64(0) {
					t.Fatalf("empty table page %d: %#v", page, out)
				}
			}
		})
	}
}
