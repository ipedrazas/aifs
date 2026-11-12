package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ipedrazas/aifs/api/internal/imaging"
)

// Roles a model reference image can have.
var Roles = []string{"identity", "full_body"}

// Model origins.
const (
	OriginUploaded  = "uploaded"
	OriginGenerated = "generated"
)

// Model is a virtual model with its reference images.
type Model struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Appearance     string     `json:"appearance"`
	PreferredPoses string     `json:"preferredPoses"`
	Styling        string     `json:"styling"`
	Origin         string     `json:"origin"`
	Archived       bool       `json:"archived"`
	ArchivedAt     *time.Time `json:"archivedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	ImageCount     int        `json:"imageCount"`
	// Cover is the first identity image, else the first image.
	Cover *ModelImage `json:"cover"`
	// Images is only filled by GetModel.
	Images []ModelImage `json:"images,omitempty"`
}

// ModelImage is a reference image of a model.
type ModelImage struct {
	ID          int64     `json:"id"`
	ModelID     int64     `json:"modelId"`
	Hash        string    `json:"hash"`
	Role        string    `json:"role"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ModelInput holds the editable model fields. On update, nil fields are left
// unchanged. Archived only applies to updates (to restore or archive).
type ModelInput struct {
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	Appearance     *string `json:"appearance"`
	PreferredPoses *string `json:"preferredPoses"`
	Styling        *string `json:"styling"`
	Archived       *bool   `json:"archived"`
}

func (in *ModelInput) normalise(create bool) error {
	return normaliseFields([]field{
		{"name", in.Name, maxName},
		{"description", in.Description, maxText},
		{"appearance", in.Appearance, maxText},
		{"preferredPoses", in.PreferredPoses, maxText},
		{"styling", in.Styling, maxText},
	}, in.Name, create)
}

const modelCols = `m.id, m.name, m.description, m.appearance, m.preferred_poses, m.styling, m.origin,
	m.archived_at, m.created_at, m.updated_at, (SELECT count(*) FROM model_image WHERE model_id = m.id)`

const modelImageCols = `i.id, i.model_id, i.blob_hash, i.role, i.filename, b.content_type, b.size, b.width, b.height, i.created_at`

const roleOrder = `CASE role WHEN 'identity' THEN 0 ELSE 1 END`

func scanModel(row scanner) (Model, error) {
	var m Model
	var archived sql.NullString
	var created, updated string
	err := row.Scan(&m.ID, &m.Name, &m.Description, &m.Appearance, &m.PreferredPoses, &m.Styling, &m.Origin,
		&archived, &created, &updated, &m.ImageCount)
	m.CreatedAt, m.UpdatedAt = parseTime(created), parseTime(updated)
	if archived.Valid {
		t := parseTime(archived.String)
		m.ArchivedAt, m.Archived = &t, true
	}
	return m, err
}

func scanModelImage(row scanner) (ModelImage, error) {
	var im ModelImage
	var created string
	err := row.Scan(&im.ID, &im.ModelID, &im.Hash, &im.Role, &im.Filename, &im.ContentType, &im.Size, &im.Width, &im.Height, &created)
	im.CreatedAt = parseTime(created)
	return im, err
}

// CreateModel adds a model with no images.
func (s *Service) CreateModel(ctx context.Context, in ModelInput) (Model, error) {
	if err := in.normalise(true); err != nil {
		return Model{}, err
	}
	if in.Archived != nil && *in.Archived {
		return Model{}, &ValidationError{Fields: map[string]string{"archived": "a new model cannot be archived"}}
	}
	now := s.timestamp()
	res, err := s.db.Write.ExecContext(ctx, `
		INSERT INTO model (name, description, appearance, preferred_poses, styling, origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		*in.Name, str(in.Description), str(in.Appearance), str(in.PreferredPoses), str(in.Styling), OriginUploaded, now, now)
	if err != nil {
		return Model{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetModel(ctx, id)
}

// UpdateModel changes the fields set in in, including archiving or
// restoring the model.
func (s *Service) UpdateModel(ctx context.Context, id int64, in ModelInput) (Model, error) {
	if err := in.normalise(false); err != nil {
		return Model{}, err
	}
	now := s.timestamp()
	sets := []string{}
	args := []any{}
	add := func(col string, v any) { sets = append(sets, col+" = ?"); args = append(args, v) }
	if in.Name != nil {
		add("name", *in.Name)
	}
	if in.Description != nil {
		add("description", *in.Description)
	}
	if in.Appearance != nil {
		add("appearance", *in.Appearance)
	}
	if in.PreferredPoses != nil {
		add("preferred_poses", *in.PreferredPoses)
	}
	if in.Styling != nil {
		add("styling", *in.Styling)
	}
	if in.Archived != nil {
		if *in.Archived {
			sets = append(sets, "archived_at = COALESCE(archived_at, ?)")
			args = append(args, now)
		} else {
			sets = append(sets, "archived_at = NULL")
		}
	}
	add("updated_at", now)
	args = append(args, id)

	res, err := s.db.Write.ExecContext(ctx, `UPDATE model SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...) //nolint:gosec // column names are constants
	if err != nil {
		return Model{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Model{}, ErrNotFound
	}
	return s.GetModel(ctx, id)
}

// ListModels returns models, newest first. Archived models are included only
// when includeArchived is set.
func (s *Service) ListModels(ctx context.Context, includeArchived bool) ([]Model, error) {
	q := `SELECT ` + modelCols + ` FROM model m`
	if !includeArchived {
		q += ` WHERE m.archived_at IS NULL`
	}
	rows, err := s.db.Read.QueryContext(ctx, q+` ORDER BY m.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Model{}
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	covers, err := s.modelCovers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if c, ok := covers[out[i].ID]; ok {
			out[i].Cover = &c
		}
	}
	return out, nil
}

func (s *Service) modelCovers(ctx context.Context) (map[int64]ModelImage, error) {
	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT `+modelImageCols+` FROM model_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.id = (
			SELECT id FROM model_image WHERE model_id = i.model_id
			ORDER BY `+roleOrder+`, id LIMIT 1
		)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]ModelImage{}
	for rows.Next() {
		im, err := scanModelImage(rows)
		if err != nil {
			return nil, err
		}
		out[im.ModelID] = im
	}
	return out, rows.Err()
}

// GetModel returns a model (archived or not) with all its images.
func (s *Service) GetModel(ctx context.Context, id int64) (Model, error) {
	m, err := scanModel(s.db.Read.QueryRowContext(ctx, `SELECT `+modelCols+` FROM model m WHERE m.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Model{}, ErrNotFound
	}
	if err != nil {
		return Model{}, err
	}

	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT `+modelImageCols+` FROM model_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.model_id = ? ORDER BY `+roleOrder+`, i.id`, id)
	if err != nil {
		return Model{}, err
	}
	defer func() { _ = rows.Close() }()
	m.Images = []ModelImage{}
	for rows.Next() {
		im, err := scanModelImage(rows)
		if err != nil {
			return Model{}, err
		}
		m.Images = append(m.Images, im)
	}
	if err := rows.Err(); err != nil {
		return Model{}, err
	}
	if len(m.Images) > 0 {
		c := m.Images[0]
		m.Cover = &c
	}
	return m, nil
}

// DeleteModel hard-deletes a model that nothing refers to. A model that is
// referenced (by a generation or photoshoot) is archived instead, so the
// history that used it stays intact. It reports whether the model was
// deleted (true) or archived (false).
func (s *Service) DeleteModel(ctx context.Context, id int64) (deleted bool, err error) {
	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	hashes, err := s.imageHashes(ctx, `SELECT blob_hash FROM model_image WHERE model_id = ?`, id)
	if err != nil {
		return false, err
	}
	res, err := s.db.Write.ExecContext(ctx, `DELETE FROM model WHERE id = ?`, id)
	if isReferenced(err) {
		now := s.timestamp()
		if _, err := s.db.Write.ExecContext(ctx,
			`UPDATE model SET archived_at = COALESCE(archived_at, ?), updated_at = ? WHERE id = ?`,
			now, now, id); err != nil {
			return false, err
		}
		s.log.InfoContext(ctx, "model is in use; archived instead of deleted", "model", id)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, ErrNotFound
	}
	s.collect(ctx, hashes)
	return true, nil
}

// AddModelImage validates and stores a reference image of a model, unchanged.
func (s *Service) AddModelImage(ctx context.Context, modelID int64, role, filename string, data []byte) (ModelImage, error) {
	if !validRole(role) {
		return ModelImage{}, &ValidationError{Fields: map[string]string{"role": "must be one of " + strings.Join(Roles, ", ")}}
	}
	info, err := imaging.Validate(data)
	if err != nil {
		return ModelImage{}, err
	}
	filename = cleanFilename(filename)

	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	var exists int
	if err := s.db.Read.QueryRowContext(ctx, `SELECT count(*) FROM model WHERE id = ?`, modelID).Scan(&exists); err != nil {
		return ModelImage{}, err
	}
	if exists == 0 {
		return ModelImage{}, ErrNotFound
	}

	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return ModelImage{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := s.timestamp()
	hash, err := s.storeOriginal(ctx, tx, data, info, now)
	if err != nil {
		return ModelImage{}, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO model_image (model_id, blob_hash, role, filename, created_at) VALUES (?, ?, ?, ?, ?)`,
		modelID, hash, role, filename, now)
	if isUnique(err) {
		return ModelImage{}, &ConflictError{Reason: "this image is already attached to the model"}
	}
	if err != nil {
		return ModelImage{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model SET updated_at = ? WHERE id = ?`, now, modelID); err != nil {
		return ModelImage{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModelImage{}, err
	}
	id, _ := res.LastInsertId()
	return s.modelImage(ctx, modelID, id)
}

func (s *Service) modelImage(ctx context.Context, modelID, imageID int64) (ModelImage, error) {
	im, err := scanModelImage(s.db.Read.QueryRowContext(ctx, `
		SELECT `+modelImageCols+` FROM model_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.id = ? AND i.model_id = ?`, imageID, modelID))
	if errors.Is(err, sql.ErrNoRows) {
		return ModelImage{}, ErrNotFound
	}
	return im, err
}

// DeleteModelImage removes one reference image record (and its bytes, if no
// other record uses them).
func (s *Service) DeleteModelImage(ctx context.Context, modelID, imageID int64) error {
	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	im, err := s.modelImage(ctx, modelID, imageID)
	if err != nil {
		return err
	}
	if _, err := s.db.Write.ExecContext(ctx, `DELETE FROM model_image WHERE id = ?`, imageID); err != nil {
		return err
	}
	if _, err := s.db.Write.ExecContext(ctx, `UPDATE model SET updated_at = ? WHERE id = ?`, s.timestamp(), modelID); err != nil {
		return err
	}
	s.collect(ctx, []string{im.Hash})
	return nil
}

func validRole(r string) bool {
	for _, x := range Roles {
		if r == x {
			return true
		}
	}
	return false
}
