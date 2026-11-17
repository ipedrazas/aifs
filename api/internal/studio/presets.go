package studio

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DefaultPresetSlug is the built-in preset used when none is chosen.
const DefaultPresetSlug = "studio"

// Preset is the scene a photograph is taken in. It never describes the
// garment: the reference photographs do.
type Preset struct {
	ID             int64     `json:"id"`
	Slug           string    `json:"slug"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Environment    string    `json:"environment"`
	Pose           string    `json:"pose"`
	Framing        string    `json:"framing"`
	Lighting       string    `json:"lighting"`
	Style          string    `json:"style"`
	PromptFragment string    `json:"promptFragment"`
	BuiltIn        bool      `json:"builtIn"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

const presetCols = `id, COALESCE(slug, ''), name, description, environment, pose, framing, lighting, style,
	prompt_fragment, built_in, created_at, updated_at`

func scanPreset(row scanner) (Preset, error) {
	var p Preset
	var created, updated string
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.Description, &p.Environment, &p.Pose, &p.Framing, &p.Lighting,
		&p.Style, &p.PromptFragment, &p.BuiltIn, &created, &updated)
	p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
	return p, err
}

// ListPresets returns built-in presets first, then custom ones by name.
func (s *Service) ListPresets(ctx context.Context) ([]Preset, error) {
	rows, err := s.db.Read.QueryContext(ctx, `SELECT `+presetCols+` FROM preset ORDER BY built_in DESC, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Preset{}
	for rows.Next() {
		p, err := scanPreset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPreset returns one preset.
func (s *Service) GetPreset(ctx context.Context, id int64) (Preset, error) {
	p, err := scanPreset(s.db.Read.QueryRowContext(ctx, `SELECT `+presetCols+` FROM preset WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Preset{}, ErrNotFound
	}
	return p, err
}

// defaultPreset returns the built-in studio preset.
func (s *Service) defaultPreset(ctx context.Context) (Preset, error) {
	return scanPreset(s.db.Read.QueryRowContext(ctx, `SELECT `+presetCols+` FROM preset WHERE slug = ?`, DefaultPresetSlug))
}

// resolvePreset returns preset id, or the default when id is 0. field names
// the request field for validation errors.
func (s *Service) resolvePreset(ctx context.Context, id int64, field string) (Preset, error) {
	if id == 0 {
		return s.defaultPreset(ctx)
	}
	p, err := s.GetPreset(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Preset{}, invalid(field, "no such preset")
	}
	return p, err
}
