package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sammwyy/startship/internal/catalog"
	"github.com/sammwyy/startship/internal/recipes"
	"github.com/sammwyy/startship/registry"
)

const registryBase = "https://raw.githubusercontent.com/sammwyy/startship/main/registry/"

func registryURL(variable, name string) string {
	if value := os.Getenv(variable); value != "" {
		return value
	}
	return registryBase + name + ".json"
}

func (a *Server) initCatalogs() {
	a.recipes = registry.NewCache(filepath.Join(a.data, "recipes-cache.json"),
		registryURL("STARTSHIP_RECIPES_URL", "recipes"), builtinRecipes, recipes.Parse,
		func() { a.live.publish(liveEvent{changeKey: "recipes_catalog"}) })
	a.serviceCatalog = registry.NewCache(filepath.Join(a.data, "services-cache.json"),
		registryURL("STARTSHIP_SERVICES_URL", "services"), catalog.Registry(catalog.All()), catalog.Parse,
		func() { a.live.publish(liveEvent{changeKey: "services_catalog"}) })
}

func (a *Server) recipeRegistry() (recipes.Registry, registry.Metadata) {
	if a.recipes != nil {
		return a.recipes.Snapshot()
	}
	return builtinRecipes, registry.Metadata{Source: "bundled"}
}

func (a *Server) serviceRegistry() (catalog.Registry, registry.Metadata) {
	if a.serviceCatalog != nil {
		return a.serviceCatalog.Snapshot()
	}
	return catalog.Registry(catalog.All()), registry.Metadata{Source: "bundled"}
}

func (a *Server) catalogData(topic string) (any, registry.Metadata) {
	if topic == "recipes_catalog" {
		index, meta := a.recipeRegistry()
		entries := index.All()
		for i := range entries {
			bundled := builtinRecipes[entries[i].ID]
			if entries[i].Category == "" {
				entries[i].Category = bundled.Category
			}
			if entries[i].Logo == "" {
				entries[i].Logo = bundled.Logo
			}
		}
		return entries, meta
	}
	index, meta := a.serviceRegistry()
	return index.All(), meta
}

func (a *Server) ensureCatalog(topic string) {
	if topic == "recipes_catalog" {
		if a.recipes != nil {
			a.recipes.Ensure()
		}
	} else if a.serviceCatalog != nil {
		a.serviceCatalog.Ensure()
	}
}

func (a *Server) catalogAPI(w http.ResponseWriter, r *http.Request, p []string, x *access) bool {
	if len(p) < 2 || (p[1] != "recipes" && p[1] != "catalog") {
		return false
	}
	isRecipes := p[1] == "recipes"
	topic, perm := "services_catalog", "services.manage"
	var cache interface {
		Refresh(context.Context) error
	}
	if isRecipes {
		topic, perm = "recipes_catalog", "apps.manage"
		if a.recipes != nil {
			cache = a.recipes
		}
	} else if a.serviceCatalog != nil {
		cache = a.serviceCatalog
	}
	if len(p) > 3 || len(p) == 3 && !isRecipes && p[2] != "status" && p[2] != "refresh" {
		fail(w, 404, "not found")
		return true
	}
	if len(p) == 3 && p[2] == "refresh" && r.Method == http.MethodPost {
		ws := r.URL.Query().Get("workspace")
		allowed := x.platform(perm)
		if ws != "" {
			record, err := a.store.Get(ws)
			allowed = err == nil && record.Kind == "workspaces" && x.visible(record) && x.can(record, perm)
		}
		if !allowed {
			forbidden(w)
		} else if cache == nil || cache.Refresh(r.Context()) != nil {
			fail(w, 502, "could not refresh catalog; previous catalog kept")
		} else {
			entries, meta := a.catalogData(topic)
			reply(w, 200, map[string]any{"entries": entries, "metadata": meta})
		}
		return true
	}
	if r.Method != http.MethodGet || len(p) == 3 && p[2] == "refresh" {
		fail(w, 405, "method not allowed")
		return true
	}
	a.ensureCatalog(topic)
	switch {
	case len(p) == 2:
		entries, _ := a.catalogData(topic)
		reply(w, 200, entries)
	case p[2] == "status":
		_, meta := a.catalogData(topic)
		reply(w, 200, meta)
	default:
		index, _ := a.recipeRegistry()
		if recipe, ok := index[p[2]]; ok {
			reply(w, 200, recipe)
		} else {
			fail(w, 404, "recipe not found")
		}
	}
	return true
}
