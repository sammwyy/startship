package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogRefreshAccess(t *testing.T) {
	f := newRBACFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/recipes" {
			fmt.Fprint(w, `{"recipes":[]}`)
		} else {
			fmt.Fprint(w, `{"engines":[]}`)
		}
	}))
	defer upstream.Close()
	t.Setenv("STARTSHIP_RECIPES_URL", upstream.URL+"/recipes")
	t.Setenv("STARTSHIP_SERVICES_URL", upstream.URL+"/services")
	f.a.initCatalogs()
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	scope := "?workspace=" + f.workspace.ID
	accountRequest(t, f.h, "POST", "catalog/refresh", "", f.bob, 403)
	accountRequest(t, f.h, "POST", "recipes/refresh"+scope, "", f.bob, 403)
	accountRequest(t, f.h, "POST", "catalog/refresh"+scope, "", f.bob, 200)
	accountRequest(t, f.h, "POST", "recipes/refresh"+scope, "", f.alice, 200)
	for _, resource := range []string{"catalog", "recipes"} {
		w := accountRequest(t, f.h, "GET", resource, "", f.bob, 200)
		if got := decodeBody[[]any](t, w.Body.Bytes()); len(got) != 0 {
			t.Fatalf("remote did not replace bundled %s: %v", resource, got)
		}
	}
	_, _, _, why := f.a.liveData(context.Background(), f.bobID, &liveSub{Topic: "services_catalog"})
	if why != "" {
		t.Fatal(why)
	}
	legacy, ok := (Service{Engine: "redis"}).engineDefinition()
	if !ok || legacy.Port != 6379 {
		t.Fatal("bundled engine disappeared for legacy installs")
	}
	legacy.Port = 6000
	saved, ok := (Service{Engine: "redis", Definition: &legacy}).engineDefinition()
	if !ok || saved.Port != 6000 {
		t.Fatal("saved engine definition not used")
	}
}
