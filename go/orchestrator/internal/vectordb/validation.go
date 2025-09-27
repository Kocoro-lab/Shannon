package vectordb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.uber.org/zap"
)

// DimensionMismatchError is returned when embedding dimensions don't match collection dimensions
type DimensionMismatchError struct {
	Collection         string
	ExpectedDimension  int
	ReceivedDimension  int
	SuggestedAction    string
}

func (e DimensionMismatchError) Error() string {
	return fmt.Sprintf("dimension mismatch for collection %s: expected %d, got %d. %s",
		e.Collection, e.ExpectedDimension, e.ReceivedDimension, e.SuggestedAction)
}

// ValidateEmbeddingDimensions checks if the embedding dimensions match the collection configuration
func (c *Client) ValidateEmbeddingDimensions(ctx context.Context) error {
	if c == nil || !c.cfg.Enabled {
		return nil
	}

	collections := []string{c.cfg.TaskEmbeddings}
	if c.cfg.Summaries != "" {
		collections = append(collections, c.cfg.Summaries)
	}

	for _, collection := range collections {
		info, err := c.getCollectionInfo(ctx, collection)
		if err != nil {
			c.log.Warn("Failed to get collection info during validation",
				zap.String("collection", collection),
				zap.Error(err))
			continue
		}

		// Check if dimensions match configured expectations
		expectedDim := c.cfg.ExpectedEmbeddingDim
		if expectedDim > 0 && info.VectorSize != expectedDim {
			return DimensionMismatchError{
				Collection:        collection,
				ExpectedDimension: expectedDim,
				ReceivedDimension: info.VectorSize,
				SuggestedAction:   "Check embedding model configuration or recreate collection with correct dimensions",
			}
		}

		c.log.Info("Collection dimension validated",
			zap.String("collection", collection),
			zap.Int("dimension", info.VectorSize))
	}

	return nil
}

// CollectionInfo holds basic information about a Qdrant collection
type CollectionInfo struct {
	Name        string
	VectorSize  int
	PointsCount int64
}

// getCollectionInfo retrieves collection information from Qdrant
func (c *Client) getCollectionInfo(ctx context.Context, collection string) (*CollectionInfo, error) {
	url := fmt.Sprintf("%s/collections/%s", c.base, collection)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpw.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get collection info: status %d", resp.StatusCode)
	}

	var result struct {
		Result struct {
			Status         string `json:"status"`
			PointsCount    int64  `json:"points_count"`
			VectorsCount   int64  `json:"vectors_count"`
			SegmentsCount  int    `json:"segments_count"`
			Config         struct {
				Params struct {
					Vectors struct {
						Size     int    `json:"size"`
						Distance string `json:"distance"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &CollectionInfo{
		Name:        collection,
		VectorSize:  result.Result.Config.Params.Vectors.Size,
		PointsCount: result.Result.PointsCount,
	}, nil
}

// ValidateAndInitialize validates dimensions and initializes the client
func ValidateAndInitialize(cfg Config) error {
	Initialize(cfg)

	client := Get()
	if client == nil {
		return fmt.Errorf("failed to initialize vectordb client")
	}

	// Validate dimensions if configured
	if cfg.ExpectedEmbeddingDim > 0 {
		if err := client.ValidateEmbeddingDimensions(context.Background()); err != nil {
			return fmt.Errorf("embedding dimension validation failed: %w", err)
		}
	}

	return nil
}