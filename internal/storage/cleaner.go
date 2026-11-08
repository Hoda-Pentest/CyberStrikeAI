package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

// ErrCleanupInProgress indicates a cleanup round is already running; the handler should map it to 409.
var ErrCleanupInProgress = errors.New("a storage cleanup round is already running")

// ErrUnknownCategory indicates the request carried an unregistered category key.
var ErrUnknownCategory = errors.New("unknown cleanup category")

// Decision reasons, used for report and log explainability.
const (
	reasonExpired  = "expired"  // past the retention period
	reasonOrphan   = "orphan"   // session/project deleted, directory left behind
	reasonLeftover = "leftover" // marker directory left by a crashed previous cleanup
	reasonActive   = "active"   // recent activity, protected
	reasonRecent   = "recent"   // grace period not yet reached
	reasonKept     = "kept"     // not expired, or this category's retention period is 0
	reasonUnknown  = "unknown"  // active-status query failed, conservatively skipped
)

// Activity queries the most recent activity time of a session/project. If the implementation errors, cleanup conservatively skips that unit.
type Activity interface {
	ConversationLastActivity(id string) (time.Time, bool, error)
	ProjectLastActivity(id string) (time.Time, bool, error)
}

// CleanRequest describes a single cleanup request.
type CleanRequest struct {
	// When DryRun is true, it only counts and does not delete.
	DryRun bool `json:"dry_run"`
	// An empty Categories means all enabled categories.
	Categories []string `json:"categories"`
	// Trigger is manual / schedule, used only for auditing and logging.
	Trigger string `json:"trigger"`
}

// CategoryReport is the statistics and execution result for a single category.
type CategoryReport struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	Hint          string `json:"hint"`
	Root          string `json:"root"`
	Enabled       bool   `json:"enabled"`
	RetentionDays int    `json:"retention_days"`
	// Missing indicates the root directory has not been created yet (the system has not produced this kind of garbage).
	Missing bool `json:"missing"`

	Units int   `json:"units"`
	Bytes int64 `json:"bytes"`

	ReclaimableUnits int   `json:"reclaimable_units"`
	ReclaimableBytes int64 `json:"reclaimable_bytes"`
	OrphanUnits      int   `json:"orphan_units"`
	SkippedActive    int   `json:"skipped_active"`
	SkippedUnsafe    int   `json:"skipped_unsafe"`

	RemovedUnits     int   `json:"removed_units"`
	FreedBytes       int64 `json:"freed_bytes"`
	RemovedEmptyDirs int   `json:"removed_empty_dirs"`

	Errors []string `json:"errors,omitempty"`
}

// Totals is the aggregation across all categories.
type Totals struct {
	Units            int   `json:"units"`
	Bytes            int64 `json:"bytes"`
	ReclaimableUnits int   `json:"reclaimable_units"`
	ReclaimableBytes int64 `json:"reclaimable_bytes"`
	RemovedUnits     int   `json:"removed_units"`
	FreedBytes       int64 `json:"freed_bytes"`
	SkippedActive    int   `json:"skipped_active"`
	Errors           int   `json:"errors"`
}

// Report is the complete result of a single inspection or cleanup.
type Report struct {
	DryRun     bool             `json:"dry_run"`
	Trigger    string           `json:"trigger"`
	StartedAt  time.Time        `json:"started_at"`
	DurationMS int64            `json:"duration_ms"`
	Filesystem Filesystem       `json:"filesystem"`
	Categories []CategoryReport `json:"categories"`
	Totals     Totals           `json:"totals"`
	Note       string           `json:"note,omitempty"`
}

// defaultCacheTTL limits how often the status endpoint traverses directories: a full walk on a large workspace may take several seconds.
const defaultCacheTTL = time.Minute

// Options holds the dependencies needed to construct a Cleaner.
type Options struct {
	Config   *config.Config
	Paths    Paths
	Activity Activity
	Logger   *zap.Logger
	// Now lets tests inject a fixed clock; when omitted, time.Now is used.
	Now func() time.Time
	// When CacheTTL is omitted, defaultCacheTTL is used; <=0 disables caching.
	CacheTTL time.Duration
}

// Cleaner enumerates, evaluates, and deletes runtime-space garbage.
type Cleaner struct {
	cfg      *config.Config
	paths    Paths
	activity Activity
	logger   *zap.Logger
	now      func() time.Time
	cacheTTL time.Duration

	// running ensures only one cleanup round at a time, preventing two administrators clicking "clean now" from stepping on each other.
	running atomic.Bool

	mu       sync.Mutex
	cached   *Report
	cachedAt time.Time
}

// NewCleaner creates a cleaner. When cfg is nil, a zero-value config is used (equivalent to all default policies).
func NewCleaner(opts Options) *Cleaner {
	c := &Cleaner{
		cfg:      opts.Config,
		paths:    opts.Paths,
		activity: opts.Activity,
		logger:   opts.Logger,
		now:      opts.Now,
		cacheTTL: opts.CacheTTL,
	}
	if c.cfg == nil {
		c.cfg = &config.Config{}
	}
	if c.now == nil {
		c.now = time.Now
	}
	if opts.CacheTTL == 0 {
		c.cacheTTL = defaultCacheTTL
	} else if opts.CacheTTL < 0 {
		c.cacheTTL = 0
	}
	return c
}

// storageConfig returns the currently effective storage policy (read on access, so it takes effect immediately after PUT /api/config).
func (c *Cleaner) storageConfig() config.StorageConfig {
	if c.cfg == nil {
		return config.StorageConfig{}
	}
	return c.cfg.Storage
}

// rootOf returns the absolute path of a category's root directory; it returns an empty string when not configured.
func (c *Cleaner) rootOf(cat category) string {
	root := strings.TrimSpace(cat.root(c.paths))
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// selectCategories returns the categories to process in registry order, ensuring a stable report order.
// When onlyEnabled is true, explicitly disabled categories are skipped.
func (c *Cleaner) selectCategories(keys []string, onlyEnabled bool) ([]category, error) {
	all := categories()
	if len(keys) == 0 {
		out := make([]category, 0, len(all))
		for _, cat := range all {
			if onlyEnabled && !c.storageConfig().CategoryEnabled(cat.key) {
				continue
			}
			out = append(out, cat)
		}
		return out, nil
	}

	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		wanted[k] = true
	}
	known := make(map[string]bool, len(all))
	for _, cat := range all {
		known[cat.key] = true
	}
	for k := range wanted {
		if !known[k] {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCategory, k)
		}
	}
	out := make([]category, 0, len(wanted))
	for _, cat := range all {
		if !wanted[cat.key] {
			continue
		}
		if onlyEnabled && !c.storageConfig().CategoryEnabled(cat.key) {
			continue
		}
		out = append(out, cat)
	}
	return out, nil
}

// scanResult is the product of a category scan: report + deletable units.
type scanResult struct {
	report   CategoryReport
	eligible []Unit
}

// scan enumerates all units under a category and evaluates each, producing statistics and a deletable list.
func (c *Cleaner) scan(cat category, now time.Time) scanResult {
	st := c.storageConfig()
	rep := CategoryReport{
		Key:           cat.key,
		Label:         cat.label,
		Hint:          cat.hint,
		Root:          c.rootOf(cat),
		Enabled:       st.CategoryEnabled(cat.key),
		RetentionDays: st.CategoryRetentionDays(cat.key),
	}
	res := scanResult{report: rep}
	if rep.Root == "" {
		rep.Missing = true
		res.report = rep
		return res
	}
	if _, err := os.Stat(rep.Root); err != nil {
		rep.Missing = true
		res.report = rep
		return res
	}

	units, err := cat.scan(rep.Root)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		res.report = rep
		return res
	}

	for _, u := range units {
		rep.Units++
		rep.Bytes += u.Size
		// Defense in depth: the scanner only produces paths under root; assert it once more here.
		if !confined(rep.Root, u.Path) {
			rep.SkippedUnsafe++
			continue
		}
		d := c.evaluate(u, rep.RetentionDays, now)
		if !d.eligible {
			if d.reason == reasonActive || d.reason == reasonUnknown {
				rep.SkippedActive++
			}
			continue
		}
		rep.ReclaimableUnits++
		rep.ReclaimableBytes += u.Size
		if d.reason == reasonOrphan {
			rep.OrphanUnits++
		}
		res.eligible = append(res.eligible, u)
	}
	res.report = rep
	return res
}

// decision is the evaluation result for a single unit.
type decision struct {
	eligible bool
	reason   string
}

// evaluate decides whether a unit can be deleted.
// Priority: crash-residual marker > recent-activity protection > session liveness > retention period.
func (c *Cleaner) evaluate(u Unit, retentionDays int, now time.Time) decision {
	// A marker directory left when a previous cleanup crashed midway: delete unconditionally.
	// This must come first, otherwise its mtime is the just-renamed time and it would be permanently blocked by active protection.
	if strings.HasSuffix(filepath.Base(u.Path), deletionMarkerSuffix) {
		return decision{eligible: true, reason: reasonLeftover}
	}

	st := c.storageConfig()
	activeGrace := time.Duration(st.ActiveGraceHoursEffective()) * time.Hour
	age := u.Age(now)
	if age < activeGrace {
		return decision{reason: reasonActive}
	}

	retention := time.Duration(retentionDays) * 24 * time.Hour

	if u.Scope != ScopeNone && u.Session != "" && c.activity != nil {
		last, exists, err := c.lastActivity(u)
		if err != nil {
			// When active status cannot be determined, skip conservatively: better to delete too little than to wrongly delete data of a running task.
			return decision{reason: reasonUnknown}
		}
		if exists {
			if now.Sub(last) < activeGrace {
				return decision{reason: reasonActive}
			}
			if retentionDays > 0 && age >= retention {
				return decision{eligible: true, reason: reasonExpired}
			}
			return decision{reason: reasonKept}
		}
		// The session/project no longer exists -> orphaned directory, reclaimed by the shorter grace period.
		if age >= time.Duration(st.OrphanGraceDaysEffective())*24*time.Hour {
			return decision{eligible: true, reason: reasonOrphan}
		}
		return decision{reason: reasonRecent}
	}

	// retention_days: 0 means no retention-based cleanup (following this project's existing convention).
	if retentionDays <= 0 {
		return decision{reason: reasonKept}
	}
	if age >= retention {
		return decision{eligible: true, reason: reasonExpired}
	}
	return decision{reason: reasonKept}
}

func (c *Cleaner) lastActivity(u Unit) (time.Time, bool, error) {
	switch u.Scope {
	case ScopeConversation:
		return c.activity.ConversationLastActivity(u.Session)
	case ScopeProject:
		return c.activity.ProjectLastActivity(u.Session)
	default:
		return time.Time{}, false, nil
	}
}

// Inspect counts the usage and reclaimable amount of all categories without deleting any files.
// Results are cached per CacheTTL; when refresh is true, recomputation is forced.
func (c *Cleaner) Inspect(refresh bool) *Report {
	c.mu.Lock()
	if !refresh && c.cacheTTL > 0 && c.cached != nil && c.now().Sub(c.cachedAt) < c.cacheTTL {
		cached := c.cached
		c.mu.Unlock()
		return cached
	}
	c.mu.Unlock()

	rep := c.buildReport(CleanRequest{DryRun: true, Trigger: "inspect"}, true)

	c.mu.Lock()
	c.cached = rep
	c.cachedAt = c.now()
	c.mu.Unlock()
	return rep
}

// invalidateCache forces the next Inspect to traverse again.
func (c *Cleaner) invalidateCache() {
	c.mu.Lock()
	c.cached = nil
	c.mu.Unlock()
}

// Clean performs one cleanup; when DryRun is true, it only counts.
// It only processes enabled categories, and allows only one round at a time.
func (c *Cleaner) Clean(req CleanRequest) (*Report, error) {
	if !c.running.CompareAndSwap(false, true) {
		return nil, ErrCleanupInProgress
	}
	defer c.running.Store(false)

	req.Trigger = strings.TrimSpace(req.Trigger)
	if req.Trigger == "" {
		req.Trigger = "manual"
	}
	if _, err := c.selectCategories(req.Categories, false); err != nil {
		return nil, err
	}
	rep := c.buildReport(req, false)
	c.invalidateCache()

	if !req.DryRun {
		c.logClean(rep, req)
	}
	return rep, nil
}

// buildReport is the shared body of Inspect and Clean.
// When inspectAll is true, it counts all categories (including disabled ones) for the status page;
// when false, it only processes enabled categories and actually performs deletion.
func (c *Cleaner) buildReport(req CleanRequest, inspectAll bool) *Report {
	startedAt := c.now()
	cats, err := c.selectCategories(req.Categories, !inspectAll)
	rep := &Report{
		DryRun:    req.DryRun,
		Trigger:   req.Trigger,
		StartedAt: startedAt,
	}
	if err != nil {
		rep.Note = err.Error()
		rep.Categories = []CategoryReport{}
		return rep
	}

	for _, cat := range cats {
		res := c.scan(cat, startedAt)
		cr := res.report

		if !req.DryRun && cr.Enabled {
			root := cr.Root
			for _, u := range res.eligible {
				if rmErr := removeUnit(u); rmErr != nil {
					cr.Errors = append(cr.Errors, fmt.Sprintf("%s: %v", filepath.Base(u.Path), rmErr))
					continue
				}
				cr.RemovedUnits++
				cr.FreedBytes += u.Size
			}
			if cat.pruneEmpty && cr.RemovedUnits > 0 && root != "" {
				// Date level + session level, at most two levels.
				cr.RemovedEmptyDirs = pruneEmptyDirs(root, 2)
			}
		}

		rep.Categories = append(rep.Categories, cr)
	}
	if rep.Categories == nil {
		rep.Categories = []CategoryReport{}
	}

	for _, cr := range rep.Categories {
		rep.Totals.Units += cr.Units
		rep.Totals.Bytes += cr.Bytes
		rep.Totals.ReclaimableUnits += cr.ReclaimableUnits
		rep.Totals.ReclaimableBytes += cr.ReclaimableBytes
		rep.Totals.RemovedUnits += cr.RemovedUnits
		rep.Totals.FreedBytes += cr.FreedBytes
		rep.Totals.SkippedActive += cr.SkippedActive
		rep.Totals.Errors += len(cr.Errors)
	}

	rep.Filesystem = c.filesystem()
	rep.DurationMS = time.Since(startedAt).Milliseconds()
	return rep
}

// filesystem takes the filesystem of the first existing category root directory as the capacity source for the overview card.
func (c *Cleaner) filesystem() Filesystem {
	probe := ""
	for _, cat := range categories() {
		if root := c.rootOf(cat); root != "" {
			probe = root
			break
		}
	}
	if probe == "" {
		probe = "."
	}
	fs, err := FilesystemUsage(probe)
	if err != nil && c.logger != nil {
		c.logger.Debug("failed to query filesystem capacity", zap.String("path", probe), zap.Error(err))
	}
	return fs
}

// removeUnit deletes a unit. Directories are atomically renamed first, then recursively deleted:
// a crash midway only leaves a directory with the deletionMarkerSuffix, which the next evaluate deletes unconditionally.
func removeUnit(u Unit) error {
	if u.IsDir {
		marker := u.Path + deletionMarkerSuffix
		if err := os.Rename(u.Path, marker); err == nil {
			return ignoreMissing(os.RemoveAll(marker))
		}
		// If renaming fails (cross-device, permissions, same-name residue), fall back to direct deletion.
	}
	return ignoreMissing(os.RemoveAll(u.Path))
}

func ignoreMissing(err error) error {
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func (c *Cleaner) logClean(rep *Report, req CleanRequest) {
	if c.logger == nil {
		return
	}
	summary := []zap.Field{
		zap.String("trigger", req.Trigger),
		zap.Int("removed_units", rep.Totals.RemovedUnits),
		zap.Int64("freed_bytes", rep.Totals.FreedBytes),
		zap.Int("skipped_active", rep.Totals.SkippedActive),
		zap.Int("errors", rep.Totals.Errors),
	}
	if rep.Totals.RemovedUnits == 0 && rep.Totals.Errors == 0 {
		c.logger.Debug("runtime-space cleanup completed, nothing to reclaim", summary...)
		return
	}
	c.logger.Info("runtime-space cleanup completed", summary...)
	for _, cr := range rep.Categories {
		if cr.RemovedUnits == 0 && len(cr.Errors) == 0 {
			continue
		}
		c.logger.Info("cleanup category details",
			zap.String("category", cr.Key),
			zap.Int("removed", cr.RemovedUnits),
			zap.Int64("freed_bytes", cr.FreedBytes),
			zap.Strings("errors", cr.Errors))
	}
}
