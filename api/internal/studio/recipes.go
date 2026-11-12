package studio

import (
	"fmt"
	"strings"

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/provider"
)

// A Recipe is an ordered list of provider calls that turns references into
// a photograph. Recipes let techniques be combined without assuming one
// model does everything.
type Recipe struct {
	Name        string
	Label       string
	Description string
	Steps       []StepSpec
}

// StepSpec describes one call of a recipe.
type StepSpec struct {
	Capability provider.Capability
	// Purpose is a short label shown in the UI ("scene", "try-on").
	Purpose string
	// UsesGarments, UsesModel and UsesPrevious select the inputs.
	UsesGarments bool
	// GarmentLimit caps the garment images sent (0 = all, up to MaxGarments).
	GarmentLimit int
	UsesModel    bool
	UsesPrevious bool
	// Prompt builds the step's prompt.
	Prompt func(PromptInput) string
}

// Input limits per call.
const (
	MaxGarments  = 4
	MaxModelRefs = 3
)

// Recipes available to generations.
var Recipes = []Recipe{
	{
		Name:        "reference",
		Label:       "Reference-guided generation",
		Description: "One call with the garment photographs, the model references and the scene prompt (approach B).",
		Steps: []StepSpec{{
			Capability:   provider.ReferenceGenerate,
			Purpose:      "photograph",
			UsesGarments: true,
			UsesModel:    true,
			Prompt:       referencePrompt,
		}},
	},
	{
		Name:        "tryon",
		Label:       "Scene, then virtual try-on",
		Description: "Generate the model in the scene, then dress them with the garment photograph (approach A).",
		Steps: []StepSpec{
			{
				Capability: provider.ReferenceGenerate,
				Purpose:    "scene",
				UsesModel:  true,
				Prompt:     scenePrompt,
			},
			{
				Capability:   provider.TryOn,
				Purpose:      "try-on",
				UsesGarments: true,
				GarmentLimit: 1,
				UsesPrevious: true,
				Prompt:       func(PromptInput) string { return "" },
			},
		},
	},
}

// RecipeByName finds a recipe.
func RecipeByName(name string) (Recipe, bool) {
	for _, r := range Recipes {
		if r.Name == name {
			return r, true
		}
	}
	return Recipe{}, false
}

// Capabilities lists the capabilities a recipe needs.
func (r Recipe) Capabilities() []provider.Capability {
	out := make([]provider.Capability, 0, len(r.Steps))
	for _, s := range r.Steps {
		out = append(out, s.Capability)
	}
	return out
}

// SupportedBy reports whether one provider can run every step.
func (r Recipe) SupportedBy(p provider.Provider) bool {
	for _, c := range r.Capabilities() {
		if !provider.Supports(p, c) {
			return false
		}
	}
	return true
}

// PromptInput is the user-entered text a prompt may use. Garment text is
// limited to what the user typed on the product: the reference photographs,
// never a machine-written description, define the garment.
type PromptInput struct {
	Product            catalog.Product
	Model              catalog.Model
	AspectRatio        string
	CustomInstructions string
}

func line(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%s: %s\n", label, value)
}

const fidelityRule = "Reproduce the garment exactly as it appears in its reference photographs: the same cut, fit, colour, wash, " +
	"stitching, hardware, prints, embroidery and every other detail. Do not invent, remove, simplify or alter any design element."

const identityRule = "Keep the person's face, skin tone, hair and build consistent with the model reference images."

const defaultScene = "Clean studio catalogue shot: plain light-grey seamless background, soft even lighting, full-body framing, natural standing pose."

func referencePrompt(in PromptInput) string {
	var b strings.Builder
	b.WriteString("Professional e-commerce fashion photograph. The person shown in the model reference images is wearing " +
		"the garment shown in the garment reference images.\n")
	b.WriteString(fidelityRule + "\n" + identityRule + "\n\n")
	b.WriteString(line("Garment", in.Product.Name))
	b.WriteString(line("Category", in.Product.Category))
	b.WriteString(line("Colour", in.Product.Colour))
	b.WriteString(line("Print details", in.Product.PrintDetails))
	b.WriteString(line("Model", in.Model.Appearance))
	b.WriteString(line("Styling", in.Model.Styling))
	b.WriteString(line("Pose", in.Model.PreferredPoses))
	b.WriteString(line("Scene", defaultScene))
	b.WriteString(line("Aspect ratio", in.AspectRatio))
	b.WriteString(line("Additional instructions", in.CustomInstructions))
	return strings.TrimSpace(b.String())
}

func scenePrompt(in PromptInput) string {
	var b strings.Builder
	b.WriteString("Professional fashion photograph of the person shown in the model reference images, " +
		"wearing simple, close-fitting plain clothing so a garment can be fitted onto them afterwards.\n")
	b.WriteString(identityRule + "\n\n")
	b.WriteString(line("Model", in.Model.Appearance))
	b.WriteString(line("Pose", in.Model.PreferredPoses))
	b.WriteString(line("Scene", defaultScene))
	b.WriteString(line("Aspect ratio", in.AspectRatio))
	b.WriteString(line("Additional instructions", in.CustomInstructions))
	return strings.TrimSpace(b.String())
}
