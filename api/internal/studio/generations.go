package studio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/provider"
)

// Generation statuses.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Step statuses.
const (
	StepPending   = "pending"
	StepSubmitted = "submitted"
	StepSucceeded = "succeeded"
	StepFailed    = "failed"
	StepCancelled = "cancelled"
)

// Snapshot is the complete, immutable record of what a generation was asked
// to do. It is stored as JSON with the generation.
type Snapshot struct {
	Recipe string `json:"recipe"`
	// Provider made the final image; Steps record the provider of each call.
	Provider           string            `json:"provider"`
	Variations         int               `json:"variations"`
	AspectRatio        string            `json:"aspectRatio"`
	CustomInstructions string            `json:"customInstructions"`
	Photoshoot         SnapshotRef       `json:"photoshoot"`
	Product            SnapshotProduct   `json:"product"`
	Model              SnapshotModel     `json:"model"`
	Preset             SnapshotPreset    `json:"preset"`
	Steps              []SnapshotStep    `json:"steps"`
	Inputs             []GenerationInput `json:"inputs"`
}

// SnapshotRef names a record at the time of the generation.
type SnapshotRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// SnapshotProduct is the product as it was when generating.
type SnapshotProduct struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	SKU          string `json:"sku"`
	Category     string `json:"category"`
	Colour       string `json:"colour"`
	PrintDetails string `json:"printDetails"`
}

// SnapshotModel is the model as it was when generating.
type SnapshotModel struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Appearance     string `json:"appearance"`
	PreferredPoses string `json:"preferredPoses"`
	Styling        string `json:"styling"`
}

// SnapshotPreset is the preset as it was when generating.
type SnapshotPreset struct {
	ID             int64  `json:"id"`
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	BuiltIn        bool   `json:"builtIn"`
	Environment    string `json:"environment"`
	Pose           string `json:"pose"`
	Framing        string `json:"framing"`
	Lighting       string `json:"lighting"`
	Style          string `json:"style"`
	PromptFragment string `json:"promptFragment"`
}

// SnapshotStep is one planned provider call (the same for every variation).
type SnapshotStep struct {
	Position      int    `json:"position"`
	Capability    string `json:"capability"`
	Purpose       string `json:"purpose"`
	Provider      string `json:"provider"`
	ProviderModel string `json:"providerModel"`
	Prompt        string `json:"prompt"`
	// Inputs are positions in Snapshot.Inputs sent with this step.
	Inputs       []int `json:"inputs"`
	UsesPrevious bool  `json:"usesPrevious"`
	// Source is the position in Snapshot.Inputs of the photograph dressed
	// by a try-on step (0 when the step dresses the previous step's output
	// or has no source).
	Source int `json:"source"`
}

// GenerationInput is a reference photograph sent to the provider.
type GenerationInput struct {
	Position int    `json:"position"`
	Role     string `json:"role"`  // garment or model
	Label    string `json:"label"` // product view or model image role
	Hash     string `json:"hash"`
	// Processed marks a garment photograph that is a processed version
	// (background removed, trimmed or resized) rather than an upload.
	Processed bool `json:"processed,omitempty"`
}

// Generation is a request for N variations through a recipe.
type Generation struct {
	ID              int64            `json:"id"`
	PhotoshootID    int64            `json:"photoshootId"`
	ProductID       int64            `json:"productId"`
	ModelID         int64            `json:"modelId"`
	Recipe          string           `json:"recipe"`
	Provider        string           `json:"provider"`
	Variations      int              `json:"variations"`
	Status          string           `json:"status"`
	Error           string           `json:"error"`
	CostMicros      int64            `json:"costMicros"`
	CancelRequested bool             `json:"cancelRequested"`
	Prompt          string           `json:"prompt"`
	CreatedAt       time.Time        `json:"createdAt"`
	StartedAt       *time.Time       `json:"startedAt"`
	FinishedAt      *time.Time       `json:"finishedAt"`
	StepsTotal      int              `json:"stepsTotal"`
	StepsDone       int              `json:"stepsDone"`
	Settings        Snapshot         `json:"settings"`
	Images          []GeneratedImage `json:"images"`
	// Steps is only filled by GetGeneration.
	Steps []Step `json:"steps,omitempty"`
}

// Step is one provider call.
type Step struct {
	ID            int64      `json:"id"`
	Variation     int        `json:"variation"`
	Position      int        `json:"position"`
	Capability    string     `json:"capability"`
	Provider      string     `json:"provider"`
	ProviderModel string     `json:"providerModel"`
	ProviderJobID string     `json:"providerJobId"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	Error         string     `json:"error"`
	CostMicros    int64      `json:"costMicros"`
	OutputHash    string     `json:"outputHash"`
	StartedAt     *time.Time `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
}

// GeneratedImage is a produced photograph with what made it.
type GeneratedImage struct {
	ID           int64  `json:"id"`
	GenerationID int64  `json:"generationId"`
	Variation    int    `json:"variation"`
	Hash         string `json:"hash"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ReviewStatus string `json:"reviewStatus"`
	// RejectionReason is set when ReviewStatus is "rejected".
	RejectionReason string         `json:"rejectionReason"`
	ReviewerNotes   string         `json:"reviewerNotes"`
	ReviewedAt      *time.Time     `json:"reviewedAt"`
	CreatedAt       time.Time      `json:"createdAt"`
	Recipe          string         `json:"recipe"`
	Provider        string         `json:"provider"`
	PhotoshootID    int64          `json:"photoshootId"`
	Product         ProductSummary `json:"product"`
	Model           ModelSummary   `json:"model"`
}

// GenerationRequest starts a generation.
type GenerationRequest struct {
	Recipe string `json:"recipe"`
	// Provider runs every step that Providers does not assign.
	Provider string `json:"provider"`
	// Providers optionally assigns a provider per capability, so a recipe
	// can combine services (e.g. a Gemini scene, then a FASHN try-on).
	Providers  map[string]string `json:"providers"`
	Variations int               `json:"variations"`
	// PresetID chooses the scene; 0 uses the photoshoot's preset.
	PresetID int64 `json:"presetId"`
	// GarmentImageID optionally chooses the product photograph sent first
	// (and alone, for try-on). Defaults to the front view.
	GarmentImageID int64 `json:"garmentImageId"`
}

// StartGeneration validates a request, plans every step and queues it. It
// returns immediately; the runner does the work.
func (s *Service) StartGeneration(ctx context.Context, photoshootID int64, req GenerationRequest) (Generation, error) {
	if req.Variations < 1 || req.Variations > s.cfg.MaxVariations {
		return Generation{}, invalid("variations", fmt.Sprintf("must be between 1 and %d", s.cfg.MaxVariations))
	}
	recipe, ok := RecipeByName(req.Recipe)
	if !ok {
		return Generation{}, invalid("recipe", "unknown recipe")
	}
	provs, err := s.stepProviders(recipe, req)
	if err != nil {
		return Generation{}, err
	}

	ps, err := s.GetPhotoshoot(ctx, photoshootID)
	if err != nil {
		return Generation{}, err
	}
	prod, model, err := s.checkRefs(ctx, ps.ProductID, ps.ModelID)
	if err != nil {
		return Generation{}, err
	}
	if len(prod.Images) == 0 {
		return Generation{}, invalid("productId", "the product has no reference photographs yet")
	}
	if len(model.Images) == 0 {
		return Generation{}, invalid("modelId", "the model has no reference images yet")
	}
	presetID := req.PresetID
	if presetID == 0 {
		presetID = ps.PresetID
	}
	preset, err := s.resolvePreset(ctx, presetID, "presetId")
	if err != nil {
		return Generation{}, err
	}
	// Processed versions replace their originals.
	images := prod.GenerationImages()
	if req.GarmentImageID != 0 {
		var chosen *catalog.ProductImage
		for i := range prod.Images {
			if prod.Images[i].ID == req.GarmentImageID {
				chosen = &prod.Images[i]
			}
		}
		if chosen == nil {
			return Generation{}, invalid("garmentImageId", "not a photograph of this product")
		}
		// Put the chosen photograph first, keeping the others' order. An
		// original chosen explicitly is sent instead of its processed version.
		rest := []catalog.ProductImage{*chosen}
		for _, im := range images {
			if im.ID != chosen.ID && (chosen.ProcessedImageID == nil || im.ID != *chosen.ProcessedImageID) {
				rest = append(rest, im)
			}
		}
		images = rest
	}
	prod.Images = images

	snap := s.plan(recipe, provs, ps, preset, prod, model, req.Variations)
	settings, err := json.Marshal(snap)
	if err != nil {
		return Generation{}, err
	}
	var prompts []string
	for _, st := range snap.Steps {
		if st.Prompt != "" {
			prompts = append(prompts, st.Prompt)
		}
	}
	prompt := strings.Join(prompts, "\n\n")

	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return Generation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := s.timestamp()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO generation (photoshoot_id, product_id, model_id, recipe, provider, variations, settings, prompt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ps.ID, prod.ID, model.ID, recipe.Name, snap.Provider, req.Variations, string(settings), prompt, now)
	if err != nil {
		return Generation{}, err
	}
	id, _ := res.LastInsertId()

	for _, in := range snap.Inputs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO generation_input (generation_id, position, role, label, blob_hash) VALUES (?, ?, ?, ?, ?)`,
			id, in.Position, in.Role, in.Label, in.Hash); err != nil {
			return Generation{}, err
		}
	}
	for v := 1; v <= req.Variations; v++ {
		for _, st := range snap.Steps {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO generation_step (generation_id, variation, position, capability, provider, provider_model, request)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, v, st.Position, st.Capability, st.Provider, st.ProviderModel, stepRequestJSON(id, v, st)); err != nil {
				return Generation{}, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE photoshoot SET updated_at = ? WHERE id = ?`, now, ps.ID); err != nil {
		return Generation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Generation{}, err
	}

	s.log.InfoContext(ctx, "generation queued", "generation", id, "recipe", recipe.Name, "provider", snap.Provider, "preset", preset.Name, "variations", req.Variations)
	if s.runner != nil {
		s.runner.Notify()
	}
	return s.GetGeneration(ctx, id)
}

// stepProviders chooses the provider of each recipe step: the one assigned
// to the step's capability, else the request's default provider.
func (s *Service) stepProviders(recipe Recipe, req GenerationRequest) ([]provider.Provider, error) {
	known := map[string]bool{}
	for _, c := range recipe.Capabilities() {
		known[string(c)] = true
	}
	for c := range req.Providers {
		if !known[c] {
			return nil, invalid("providers", fmt.Sprintf("the %s recipe has no %s step", recipe.Label, c))
		}
	}
	out := make([]provider.Provider, 0, len(recipe.Steps))
	for _, st := range recipe.Steps {
		name, field := req.Provider, "provider"
		if n := req.Providers[string(st.Capability)]; n != "" {
			name, field = n, "providers."+string(st.Capability)
		}
		if name == "" {
			return nil, invalid(field, fmt.Sprintf("choose a provider for the %s step", st.Purpose))
		}
		p, err := s.providers.Get(name)
		if err != nil {
			return nil, invalid(field, fmt.Sprintf("%s is not configured", name))
		}
		if !provider.Supports(p, st.Capability) {
			return nil, invalid(field, fmt.Sprintf("%s cannot run the %s step of the %s recipe", p.Label(), st.Purpose, recipe.Label))
		}
		out = append(out, p)
	}
	return out, nil
}

// plan builds the snapshot: inputs, and each step's provider, prompt and
// inputs. provs holds the provider of each recipe step.
func (s *Service) plan(recipe Recipe, provs []provider.Provider, ps Photoshoot, preset Preset, prod catalog.Product, model catalog.Model, variations int) Snapshot {
	snap := Snapshot{
		Recipe:             recipe.Name,
		Provider:           provs[len(provs)-1].Name(),
		Variations:         variations,
		AspectRatio:        ps.AspectRatio,
		CustomInstructions: ps.CustomInstructions,
		Photoshoot:         SnapshotRef{ID: ps.ID, Name: ps.Name},
		Product: SnapshotProduct{
			ID: prod.ID, Name: prod.Name, SKU: prod.SKU, Category: prod.Category,
			Colour: prod.Colour, PrintDetails: prod.PrintDetails,
		},
		Model: SnapshotModel{
			ID: model.ID, Name: model.Name, Appearance: model.Appearance,
			PreferredPoses: model.PreferredPoses, Styling: model.Styling,
		},
		Preset: SnapshotPreset{
			ID: preset.ID, Slug: preset.Slug, Name: preset.Name, BuiltIn: preset.BuiltIn,
			Environment: preset.Environment, Pose: preset.Pose, Framing: preset.Framing,
			Lighting: preset.Lighting, Style: preset.Style, PromptFragment: preset.PromptFragment,
		},
	}

	var garments, models []int
	for i, im := range prod.Images {
		if i == MaxGarments {
			break
		}
		pos := len(snap.Inputs) + 1
		snap.Inputs = append(snap.Inputs, GenerationInput{Position: pos, Role: "garment", Label: im.View, Hash: im.Hash, Processed: !im.Original()})
		garments = append(garments, pos)
	}
	for i, im := range model.Images {
		if i == MaxModelRefs {
			break
		}
		pos := len(snap.Inputs) + 1
		snap.Inputs = append(snap.Inputs, GenerationInput{Position: pos, Role: "model", Label: im.Role, Hash: im.Hash})
		models = append(models, pos)
	}

	pin := PromptInput{Product: prod, Model: model, Preset: preset, AspectRatio: ps.AspectRatio, CustomInstructions: ps.CustomInstructions}
	for i, spec := range recipe.Steps {
		var inputs []int
		if spec.UsesGarments {
			g := garments
			if spec.GarmentLimit > 0 && len(g) > spec.GarmentLimit {
				g = g[:spec.GarmentLimit]
			}
			inputs = append(inputs, g...)
		}
		if spec.UsesModel {
			inputs = append(inputs, models...)
		}
		source := 0
		if spec.SourceFromModel {
			source = models[0]
			for _, pos := range models {
				if snap.Inputs[pos-1].Label == "full_body" {
					source = pos
					break
				}
			}
		}
		snap.Steps = append(snap.Steps, SnapshotStep{
			Source:        source,
			Position:      i + 1,
			Capability:    string(spec.Capability),
			Purpose:       spec.Purpose,
			Provider:      provs[i].Name(),
			ProviderModel: provs[i].Model(spec.Capability),
			Prompt:        spec.Prompt(pin),
			Inputs:        inputs,
			UsesPrevious:  spec.UsesPrevious,
		})
	}
	return snap
}

// stepRequest is what is stored per step: enough to rebuild the provider
// request after a restart.
type stepRequest struct {
	Prompt       string `json:"prompt"`
	Inputs       []int  `json:"inputs"`
	UsesPrevious bool   `json:"usesPrevious"`
	Source       int    `json:"source"`
	Seed         int64  `json:"seed"`
}

func stepRequestJSON(generationID int64, variation int, st SnapshotStep) string {
	b, _ := json.Marshal(stepRequest{
		Prompt:       st.Prompt,
		Inputs:       st.Inputs,
		UsesPrevious: st.UsesPrevious,
		Source:       st.Source,
		Seed:         seed(generationID, variation),
	})
	return string(b)
}

// seed is deterministic per generation and variation, so a resumed step
// asks for the same image.
func seed(generationID int64, variation int) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d/%d", generationID, variation)
	return int64(h.Sum64() & 0x7fffffffffffffff) //nolint:gosec // masked to 63 bits
}

const generationCols = `g.id, g.photoshoot_id, g.product_id, g.model_id, g.recipe, g.provider, g.variations, g.status,
	g.error, g.cost_micros, g.cancel_requested, g.prompt, g.settings, g.created_at, g.started_at, g.finished_at,
	(SELECT count(*) FROM generation_step st WHERE st.generation_id = g.id),
	(SELECT count(*) FROM generation_step st WHERE st.generation_id = g.id AND st.status IN ('succeeded', 'failed', 'cancelled'))`

func scanGeneration(row scanner) (Generation, error) {
	var g Generation
	var settings, created string
	var started, finished *string
	err := row.Scan(&g.ID, &g.PhotoshootID, &g.ProductID, &g.ModelID, &g.Recipe, &g.Provider, &g.Variations, &g.Status,
		&g.Error, &g.CostMicros, &g.CancelRequested, &g.Prompt, &settings, &created, &started, &finished,
		&g.StepsTotal, &g.StepsDone)
	if err != nil {
		return g, err
	}
	g.CreatedAt, g.StartedAt, g.FinishedAt = parseTime(created), parseTimePtr(started), parseTimePtr(finished)
	if err := json.Unmarshal([]byte(settings), &g.Settings); err != nil {
		return g, fmt.Errorf("generation %d settings: %w", g.ID, err)
	}
	g.Images = []GeneratedImage{}
	return g, nil
}

func (s *Service) listGenerations(ctx context.Context, where string, args ...any) ([]Generation, error) {
	rows, err := s.db.Read.QueryContext(ctx, `SELECT `+generationCols+` FROM generation g `+where+` ORDER BY g.id DESC LIMIT 100`, args...) //nolint:gosec // where is a constant
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Generation{}
	byID := map[int64]int{}
	for rows.Next() {
		g, err := scanGeneration(rows)
		if err != nil {
			return nil, err
		}
		byID[g.ID] = len(out)
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	images, err := s.ListImages(ctx, ImageFilter{GenerationIDs: keys(byID)})
	if err != nil {
		return nil, err
	}
	for _, im := range images {
		g := &out[byID[im.GenerationID]]
		g.Images = append(g.Images, im)
	}
	return out, nil
}

func keys(m map[int64]int) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// GetGeneration returns a generation with its steps and images.
func (s *Service) GetGeneration(ctx context.Context, id int64) (Generation, error) {
	gens, err := s.listGenerations(ctx, `WHERE g.id = ?`, id)
	if err != nil {
		return Generation{}, err
	}
	if len(gens) == 0 {
		return Generation{}, ErrNotFound
	}
	g := gens[0]
	g.Steps, err = s.steps(ctx, id)
	return g, err
}

func (s *Service) steps(ctx context.Context, generationID int64) ([]Step, error) {
	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT id, variation, position, capability, provider, provider_model, provider_job_id, status, attempts,
			error, cost_micros, COALESCE(output_blob_hash, ''), started_at, finished_at
		FROM generation_step WHERE generation_id = ? ORDER BY variation, position`, generationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Step{}
	for rows.Next() {
		var st Step
		var started, finished *string
		if err := rows.Scan(&st.ID, &st.Variation, &st.Position, &st.Capability, &st.Provider, &st.ProviderModel,
			&st.ProviderJobID, &st.Status, &st.Attempts, &st.Error, &st.CostMicros, &st.OutputHash, &started, &finished); err != nil {
			return nil, err
		}
		st.StartedAt, st.FinishedAt = parseTimePtr(started), parseTimePtr(finished)
		out = append(out, st)
	}
	return out, rows.Err()
}

// CancelGeneration stops a queued or running generation. Images already
// produced are kept.
func (s *Service) CancelGeneration(ctx context.Context, id int64) (Generation, error) {
	if err := s.runner.Cancel(ctx, id); err != nil {
		return Generation{}, err
	}
	return s.GetGeneration(ctx, id)
}

// ImageFilter selects generated images.
type ImageFilter struct {
	ProductID     int64
	PhotoshootID  int64
	GenerationIDs []int64
	ReviewStatus  string
}

// ReviewStatuses an image can have.
var ReviewStatuses = []string{"pending", "approved", "rejected", "needs_refinement"}

const imageSelect = `SELECT gi.id, gi.generation_id, gi.variation, gi.blob_hash, b.width, b.height, gi.review_status,
	COALESCE(gi.rejection_reason, ''), gi.reviewer_notes, gi.reviewed_at, gi.created_at, g.recipe, g.provider, g.photoshoot_id, p.id, p.name, COALESCE(p.sku, ''), m.id, m.name, m.archived_at IS NOT NULL
	FROM generated_image gi
	JOIN generation g ON g.id = gi.generation_id
	JOIN blob b ON b.hash = gi.blob_hash
	JOIN product p ON p.id = g.product_id
	JOIN model m ON m.id = g.model_id`

func scanGeneratedImage(row scanner) (GeneratedImage, error) {
	var im GeneratedImage
	var created string
	var reviewed *string
	err := row.Scan(&im.ID, &im.GenerationID, &im.Variation, &im.Hash, &im.Width, &im.Height, &im.ReviewStatus,
		&im.RejectionReason, &im.ReviewerNotes, &reviewed, &created, &im.Recipe, &im.Provider, &im.PhotoshootID,
		&im.Product.ID, &im.Product.Name, &im.Product.SKU, &im.Model.ID, &im.Model.Name, &im.Model.Archived)
	im.CreatedAt, im.ReviewedAt = parseTime(created), parseTimePtr(reviewed)
	return im, err
}

// ListImages returns generated images, newest first.
func (s *Service) ListImages(ctx context.Context, f ImageFilter) ([]GeneratedImage, error) {
	if f.ReviewStatus != "" && !contains(ReviewStatuses, f.ReviewStatus) {
		return nil, invalid("status", "must be one of "+strings.Join(ReviewStatuses, ", "))
	}
	var where []string
	var args []any
	if f.ProductID > 0 {
		where, args = append(where, "g.product_id = ?"), append(args, f.ProductID)
	}
	if f.PhotoshootID > 0 {
		where, args = append(where, "g.photoshoot_id = ?"), append(args, f.PhotoshootID)
	}
	if len(f.GenerationIDs) > 0 {
		where = append(where, "g.id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.GenerationIDs)), ",")+")")
		for _, id := range f.GenerationIDs {
			args = append(args, id)
		}
	}
	if f.ReviewStatus != "" {
		where, args = append(where, "gi.review_status = ?"), append(args, f.ReviewStatus)
	}
	q := imageSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := s.db.Read.QueryContext(ctx, q+" ORDER BY gi.id DESC LIMIT 500", args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []GeneratedImage{}
	for rows.Next() {
		im, err := scanGeneratedImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, rows.Err()
}

// ImageDetail is a generated image with its full provenance.
type ImageDetail struct {
	GeneratedImage
	Prompt   string   `json:"prompt"`
	Settings Snapshot `json:"settings"`
	Steps    []Step   `json:"steps"`
	// CostMicros is the cost of the steps that produced this image.
	CostMicros int64 `json:"costMicros"`
	// ReviewHistory lists every decision, oldest first.
	ReviewHistory []ReviewEvent `json:"reviewHistory"`
}

// GetImage returns one generated image with the snapshot that produced it.
func (s *Service) GetImage(ctx context.Context, id int64) (ImageDetail, error) {
	im, err := scanGeneratedImage(s.db.Read.QueryRowContext(ctx, imageSelect+` WHERE gi.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ImageDetail{}, ErrNotFound
	}
	if err != nil {
		return ImageDetail{}, err
	}
	g, err := s.GetGeneration(ctx, im.GenerationID)
	if err != nil {
		return ImageDetail{}, err
	}
	d := ImageDetail{GeneratedImage: im, Prompt: g.Prompt, Settings: g.Settings, Steps: []Step{}}
	if d.ReviewHistory, err = s.reviewHistory(ctx, id); err != nil {
		return ImageDetail{}, err
	}
	for _, st := range g.Steps {
		if st.Variation == im.Variation {
			d.Steps = append(d.Steps, st)
			d.CostMicros += st.CostMicros
		}
	}
	return d, nil
}
