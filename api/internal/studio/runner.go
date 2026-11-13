package studio

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/imaging"
	"github.com/ipedrazas/aifs/api/internal/provider"
)

// RunnerConfig tunes the background runner.
type RunnerConfig struct {
	// Workers is how many generations run at once.
	Workers int
	// ProviderConcurrency is how many jobs may be in flight per provider.
	ProviderConcurrency int
	// PollInterval is the delay between status checks of a provider job.
	PollInterval time.Duration
	// ScanInterval is how often the queue is checked without a notification.
	ScanInterval time.Duration
	// StepTimeout bounds one provider call, from first submission.
	StepTimeout time.Duration
	// MaxAttempts bounds submissions of one step (transient errors, lost jobs).
	MaxAttempts int
	// RetryBase is the first backoff delay; it doubles per attempt.
	RetryBase time.Duration
}

func (c *RunnerConfig) defaults() {
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.ProviderConcurrency <= 0 {
		c.ProviderConcurrency = 4
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.ScanInterval <= 0 {
		c.ScanInterval = 5 * time.Second
	}
	if c.StepTimeout <= 0 {
		c.StepTimeout = 10 * time.Minute
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.RetryBase <= 0 {
		c.RetryBase = time.Second
	}
}

// errUserCancelled is the cancellation cause when the user cancels, as
// opposed to the server shutting down (which must leave work resumable).
var errUserCancelled = errors.New("cancelled by the user")

// Runner executes queued generations. The generation table is the queue:
// work survives restarts because every state change is written before it is
// acted on, steps are resumed from their stored provider job id, and at most
// one image per variation can be recorded.
type Runner struct {
	db        *db.DB
	blobs     blob.Store
	providers *provider.Registry
	log       *slog.Logger
	cfg       RunnerConfig
	now       func() time.Time

	wake chan struct{}

	mu     sync.Mutex
	active map[int64]context.CancelCauseFunc
	sems   map[string]chan struct{}
	wg     sync.WaitGroup
}

// NewRunner returns a runner; call Run to start it.
func NewRunner(database *db.DB, blobs blob.Store, providers *provider.Registry, log *slog.Logger, cfg RunnerConfig) *Runner {
	cfg.defaults()
	return &Runner{
		db: database, blobs: blobs, providers: providers, log: log, cfg: cfg, now: time.Now,
		wake:   make(chan struct{}, 1),
		active: map[int64]context.CancelCauseFunc{},
		sems:   map[string]chan struct{}{},
	}
}

func (r *Runner) timestamp() string { return r.now().UTC().Format(time.RFC3339Nano) }

// Notify wakes the runner after new work is queued.
func (r *Runner) Notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run processes the queue until ctx is done, then waits for in-flight work
// to stop. Interrupted generations stay "running" and are resumed by the
// next Run.
func (r *Runner) Run(ctx context.Context) {
	r.log.InfoContext(ctx, "generation runner started", "workers", r.cfg.Workers)
	t := time.NewTicker(r.cfg.ScanInterval)
	defer t.Stop()
	for {
		r.dispatch(ctx)
		select {
		case <-ctx.Done():
			r.wg.Wait()
			r.log.Info("generation runner stopped")
			return
		case <-r.wake:
		case <-t.C:
		}
	}
}

// dispatch starts queued generations and adopts running ones left by a
// previous process, up to the worker limit.
func (r *Runner) dispatch(ctx context.Context) {
	cands, err := r.candidates(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.log.ErrorContext(ctx, "runner: scan queue", "err", err)
		}
		return
	}

	for _, c := range cands {
		if ctx.Err() != nil {
			return
		}
		r.mu.Lock()
		if _, busy := r.active[c.id]; busy || len(r.active) >= r.cfg.Workers {
			r.mu.Unlock()
			continue
		}
		if c.status == StatusQueued {
			res, err := r.db.Write.ExecContext(ctx,
				`UPDATE generation SET status = 'running', started_at = COALESCE(started_at, ?) WHERE id = ? AND status = 'queued'`,
				r.timestamp(), c.id)
			if err != nil {
				r.mu.Unlock()
				r.log.ErrorContext(ctx, "runner: claim", "generation", c.id, "err", err)
				continue
			}
			if n, _ := res.RowsAffected(); n == 0 {
				r.mu.Unlock()
				continue // cancelled meanwhile
			}
		} else {
			r.log.InfoContext(ctx, "runner: resuming interrupted generation", "generation", c.id)
		}
		gctx, cancel := context.WithCancelCause(ctx)
		r.active[c.id] = cancel
		r.mu.Unlock()

		r.wg.Add(1)
		go func(id int64) {
			defer r.wg.Done()
			defer func() {
				r.mu.Lock()
				delete(r.active, id)
				r.mu.Unlock()
				cancel(nil)
				r.Notify() // a worker slot is free
			}()
			r.process(gctx, id)
		}(c.id)
	}
}

type candidate struct {
	id     int64
	status string
}

func (r *Runner) candidates(ctx context.Context) ([]candidate, error) {
	rows, err := r.db.Read.QueryContext(ctx,
		`SELECT id, status FROM generation WHERE status IN ('queued', 'running') ORDER BY id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Cancel stops a generation. A queued one is cancelled at once; a running
// one is interrupted and finalised by its worker.
func (r *Runner) Cancel(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.timestamp()
	res, err := r.db.Write.ExecContext(ctx, `
		UPDATE generation SET status = 'cancelled', cancel_requested = 1, finished_at = ?
		WHERE id = ? AND status = 'queued'`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		_, err := r.db.Write.ExecContext(ctx, `
			UPDATE generation_step SET status = 'cancelled', finished_at = ?
			WHERE generation_id = ? AND status IN ('pending', 'submitted')`, now, id)
		return err
	}

	res, err = r.db.Write.ExecContext(ctx, `UPDATE generation SET cancel_requested = 1 WHERE id = ? AND status = 'running'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if cancel, ok := r.active[id]; ok {
			cancel(errUserCancelled)
		}
		r.Notify() // not active (between processes): dispatch finalises it
		return nil
	}

	var status string
	err = r.db.Read.QueryRowContext(ctx, `SELECT status FROM generation WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return &ConflictError{Reason: "the generation has already " + status}
}

// --- processing ------------------------------------------------------------

type runStep struct {
	id         int64
	variation  int
	position   int
	capability provider.Capability
	provider   string
	request    stepRequest
	jobID      string
	status     string
	attempts   int
	outputHash string
	startedAt  *time.Time
}

type runGeneration struct {
	id          int64
	aspectRatio string
	category    string
	inputs      map[int]GenerationInput
	steps       map[int][]*runStep // by variation, ordered by position
	cancelReq   bool
}

func (r *Runner) load(ctx context.Context, id int64) (*runGeneration, error) {
	g := &runGeneration{id: id, inputs: map[int]GenerationInput{}, steps: map[int][]*runStep{}}
	var settings string
	if err := r.db.Read.QueryRowContext(ctx, `SELECT settings, cancel_requested FROM generation WHERE id = ?`, id).
		Scan(&settings, &g.cancelReq); err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(settings), &snap); err != nil {
		return nil, err
	}
	g.aspectRatio, g.category = snap.AspectRatio, snap.Product.Category
	for _, in := range snap.Inputs {
		g.inputs[in.Position] = in
	}

	rows, err := r.db.Read.QueryContext(ctx, `
		SELECT id, variation, position, capability, provider, request, provider_job_id, status, attempts,
			COALESCE(output_blob_hash, ''), started_at
		FROM generation_step WHERE generation_id = ? ORDER BY variation, position`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		st := &runStep{}
		var req string
		var started *string
		if err := rows.Scan(&st.id, &st.variation, &st.position, &st.capability, &st.provider, &req, &st.jobID,
			&st.status, &st.attempts, &st.outputHash, &started); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(req), &st.request); err != nil {
			return nil, fmt.Errorf("step %d request: %w", st.id, err)
		}
		st.startedAt = parseTimePtr(started)
		g.steps[st.variation] = append(g.steps[st.variation], st)
	}
	return g, rows.Err()
}

func (r *Runner) process(ctx context.Context, id int64) {
	log := r.log.With("generation", id)
	g, err := r.load(ctx, id)
	if err != nil {
		if ctx.Err() == nil {
			log.ErrorContext(ctx, "runner: load generation", "err", err)
			r.finish(context.WithoutCancel(ctx), id, StatusFailed, "could not load the generation: "+err.Error())
		}
		return
	}
	if g.cancelReq {
		r.finishCancelled(context.WithoutCancel(ctx), id)
		return
	}

	var wg sync.WaitGroup
	for v := range g.steps {
		wg.Go(func() { r.runVariation(ctx, g, v, log) })
	}
	wg.Wait()

	switch {
	case errors.Is(context.Cause(ctx), errUserCancelled):
		r.finishCancelled(context.WithoutCancel(ctx), id)
	case ctx.Err() != nil:
		log.Info("runner: generation interrupted by shutdown; it will resume")
	default:
		r.finalise(ctx, id)
	}
}

// runVariation runs the steps of one variation in order.
func (r *Runner) runVariation(ctx context.Context, g *runGeneration, variation int, log *slog.Logger) {
	var prev string
	for _, st := range g.steps[variation] {
		switch st.status {
		case StepSucceeded:
			prev = st.outputHash
			continue
		case StepFailed, StepCancelled:
			return
		}
		if ctx.Err() != nil {
			return
		}
		out, err := r.runStep(ctx, g, st, prev)
		if err != nil {
			if ctx.Err() != nil {
				return // shutdown or cancel; handled by the caller
			}
			log.WarnContext(ctx, "runner: step failed", "variation", variation, "step", st.position, "err", err)
			r.failStep(context.WithoutCancel(ctx), st, err.Error())
			return
		}
		prev = out
	}
}

// runStep submits (or resumes) one provider call and records its output.
func (r *Runner) runStep(ctx context.Context, g *runGeneration, st *runStep, prev string) (string, error) {
	p, err := r.providers.Get(st.provider)
	if err != nil {
		return "", err
	}

	sem := r.semaphore(st.provider)
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	if st.startedAt == nil {
		now := r.now()
		st.startedAt = &now
		if _, err := r.db.Write.ExecContext(ctx, `UPDATE generation_step SET started_at = ? WHERE id = ?`,
			now.UTC().Format(time.RFC3339Nano), st.id); err != nil {
			return "", err
		}
	}
	deadline := st.startedAt.Add(r.cfg.StepTimeout)
	sctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	for {
		if st.status != StepSubmitted || st.jobID == "" {
			if err := r.submit(sctx, g, p, st, prev); err != nil {
				return "", r.timeoutError(ctx, sctx, err)
			}
		}
		res, err := r.poll(sctx, p, st)
		if errors.Is(err, provider.ErrUnknownJob) {
			// The provider lost the job (e.g. it restarted): submit again.
			r.log.WarnContext(ctx, "runner: provider lost the job; resubmitting", "step", st.id, "job", st.jobID)
			st.jobID, st.status = "", StepPending
			continue
		}
		if err != nil {
			return "", r.timeoutError(ctx, sctx, err)
		}
		if res.Status == provider.Failed {
			msg := res.Error
			if msg == "" {
				msg = "the provider reported a failure without details"
			}
			return "", errors.New(msg)
		}
		return r.complete(ctx, g, p, st, res)
	}
}

func (r *Runner) timeoutError(parent, step context.Context, err error) error {
	if parent.Err() == nil && errors.Is(step.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s", r.cfg.StepTimeout)
	}
	return err
}

func (r *Runner) semaphore(name string) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sems[name]
	if !ok {
		s = make(chan struct{}, r.cfg.ProviderConcurrency)
		r.sems[name] = s
	}
	return s
}

// submit sends the request, retrying transient errors with backoff, and
// stores the job id before anything else happens.
func (r *Runner) submit(ctx context.Context, g *runGeneration, p provider.Provider, st *runStep, prev string) error {
	req, err := r.buildRequest(ctx, g, st, prev)
	if err != nil {
		return err
	}
	for {
		if st.attempts >= r.cfg.MaxAttempts {
			return fmt.Errorf("gave up after %d attempts", st.attempts)
		}
		st.attempts++
		if _, err := r.db.Write.ExecContext(ctx, `UPDATE generation_step SET attempts = ? WHERE id = ?`, st.attempts, st.id); err != nil {
			return err
		}
		jobID, err := p.Submit(ctx, req)
		if err == nil {
			st.jobID, st.status = jobID, StepSubmitted
			_, err := r.db.Write.ExecContext(ctx,
				`UPDATE generation_step SET provider_job_id = ?, status = 'submitted' WHERE id = ?`, jobID, st.id)
			return err
		}
		transient, after := provider.IsTransient(err)
		if !transient || ctx.Err() != nil {
			return err
		}
		if st.attempts >= r.cfg.MaxAttempts {
			return fmt.Errorf("%w (gave up after %d attempts)", err, st.attempts)
		}
		wait := max(after, r.cfg.RetryBase<<(st.attempts-1))
		r.log.WarnContext(ctx, "runner: transient provider error; retrying", "step", st.id, "in", wait, "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// poll waits for a submitted job to finish. Transient poll errors are
// retried until the step deadline.
func (r *Runner) poll(ctx context.Context, p provider.Provider, st *runStep) (provider.Result, error) {
	for {
		res, err := p.Poll(ctx, st.jobID)
		if err == nil && res.Status != provider.Pending {
			return res, nil
		}
		if err != nil {
			if transient, _ := provider.IsTransient(err); !transient {
				return provider.Result{}, err
			}
			r.log.WarnContext(ctx, "runner: transient poll error", "step", st.id, "err", err)
		}
		select {
		case <-time.After(r.cfg.PollInterval):
		case <-ctx.Done():
			return provider.Result{}, ctx.Err()
		}
	}
}

// buildRequest rebuilds the provider request from stored hashes.
func (r *Runner) buildRequest(ctx context.Context, g *runGeneration, st *runStep, prev string) (provider.Request, error) {
	req := provider.Request{
		Capability:      st.capability,
		Prompt:          st.request.Prompt,
		AspectRatio:     g.aspectRatio,
		Seed:            st.request.Seed,
		GarmentCategory: g.category,
	}
	for _, pos := range st.request.Inputs {
		in, ok := g.inputs[pos]
		if !ok {
			return req, fmt.Errorf("input %d missing from the snapshot", pos)
		}
		im, err := r.readBlob(ctx, blob.Original, in.Hash)
		if err != nil {
			return req, fmt.Errorf("read %s reference: %w", in.Role, err)
		}
		im.Role, im.Label = in.Role, in.Label
		if in.Role == "garment" {
			req.Garments = append(req.Garments, im)
		} else {
			req.ModelRefs = append(req.ModelRefs, im)
		}
	}
	if pos := st.request.Source; pos > 0 {
		in, ok := g.inputs[pos]
		if !ok {
			return req, fmt.Errorf("source input %d missing from the snapshot", pos)
		}
		im, err := r.readBlob(ctx, blob.Original, in.Hash)
		if err != nil {
			return req, fmt.Errorf("read the model photograph: %w", err)
		}
		im.Role, im.Label = "source", in.Label
		req.Source = &im
	}
	if st.request.UsesPrevious {
		if prev == "" {
			return req, errors.New("the previous step produced no image")
		}
		im, err := r.readBlob(ctx, blob.Generated, prev)
		if err != nil {
			return req, fmt.Errorf("read previous step output: %w", err)
		}
		im.Role = "source"
		req.Source = &im
	}
	return req, nil
}

func (r *Runner) readBlob(ctx context.Context, ns blob.Namespace, hash string) (provider.Image, error) {
	o, err := r.blobs.Open(ctx, ns, hash)
	if err != nil {
		return provider.Image{}, err
	}
	defer func() { _ = o.Close() }()
	data, err := io.ReadAll(o)
	if err != nil {
		return provider.Image{}, err
	}
	info, err := imaging.Validate(data)
	if err != nil {
		return provider.Image{}, err
	}
	return provider.Image{Hash: hash, ContentType: info.ContentType, Data: data}, nil
}

// complete stores the output and marks the step succeeded. The last step of
// a variation also records the generated image, in the same transaction.
func (r *Runner) complete(ctx context.Context, g *runGeneration, p provider.Provider, st *runStep, res provider.Result) (string, error) {
	if len(res.Images) == 0 {
		return "", errors.New("the provider returned no image")
	}
	data := res.Images[0].Data
	info, err := imaging.Validate(data)
	if err != nil {
		return "", fmt.Errorf("the provider returned an unusable image: %w", err)
	}
	hash, size, err := r.blobs.Put(ctx, blob.Generated, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("store image: %w", err)
	}
	cost := r.providers.Cost(p.Name(), res)
	last := st.position == len(g.steps[st.variation])

	ctx = context.WithoutCancel(ctx) // the provider work is done; always record it
	tx, err := r.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	now := r.timestamp()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO blob (hash, namespace, content_type, size, width, height, orientation, created_at)
		VALUES (?, 'generated', ?, ?, ?, ?, ?, ?) ON CONFLICT (hash) DO NOTHING`,
		hash, info.ContentType, size, info.Width, info.Height, info.Orientation, now); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE generation_step SET status = 'succeeded', output_blob_hash = ?, cost_micros = ?, finished_at = ?, error = ''
		WHERE id = ?`, hash, cost, now, st.id); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE generation SET cost_micros = cost_micros + ? WHERE id = ?`, cost, g.id); err != nil {
		return "", err
	}
	if last {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO generated_image (generation_id, variation, blob_hash, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (generation_id, variation) DO NOTHING`, g.id, st.variation, hash, now); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	st.status, st.outputHash = StepSucceeded, hash
	return hash, nil
}

func (r *Runner) failStep(ctx context.Context, st *runStep, msg string) {
	if _, err := r.db.Write.ExecContext(ctx,
		`UPDATE generation_step SET status = 'failed', error = ?, finished_at = ? WHERE id = ?`,
		truncate(msg, 1000), r.timestamp(), st.id); err != nil {
		r.log.ErrorContext(ctx, "runner: record step failure", "step", st.id, "err", err)
	}
	st.status = StepFailed
}

// finalise sets the outcome once every variation has stopped: succeeded
// when every variation produced an image, otherwise failed with the reason.
func (r *Runner) finalise(ctx context.Context, id int64) {
	var variations, images int
	if err := r.db.Read.QueryRowContext(ctx, `
		SELECT g.variations, (SELECT count(*) FROM generated_image gi WHERE gi.generation_id = g.id)
		FROM generation g WHERE g.id = ?`, id).Scan(&variations, &images); err != nil {
		r.log.ErrorContext(ctx, "runner: finalise", "generation", id, "err", err)
		return
	}
	if images >= variations {
		r.finish(ctx, id, StatusSucceeded, "")
		return
	}
	var firstErr string
	_ = r.db.Read.QueryRowContext(ctx, `
		SELECT error FROM generation_step WHERE generation_id = ? AND status = 'failed'
		ORDER BY variation, position LIMIT 1`, id).Scan(&firstErr)
	if firstErr == "" {
		firstErr = "no image was produced"
	}
	msg := firstErr
	if variations > 1 {
		msg = fmt.Sprintf("%d of %d variations failed: %s", variations-images, variations, firstErr)
	}
	r.finish(ctx, id, StatusFailed, msg)
}

func (r *Runner) finish(ctx context.Context, id int64, status, msg string) {
	if _, err := r.db.Write.ExecContext(ctx,
		`UPDATE generation SET status = ?, error = ?, finished_at = ? WHERE id = ? AND status IN ('queued', 'running')`,
		status, truncate(msg, 2000), r.timestamp(), id); err != nil {
		r.log.ErrorContext(ctx, "runner: finish", "generation", id, "err", err)
		return
	}
	r.log.InfoContext(ctx, "generation finished", "generation", id, "status", status, "error", msg)
}

func (r *Runner) finishCancelled(ctx context.Context, id int64) {
	if _, err := r.db.Write.ExecContext(ctx, `
		UPDATE generation_step SET status = 'cancelled', finished_at = ?
		WHERE generation_id = ? AND status IN ('pending', 'submitted')`, r.timestamp(), id); err != nil {
		r.log.ErrorContext(ctx, "runner: cancel steps", "generation", id, "err", err)
	}
	r.finish(ctx, id, StatusCancelled, "")
}
