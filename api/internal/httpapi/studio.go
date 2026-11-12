package httpapi

import (
	"net/http"
	"strconv"

	"github.com/ipedrazas/aifs/api/internal/studio"
)

type studioHandlers struct {
	d Deps
}

func (h studioHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	failCatalog(h.d, w, r, err)
}

// PhotoshootList is the body of GET /api/v1/photoshoots.
type PhotoshootList struct {
	Items []studio.Photoshoot `json:"items"`
}

func queryID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return n
}

func (h studioHandlers) listPhotoshoots(w http.ResponseWriter, r *http.Request) {
	items, err := h.d.Studio.ListPhotoshoots(r.Context(), queryID(r, "productId"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, PhotoshootList{Items: items})
}

func (h studioHandlers) createPhotoshoot(w http.ResponseWriter, r *http.Request) {
	var in studio.PhotoshootInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ps, err := h.d.Studio.CreatePhotoshoot(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/photoshoots/"+strconv.FormatInt(ps.ID, 10))
	writeJSON(w, http.StatusCreated, ps)
}

func (h studioHandlers) getPhotoshoot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ps, err := h.d.Studio.GetPhotoshoot(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (h studioHandlers) updatePhotoshoot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in studio.PhotoshootInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ps, err := h.d.Studio.UpdatePhotoshoot(r.Context(), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (h studioHandlers) deletePhotoshoot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.d.Studio.DeletePhotoshoot(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startGeneration queues a generation and returns at once (202).
func (h studioHandlers) startGeneration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req studio.GenerationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	g, err := h.d.Studio.StartGeneration(r.Context(), id, req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/generations/"+strconv.FormatInt(g.ID, 10))
	writeJSON(w, http.StatusAccepted, g)
}

func (h studioHandlers) getGeneration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	g, err := h.d.Studio.GetGeneration(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (h studioHandlers) cancelGeneration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	g, err := h.d.Studio.CancelGeneration(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// ImageList is the body of GET /api/v1/images.
type ImageList struct {
	Items []studio.GeneratedImage `json:"items"`
}

func (h studioHandlers) listImages(w http.ResponseWriter, r *http.Request) {
	items, err := h.d.Studio.ListImages(r.Context(), studio.ImageFilter{
		ProductID:    queryID(r, "productId"),
		PhotoshootID: queryID(r, "photoshootId"),
		ReviewStatus: r.URL.Query().Get("status"),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ImageList{Items: items})
}

func (h studioHandlers) getImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	im, err := h.d.Studio.GetImage(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, im)
}

// ProviderInfo describes a configured provider.
type ProviderInfo struct {
	Name            string            `json:"name"`
	Label           string            `json:"label"`
	Capabilities    []string          `json:"capabilities"`
	Models          map[string]string `json:"models"`
	UnitPriceMicros int64             `json:"unitPriceMicros"`
	Recipes         []string          `json:"recipes"`
}

// RecipeInfo describes a recipe.
type RecipeInfo struct {
	Name        string           `json:"name"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	Steps       []RecipeStepInfo `json:"steps"`
}

// RecipeStepInfo is one step of a recipe.
type RecipeStepInfo struct {
	Capability string `json:"capability"`
	Purpose    string `json:"purpose"`
}

// ProvidersResponse is the body of GET /api/v1/providers.
type ProvidersResponse struct {
	Providers     []ProviderInfo `json:"providers"`
	Recipes       []RecipeInfo   `json:"recipes"`
	MaxVariations int            `json:"maxVariations"`
}

func (h studioHandlers) providers(w http.ResponseWriter, _ *http.Request) {
	resp := ProvidersResponse{Providers: []ProviderInfo{}, Recipes: []RecipeInfo{}, MaxVariations: h.d.MaxVariations}
	for _, rc := range studio.Recipes {
		info := RecipeInfo{Name: rc.Name, Label: rc.Label, Description: rc.Description}
		for _, st := range rc.Steps {
			info.Steps = append(info.Steps, RecipeStepInfo{Capability: string(st.Capability), Purpose: st.Purpose})
		}
		resp.Recipes = append(resp.Recipes, info)
	}
	for _, p := range h.d.Providers.All() {
		info := ProviderInfo{
			Name: p.Name(), Label: p.Label(), Models: map[string]string{},
			UnitPriceMicros: h.d.Providers.UnitPrice(p.Name()), Recipes: []string{},
		}
		for _, c := range p.Capabilities() {
			info.Capabilities = append(info.Capabilities, string(c))
			info.Models[string(c)] = p.Model(c)
		}
		for _, rc := range studio.Recipes {
			if rc.SupportedBy(p) {
				info.Recipes = append(info.Recipes, rc.Name)
			}
		}
		resp.Providers = append(resp.Providers, info)
	}
	writeJSON(w, http.StatusOK, resp)
}
