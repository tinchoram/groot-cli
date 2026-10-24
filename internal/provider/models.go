package provider

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// ModelInfo is one model the provider offers.
type ModelInfo struct {
	ID          string
	DisplayName string
}

// ModelLister is implemented by providers that can list their models.
type ModelLister interface {
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// PriceOf returns the list price of a model in USD per million tokens. The
// boolean is false when the model is not in the price table.
func PriceOf(model string) (Price, bool) {
	p, ok := prices[model]
	return p, ok
}

// ListModels pages through the Models API and returns the models in the order
// the API lists them.
func (a *Anthropic) ListModels(ctx context.Context) ([]ModelInfo, error) {
	page, err := a.client.Models.List(ctx, anthropic.ModelListParams{Limit: param.NewOpt(int64(100))})
	if err != nil {
		return nil, err
	}
	var out []ModelInfo
	for {
		for _, m := range page.Data {
			out = append(out, ModelInfo{ID: m.ID, DisplayName: m.DisplayName})
		}
		if !page.HasMore {
			return out, nil
		}
		page, err = page.GetNextPage()
		if err != nil {
			return nil, err
		}
	}
}

// ListModels returns the models the mock was given.
func (m *Mock) ListModels(context.Context) ([]ModelInfo, error) {
	return m.Models, m.ModelsErr
}
