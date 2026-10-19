package test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws-containers/retail-store-sample-app/catalog/model"
)

func TestCatalogList(t *testing.T) {
	writer := makeRequest("GET", "/catalog", nil)

	assert.Equal(t, http.StatusOK, writer.Code)

	var response []model.Product
	json.Unmarshal(writer.Body.Bytes(), &response)

	assert.Equal(t, 10, len(response))
}

func TestCatalogProduct(t *testing.T) {
	writer := makeRequest("GET", "/catalog/product/cc789f85-1476-452a-8100-9e74502198e0", nil)

	assert.Equal(t, http.StatusOK, writer.Code)

	var response model.Product
	json.Unmarshal(writer.Body.Bytes(), &response)

	assert.Equal(t, "Temporal Tickstopper", response.Name)
}

func TestCatalogProductMissing(t *testing.T) {
	writer := makeRequest("GET", "/catalog/product/missing", nil)

	assert.Equal(t, http.StatusNotFound, writer.Code)
}

func TestCatalogSize(t *testing.T) {
	writer := makeRequest("GET", "/catalog/size", nil)
	assert.Equal(t, http.StatusOK, writer.Code)

	var response model.CatalogSizeResponse
	err := json.Unmarshal(writer.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, 12, response.Size)

	writerAccessories := makeRequest("GET", "/catalog/size?tags=accessories", nil)
	assert.Equal(t, http.StatusOK, writerAccessories.Code)
	var responseAccessories model.CatalogSizeResponse
	err = json.Unmarshal(writerAccessories.Body.Bytes(), &responseAccessories)
	assert.NoError(t, err)
	assert.Equal(t, 5, responseAccessories.Size)
}

func TestCatalogTags(t *testing.T) {
	writer := makeRequest("GET", "/catalog/tags", nil)
	assert.Equal(t, http.StatusOK, writer.Code)

	var tags []model.Tag
	err := json.Unmarshal(writer.Body.Bytes(), &tags)
	assert.NoError(t, err)
	assert.NotEmpty(t, tags)
	assert.Equal(t, 4, len(tags))

	// Verify ordered by display name
	for i := 1; i < len(tags); i++ {
		assert.True(t, tags[i-1].DisplayName <= tags[i].DisplayName,
			"Tags should be ordered by DisplayName: %s > %s", tags[i-1].DisplayName, tags[i].DisplayName)
	}
}

func TestCatalogListFilteringAndPagination(t *testing.T) {
	t.Run("filter by tags", func(t *testing.T) {
		writer := makeRequest("GET", "/catalog?tags=accessories", nil)
		assert.Equal(t, http.StatusOK, writer.Code)

		var products []model.Product
		err := json.Unmarshal(writer.Body.Bytes(), &products)
		assert.NoError(t, err)
		assert.NotEmpty(t, products)
		for _, p := range products {
			hasTag := false
			for _, tag := range p.Tags {
				if tag.Name == "accessories" {
					hasTag = true
					break
				}
			}
			assert.True(t, hasTag, "product %s should have accessories tag", p.Name)
		}
	})

	t.Run("order price ascending", func(t *testing.T) {
		writer := makeRequest("GET", "/catalog?order=price_asc", nil)
		assert.Equal(t, http.StatusOK, writer.Code)

		var products []model.Product
		err := json.Unmarshal(writer.Body.Bytes(), &products)
		assert.NoError(t, err)
		assert.NotEmpty(t, products)
		for i := 1; i < len(products); i++ {
			assert.True(t, products[i-1].Price <= products[i].Price,
				"expected %d <= %d", products[i-1].Price, products[i].Price)
		}
	})

	t.Run("order price descending", func(t *testing.T) {
		writer := makeRequest("GET", "/catalog?order=price_desc", nil)
		assert.Equal(t, http.StatusOK, writer.Code)

		var products []model.Product
		err := json.Unmarshal(writer.Body.Bytes(), &products)
		assert.NoError(t, err)
		assert.NotEmpty(t, products)
		for i := 1; i < len(products); i++ {
			assert.True(t, products[i-1].Price >= products[i].Price,
				"expected %d >= %d", products[i-1].Price, products[i].Price)
		}
	})

	t.Run("pagination page and size", func(t *testing.T) {
		writer := makeRequest("GET", "/catalog?page=1&size=3", nil)
		assert.Equal(t, http.StatusOK, writer.Code)

		var products []model.Product
		err := json.Unmarshal(writer.Body.Bytes(), &products)
		assert.NoError(t, err)
		assert.Equal(t, 3, len(products))
	})
}

func TestCatalogInvalidPaginationParams(t *testing.T) {
	t.Run("invalid page", func(t *testing.T) {
		writer := makeRequest("GET", "/catalog?page=invalid", nil)
		assert.Equal(t, http.StatusBadRequest, writer.Code)
	})

	t.Run("invalid size", func(t *testing.T) {
		writer := makeRequest("GET", "/catalog?size=invalid", nil)
		assert.Equal(t, http.StatusBadRequest, writer.Code)
	})
}

func TestCatalogSearchAndReindexDisabled(t *testing.T) {
	t.Run("search disabled", func(t *testing.T) {
		writer := makeRequest("GET", "/catalog/search?keyword=watch", nil)
		assert.Equal(t, http.StatusServiceUnavailable, writer.Code)
	})

	t.Run("reindex disabled", func(t *testing.T) {
		writer := makeRequest("POST", "/catalog/reindex", nil)
		assert.Equal(t, http.StatusServiceUnavailable, writer.Code)
	})
}
