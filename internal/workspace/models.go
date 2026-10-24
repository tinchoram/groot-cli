package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/provider"
)

const (
	// modelListTimeout bounds the call that fetches the provider's list.
	modelListTimeout = 10 * time.Second
	// modelCacheTTL is how long a fetched list is reused.
	modelCacheTTL = 10 * time.Minute
	// modelSuggestions is how many closest ids a refusal offers.
	modelSuggestions = 3
)

// modelCache keeps the provider's list of models for modelCacheTTL. Only a
// successful fetch is kept. The mutex also keeps concurrent callers from
// fetching the list twice.
type modelCache struct {
	mu     sync.Mutex
	models []provider.ModelInfo
	at     time.Time
}

// ListModels asks the configured provider for the models it offers. The list is
// cached for modelCacheTTL.
func (w *Workspace) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	w.modelCache.mu.Lock()
	defer w.modelCache.mu.Unlock()
	if !w.modelCache.at.IsZero() && time.Since(w.modelCache.at) < modelCacheTTL {
		return slices.Clone(w.modelCache.models), nil
	}
	lister, ok := w.opts.NewProvider(w.opts.Config.Provider.Model).(provider.ModelLister)
	if !ok {
		return nil, errors.New("this provider cannot list its models")
	}
	lctx, cancel := context.WithTimeout(ctx, modelListTimeout)
	defer cancel()
	models, err := lister.ListModels(lctx)
	if err != nil {
		return nil, err
	}
	w.modelCache.models, w.modelCache.at = models, time.Now()
	return slices.Clone(models), nil
}

// CheckModel validates a model id against the provider's list. An unknown id is
// refused with the closest ids. When the list cannot be fetched, the id is
// accepted and warn says the check did not happen. force skips the check.
func (w *Workspace) CheckModel(ctx context.Context, id string, force bool) (warn string, err error) {
	if force {
		return "", nil
	}
	models, lerr := w.ListModels(ctx)
	if lerr != nil {
		return "model not checked against the provider list: " + lerr.Error(), nil
	}
	if hasModel(models, id) {
		return "", nil
	}
	near := suggestModels(id, models, modelSuggestions)
	if len(near) == 0 {
		return "", fmt.Errorf("unknown model %q (use --force to set it anyway)", id)
	}
	return "", fmt.Errorf("unknown model %q · did you mean %s? (use --force to set it anyway)", id, strings.Join(near, ", "))
}

// CheckConfiguredModel checks provider.model from groot.yaml at startup. It
// returns a warning, never an error: a bad configured model does not stop the
// program. It returns "" when no model is configured or the model is listed.
func (w *Workspace) CheckConfiguredModel(ctx context.Context) string {
	id := w.opts.Config.Provider.Model
	if id == "" {
		return ""
	}
	warn, err := w.CheckModel(ctx, id, false)
	if err != nil {
		return "provider.model in groot.yaml: " + err.Error()
	}
	return warn
}

// warnUnlistedRoute checks the model of a flow route when a task opens. It only
// warns: the route still applies.
func (w *Workspace) warnUnlistedRoute(key, model string) {
	if model == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelListTimeout)
	defer cancel()
	warn, err := w.CheckModel(ctx, model, false)
	switch {
	case err != nil:
		keyedWarn(w.opts.Hooks, key)("route " + err.Error())
	case warn != "":
		keyedWarn(w.opts.Hooks, key)("route " + warn)
	}
}

// suggestModels returns up to n ids of models closest to id: first the ids that
// share a prefix with it, then by edit distance.
func suggestModels(id string, models []provider.ModelInfo, n int) []string {
	type candidate struct {
		id     string
		prefix bool
		dist   int
	}
	cands := make([]candidate, 0, len(models))
	for _, m := range models {
		cands = append(cands, candidate{
			id:     m.ID,
			prefix: strings.HasPrefix(m.ID, id) || strings.HasPrefix(id, m.ID),
			dist:   levenshtein(id, m.ID),
		})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].prefix != cands[j].prefix {
			return cands[i].prefix
		}
		return cands[i].dist < cands[j].dist
	})
	var out []string
	for i := 0; i < len(cands) && i < n; i++ {
		out = append(out, cands[i].id)
	}
	return out
}

// levenshtein is the edit distance between a and b, in runes.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// SetAgentModel saves the model an agent runs on into the agent's file. The id
// is checked first, as CheckModel does; an unlisted or unknown id is refused.
// The returned warning says when the check could not run.
func (w *Workspace) SetAgentModel(ctx context.Context, agent, model string) (string, error) {
	warn, err := w.CheckModel(ctx, model, false)
	if err != nil {
		return "", err
	}
	if err := w.SaveAgentModel(agent, model); err != nil {
		return "", err
	}
	return warn, nil
}

// SaveAgentModel writes the model of an agent into its file without checking the
// id. The UI uses it for --force.
func (w *Workspace) SaveAgentModel(agent, model string) error {
	defs, err := catalog.Load(w.opts.Home)
	if err != nil {
		return err
	}
	for _, d := range defs {
		if d.Name != agent {
			continue
		}
		d.Model = model
		if err := catalog.Save(w.opts.Home, d, d.Prompt); err != nil {
			return err
		}
		// The catalog in memory and the open conversations of this agent
		// follow the file, so list_agents and the next turn see the change.
		if err := w.reload(); err != nil {
			return err
		}
		w.applyAgentModel(agent, model)
		return nil
	}
	return fmt.Errorf("unknown agent %q", agent)
}

func hasModel(models []provider.ModelInfo, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}

// ListModels and SetAgentModel let the administrator agent reach the model
// tools through the admin manager.
func (a adminManager) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	return a.w.ListModels(ctx)
}

func (a adminManager) SetAgentModel(ctx context.Context, agent, model string) (string, error) {
	return a.w.SetAgentModel(ctx, agent, model)
}

// applyAgentModel switches every open conversation of agent to model. An
// empty model means "the default", which the provider resolves.
func (w *Workspace) applyAgentModel(agent, model string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, c := range w.convs {
		if !strings.HasSuffix(key, "/"+agent) {
			continue
		}
		target := model
		if target == "" {
			target = w.inheritedModelLocked(c.space)
		}
		_ = c.SetModel(target)
	}
}
