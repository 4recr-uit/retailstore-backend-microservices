package api

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws-containers/retail-store-sample-app/catalog/model"
)

type mockCatalogRepository struct {
	getProductsFn   func(tags []string, order string, pageNum, pageSize int, ctx context.Context) ([]model.Product, error)
	countProductsFn func(tags []string, ctx context.Context) (int, error)
	getProductFn    func(id string, ctx context.Context) (*model.Product, error)
	getTagsFn       func(ctx context.Context) ([]model.Tag, error)
}

func (m *mockCatalogRepository) GetProducts(tags []string, order string, pageNum, pageSize int, ctx context.Context) ([]model.Product, error) {
	if m.getProductsFn != nil {
		return m.getProductsFn(tags, order, pageNum, pageSize, ctx)
	}
	return nil, nil
}

func (m *mockCatalogRepository) CountProducts(tags []string, ctx context.Context) (int, error) {
	if m.countProductsFn != nil {
		return m.countProductsFn(tags, ctx)
	}
	return 0, nil
}

func (m *mockCatalogRepository) GetProduct(id string, ctx context.Context) (*model.Product, error) {
	if m.getProductFn != nil {
		return m.getProductFn(id, ctx)
	}
	return nil, nil
}

func (m *mockCatalogRepository) GetTags(ctx context.Context) ([]model.Tag, error) {
	if m.getTagsFn != nil {
		return m.getTagsFn(ctx)
	}
	return nil, nil
}

type mockSearchRepository struct {
	searchProductsFn func(keyword string, page, size int, ctx context.Context) ([]model.Product, error)
	reindexFn        func() error
}

func (m *mockSearchRepository) SearchProducts(keyword string, page, size int, ctx context.Context) ([]model.Product, error) {
	if m.searchProductsFn != nil {
		return m.searchProductsFn(keyword, page, size, ctx)
	}
	return nil, nil
}

func (m *mockSearchRepository) Reindex() error {
	if m.reindexFn != nil {
		return m.reindexFn()
	}
	return nil
}

func TestNewCatalogAPI(t *testing.T) {
	repo := &mockCatalogRepository{}

	t.Run("search disabled", func(t *testing.T) {
		catalogAPI, err := NewCatalogAPI(repo, nil)
		assert.NoError(t, err)
		assert.NotNil(t, catalogAPI)
		assert.False(t, catalogAPI.IsSearchEnabled())
	})

	t.Run("search enabled", func(t *testing.T) {
		searchRepo := &mockSearchRepository{}
		catalogAPI, err := NewCatalogAPI(repo, searchRepo)
		assert.NoError(t, err)
		assert.NotNil(t, catalogAPI)
		assert.True(t, catalogAPI.IsSearchEnabled())
	})
}

func TestCatalogAPI_GetProducts(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		expected := []model.Product{
			{ID: "prod-1", Name: "Product 1", Price: 100},
			{ID: "prod-2", Name: "Product 2", Price: 200},
		}
		repo := &mockCatalogRepository{
			getProductsFn: func(tags []string, order string, pageNum, pageSize int, ctx context.Context) ([]model.Product, error) {
				assert.Equal(t, []string{"clothing"}, tags)
				assert.Equal(t, "price_asc", order)
				assert.Equal(t, 1, pageNum)
				assert.Equal(t, 10, pageSize)
				return expected, nil
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		products, err := catalogAPI.GetProducts([]string{"clothing"}, "price_asc", 1, 10, ctx)
		assert.NoError(t, err)
		assert.Equal(t, expected, products)
	})

	t.Run("error propagation", func(t *testing.T) {
		expectedErr := errors.New("db error")
		repo := &mockCatalogRepository{
			getProductsFn: func(tags []string, order string, pageNum, pageSize int, ctx context.Context) ([]model.Product, error) {
				return nil, expectedErr
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		products, err := catalogAPI.GetProducts(nil, "", 1, 10, ctx)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, products)
	})
}

func TestCatalogAPI_GetProduct(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		expected := &model.Product{ID: "prod-1", Name: "Product 1", Price: 100}
		repo := &mockCatalogRepository{
			getProductFn: func(id string, ctx context.Context) (*model.Product, error) {
				assert.Equal(t, "prod-1", id)
				return expected, nil
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		product, err := catalogAPI.GetProduct("prod-1", ctx)
		assert.NoError(t, err)
		assert.Equal(t, expected, product)
	})

	t.Run("error propagation", func(t *testing.T) {
		expectedErr := errors.New("not found")
		repo := &mockCatalogRepository{
			getProductFn: func(id string, ctx context.Context) (*model.Product, error) {
				return nil, expectedErr
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		product, err := catalogAPI.GetProduct("missing", ctx)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, product)
	})
}

func TestCatalogAPI_GetSize(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		repo := &mockCatalogRepository{
			countProductsFn: func(tags []string, ctx context.Context) (int, error) {
				assert.Equal(t, []string{"food"}, tags)
				return 42, nil
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		size, err := catalogAPI.GetSize([]string{"food"}, ctx)
		assert.NoError(t, err)
		assert.Equal(t, 42, size)
	})

	t.Run("error propagation", func(t *testing.T) {
		expectedErr := errors.New("count failed")
		repo := &mockCatalogRepository{
			countProductsFn: func(tags []string, ctx context.Context) (int, error) {
				return 0, expectedErr
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		size, err := catalogAPI.GetSize(nil, ctx)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Equal(t, 0, size)
	})
}

func TestCatalogAPI_GetTags(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		expected := []model.Tag{
			{Name: "tag1", DisplayName: "Tag 1"},
			{Name: "tag2", DisplayName: "Tag 2"},
		}
		repo := &mockCatalogRepository{
			getTagsFn: func(ctx context.Context) ([]model.Tag, error) {
				return expected, nil
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		tags, err := catalogAPI.GetTags(ctx)
		assert.NoError(t, err)
		assert.Equal(t, expected, tags)
	})

	t.Run("error propagation", func(t *testing.T) {
		expectedErr := errors.New("tags error")
		repo := &mockCatalogRepository{
			getTagsFn: func(ctx context.Context) ([]model.Tag, error) {
				return nil, expectedErr
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, nil)
		tags, err := catalogAPI.GetTags(ctx)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, tags)
	})
}

func TestCatalogAPI_SearchProducts(t *testing.T) {
	ctx := context.Background()

	t.Run("search disabled returns nil nil", func(t *testing.T) {
		repo := &mockCatalogRepository{}
		catalogAPI, _ := NewCatalogAPI(repo, nil)

		products, err := catalogAPI.SearchProducts("watch", 1, 10, ctx)
		assert.NoError(t, err)
		assert.Nil(t, products)
	})

	t.Run("search enabled success", func(t *testing.T) {
		repo := &mockCatalogRepository{}
		expected := []model.Product{
			{ID: "prod-search", Name: "Search Watch"},
		}
		searchRepo := &mockSearchRepository{
			searchProductsFn: func(keyword string, page, size int, ctx context.Context) ([]model.Product, error) {
				assert.Equal(t, "watch", keyword)
				assert.Equal(t, 1, page)
				assert.Equal(t, 10, size)
				return expected, nil
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, searchRepo)
		products, err := catalogAPI.SearchProducts("watch", 1, 10, ctx)
		assert.NoError(t, err)
		assert.Equal(t, expected, products)
	})

	t.Run("search enabled error propagation", func(t *testing.T) {
		repo := &mockCatalogRepository{}
		expectedErr := errors.New("search service unavailable")
		searchRepo := &mockSearchRepository{
			searchProductsFn: func(keyword string, page, size int, ctx context.Context) ([]model.Product, error) {
				return nil, expectedErr
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, searchRepo)
		products, err := catalogAPI.SearchProducts("watch", 1, 10, ctx)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, products)
	})
}

func TestCatalogAPI_Reindex(t *testing.T) {
	t.Run("search disabled returns error", func(t *testing.T) {
		repo := &mockCatalogRepository{}
		catalogAPI, _ := NewCatalogAPI(repo, nil)

		err := catalogAPI.Reindex()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "search is not enabled")
	})

	t.Run("search enabled success", func(t *testing.T) {
		repo := &mockCatalogRepository{}
		reindexCalled := false
		searchRepo := &mockSearchRepository{
			reindexFn: func() error {
				reindexCalled = true
				return nil
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, searchRepo)
		err := catalogAPI.Reindex()
		assert.NoError(t, err)
		assert.True(t, reindexCalled)
	})

	t.Run("search enabled error propagation", func(t *testing.T) {
		repo := &mockCatalogRepository{}
		expectedErr := errors.New("reindex failed")
		searchRepo := &mockSearchRepository{
			reindexFn: func() error {
				return expectedErr
			},
		}

		catalogAPI, _ := NewCatalogAPI(repo, searchRepo)
		err := catalogAPI.Reindex()
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})
}
