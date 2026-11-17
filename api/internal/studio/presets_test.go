package studio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/provider"
	"github.com/ipedrazas/aifs/api/internal/provider/fake"
	"github.com/ipedrazas/aifs/api/internal/provider/gemini"
)

// renamed exposes a provider under another name, so tests can combine two
// providers without network access.
type renamed struct {
	provider.Provider
	name string
	caps []provider.Capability
}

func (r renamed) Name() string                        { return r.name }
func (r renamed) Capabilities() []provider.Capability { return r.caps }

func TestBuiltInPresets(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "")
	presets, err := e.svc.ListPresets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	envs := map[string]bool{}
	for _, p := range presets {
		if !p.BuiltIn {
			t.Fatalf("unexpected custom preset: %+v", p)
		}
		slugs = append(slugs, p.Slug)
		envs[p.Environment] = true
		// Presets set the scene; they never describe the garment.
		all := strings.ToLower(strings.Join([]string{p.Environment, p.Pose, p.Framing, p.Lighting, p.Style, p.PromptFragment}, " "))
		for _, word := range []string{"jeans", "denim", "trousers", "shirt", "dress", "garment", "fabric", "wash"} {
			if strings.Contains(all, word) {
				t.Errorf("preset %s mentions %q", p.Slug, word)
			}
		}
	}
	if strings.Join(slugs, ",") != "studio,urban,coffee-shop,outdoor" || len(envs) != 4 {
		t.Fatalf("presets: %v", slugs)
	}

	// Built-in presets are read-only, even to direct SQL.
	if _, err := e.db.Write.ExecContext(ctx, `UPDATE preset SET name = 'x' WHERE slug = 'studio'`); err == nil {
		t.Fatal("a built-in preset was updated")
	}
	if _, err := e.db.Write.ExecContext(ctx, `DELETE FROM preset WHERE slug = 'outdoor'`); err == nil {
		t.Fatal("a built-in preset was deleted")
	}

	// A new photoshoot uses the studio preset; another can be chosen.
	if e.shoot.Preset.Name != "Clean studio catalogue" || e.shoot.PresetID != presets[0].ID {
		t.Fatalf("default preset: %+v", e.shoot.Preset)
	}
	ps, err := e.svc.UpdatePhotoshoot(ctx, e.shoot.ID, PhotoshootInput{PresetID: &presets[3].ID})
	if err != nil || ps.Preset.Name != "Outdoor" {
		t.Fatalf("update preset: %+v %v", ps.Preset, err)
	}
	var ve *ValidationError
	if _, err := e.svc.UpdatePhotoshoot(ctx, e.shoot.ID, PhotoshootInput{PresetID: ptr(int64(999))}); !errors.As(err, &ve) {
		t.Fatalf("unknown preset: %v", err)
	}
	if _, err := e.svc.GetPreset(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing preset: %v", err)
	}
}

func TestPresetShapesThePromptAndIsSnapshotted(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "")
	presets, _ := e.svc.ListPresets(ctx)
	prompts := map[string]bool{}
	for _, p := range presets {
		g, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{
			Recipe: "reference", Provider: fake.Name, Variations: 1, PresetID: p.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if g.Settings.Preset.ID != p.ID || g.Settings.Preset.Environment != p.Environment || !g.Settings.Preset.BuiltIn {
			t.Fatalf("snapshot preset: %+v", g.Settings.Preset)
		}
		if !strings.Contains(g.Prompt, "Environment: "+p.Environment) || !strings.Contains(g.Prompt, "Lighting: "+p.Lighting) {
			t.Fatalf("prompt for %s:\n%s", p.Slug, g.Prompt)
		}
		prompts[g.Prompt] = true
	}
	if len(prompts) != 4 {
		t.Fatal("presets do not produce distinct prompts")
	}

	// Without a choice, the photoshoot's preset applies.
	if _, err := e.svc.UpdatePhotoshoot(ctx, e.shoot.ID, PhotoshootInput{PresetID: &presets[1].ID}); err != nil {
		t.Fatal(err)
	}
	g := e.generate("reference", 1)
	if g.Settings.Preset.Slug != "urban" {
		t.Fatalf("photoshoot preset not used: %+v", g.Settings.Preset)
	}
	var ve *ValidationError
	if _, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{
		Recipe: "reference", Provider: fake.Name, Variations: 1, PresetID: 999,
	}); !errors.As(err, &ve) {
		t.Fatalf("unknown preset: %v", err)
	}
}

// FR-05: the garment is defined by its photographs. The only garment text in
// a prompt is what the user typed on the product.
func TestPromptsCarryNoMachineWrittenGarmentDescription(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "")
	bare, _ := e.cat.CreateProduct(ctx, catalog.ProductInput{
		Name: ptr("SKU 1"), Description: ptr("internal description"), Notes: ptr("internal notes"),
	})
	if _, err := e.cat.AddProductImage(ctx, bare.ID, "front", "f.png", fixture(t, "plain.png")); err != nil {
		t.Fatal(err)
	}
	ps, _ := e.svc.CreatePhotoshoot(ctx, PhotoshootInput{ProductID: &bare.ID, ModelID: &e.model.ID})
	for _, recipe := range []string{"reference", "tryon", "tryon-photo"} {
		g, err := e.svc.StartGeneration(ctx, ps.ID, GenerationRequest{Recipe: recipe, Provider: fake.Name, Variations: 1})
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(g.Prompt, "\n") {
			label, _, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			switch label {
			case "Colour", "Print details":
				t.Errorf("%s: garment line without user text: %q", recipe, line)
			}
		}
		for _, private := range []string{"internal description", "internal notes"} {
			if strings.Contains(g.Prompt, private) {
				t.Errorf("%s: prompt carries %q", recipe, private)
			}
		}
	}
	// A product's own fields appear verbatim, and nothing else describes it.
	g := e.generate("reference", 1)
	if !strings.Contains(g.Prompt, "Colour: indigo\n") || !strings.Contains(g.Prompt, "Print details: embroidered pocket\n") {
		t.Fatalf("prompt:\n%s", g.Prompt)
	}
}

// The tryon recipe can take its scene from one provider and the try-on from
// another; both recipes' results sit together in the photoshoot.
func TestMixedProvidersAndRecipesTogether(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "")
	scene := renamed{Provider: fake.New(0), name: "scenes", caps: []provider.Capability{provider.ReferenceGenerate}}
	e.reg.Add(scene, 5_000)
	e.start()

	var ve *ValidationError
	for name, req := range map[string]GenerationRequest{
		"capability unsupported": {Recipe: "tryon", Provider: "scenes", Variations: 1},
		"unknown step":           {Recipe: "reference", Provider: fake.Name, Providers: map[string]string{"try_on": fake.Name}, Variations: 1},
		"unconfigured":           {Recipe: "tryon", Provider: fake.Name, Providers: map[string]string{"reference_generate": "gemini"}, Variations: 1},
		"no provider":            {Recipe: "reference", Variations: 1},
	} {
		if _, err := e.svc.StartGeneration(ctx, e.shoot.ID, req); !errors.As(err, &ve) {
			t.Errorf("%s: %v", name, err)
		}
	}

	mixed, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{
		Recipe: "tryon", Variations: 1,
		Providers: map[string]string{"reference_generate": "scenes", "try_on": fake.Name},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{
		Recipe: "reference", Variations: 1, Providers: map[string]string{"reference_generate": "scenes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mixed = e.waitStatus(mixed.ID, StatusSucceeded)
	ref = e.waitStatus(ref.ID, StatusSucceeded)

	if mixed.Provider != fake.Name || mixed.Settings.Steps[0].Provider != "scenes" || mixed.Settings.Steps[1].Provider != fake.Name {
		t.Fatalf("mixed plan: %s %+v", mixed.Provider, mixed.Settings.Steps)
	}
	if mixed.Steps[0].Provider != "scenes" || mixed.CostMicros != 2*fake.CostMicros {
		t.Fatalf("mixed steps: %+v cost %d", mixed.Steps, mixed.CostMicros)
	}
	if ref.Provider != "scenes" {
		t.Fatalf("reference provider: %s", ref.Provider)
	}

	images, err := e.svc.ListImages(ctx, ImageFilter{PhotoshootID: e.shoot.ID})
	if err != nil {
		t.Fatal(err)
	}
	recipes := map[string]bool{}
	for _, im := range images {
		recipes[im.Recipe] = true
	}
	if len(images) != 2 || !recipes["tryon"] || !recipes["reference"] {
		t.Fatalf("images: %+v", images)
	}
}

// The Gemini adapter end to end through the durable runner, against a
// replayed OpenRouter response.
func TestGeminiThroughRunner(t *testing.T) {
	ok, err := os.ReadFile(filepath.Join("..", "provider", "gemini", "testdata", "images_ok.json"))
	if err != nil {
		t.Fatal(err)
	}
	var prompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		_ = jsonDecode(r, &body)
		prompt = body.Prompt
		_, _ = w.Write(ok)
	}))
	defer srv.Close()

	e := newEnv(t, 0, "")
	gp, err := gemini.New(gemini.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	e.reg.Add(gp, gp.UnitPriceMicros())
	e.start()

	g, err := e.svc.StartGeneration(context.Background(), e.shoot.ID, GenerationRequest{Recipe: "reference", Provider: gemini.Name, Variations: 1})
	if err != nil {
		t.Fatal(err)
	}
	g = e.waitStatus(g.ID, StatusSucceeded)
	if len(g.Images) != 1 || g.CostMicros != 136_900 || g.Settings.Steps[0].ProviderModel != gemini.DefaultModel+" (2K)" {
		t.Fatalf("generation: %+v", g)
	}
	// What was sent is the stored prompt behind a legend of the stored inputs.
	if !strings.HasSuffix(prompt, g.Settings.Steps[0].Prompt) || !strings.HasPrefix(prompt, "Attached images, in order:\n1. garment reference photograph (front)") {
		t.Fatalf("sent prompt:\n%s", prompt)
	}
}

func jsonDecode(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
