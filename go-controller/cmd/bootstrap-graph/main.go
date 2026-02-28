package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/codec"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/graph"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
)

// #region main
func main() {
	dbPath := envOr("ADAPTIVE_DB", "adaptive_state.db")
	grpcAddr := envOr("CODEC_ADDR", "localhost:50051")

	similarityThreshold := float32(0.3)
	temporalWindowMinutes := 30.0
	searchTopK := 5

	fmt.Println("=== Graph Bootstrap Tool ===")
	fmt.Printf("  DB: %s | Codec: %s\n", dbPath, grpcAddr)
	fmt.Printf("  Similarity threshold: %.2f | Temporal window: %.0f min\n", similarityThreshold, temporalWindowMinutes)

	// Open state DB (for graph store)
	store, err := state.NewStore(dbPath)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	graphStore, err := graph.NewGraphStore(store.DB())
	if err != nil {
		log.Fatalf("failed to init graph store: %v", err)
	}

	// Connect to Python inference service
	codecClient, err := codec.NewCodecClient(grpcAddr)
	if err != nil {
		log.Fatalf("failed to connect to codec service at %s: %v", grpcAddr, err)
	}
	defer codecClient.Close()

	// Fetch all evidence
	fmt.Print("Fetching all evidence... ")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	allEvidence, err := codecClient.ListAllEvidence(ctx)
	cancel()
	if err != nil {
		log.Fatalf("list all evidence: %v", err)
	}
	fmt.Printf("%d items found.\n", len(allEvidence))

	if len(allEvidence) == 0 {
		fmt.Println("No evidence to bootstrap. Done.")
		return
	}

	// Phase 1: Similarity-based co_retrieval edges
	fmt.Println("\n--- Phase 1: Similarity Edges ---")
	coRetrievalCount := 0
	for i, item := range allEvidence {
		// Search for similar items using this item's text
		searchCtx, searchCancel := context.WithTimeout(context.Background(), 30*time.Second)
		results, searchErr := codecClient.Search(searchCtx, item.Text, searchTopK, similarityThreshold)
		searchCancel()
		if searchErr != nil {
			log.Printf("search error for %s: %v", item.ID[:8], searchErr)
			continue
		}

		for _, r := range results {
			if r.ID == item.ID {
				continue // skip self
			}
			// Weight proportional to similarity, scaled to 0-0.5 range
			weight := float64(r.Score) * 0.5
			if weight < 0.01 {
				continue
			}
			if err := graphStore.IncrementEdge(item.ID, r.ID, "co_retrieval", weight); err != nil {
				log.Printf("edge error: %v", err)
				continue
			}
			coRetrievalCount++
		}

		if (i+1)%10 == 0 || i+1 == len(allEvidence) {
			fmt.Printf("  [%d/%d] processed, %d edges so far\n", i+1, len(allEvidence), coRetrievalCount)
		}
	}
	fmt.Printf("  Total co_retrieval edges: %d\n", coRetrievalCount)

	// Phase 2: Temporal edges based on stored_at proximity
	fmt.Println("\n--- Phase 2: Temporal Edges ---")

	type timedItem struct {
		ID       string
		StoredAt time.Time
	}

	var timed []timedItem
	for _, item := range allEvidence {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(item.MetadataJSON), &meta); err != nil {
			continue
		}
		storedAt, ok := meta["stored_at"].(string)
		if !ok || storedAt == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, storedAt)
		if err != nil {
			continue
		}
		timed = append(timed, timedItem{ID: item.ID, StoredAt: t})
	}

	// Sort by stored_at
	sort.Slice(timed, func(i, j int) bool {
		return timed[i].StoredAt.Before(timed[j].StoredAt)
	})

	temporalCount := 0
	windowDuration := time.Duration(temporalWindowMinutes) * time.Minute
	for i := 0; i < len(timed)-1; i++ {
		for j := i + 1; j < len(timed); j++ {
			gap := timed[j].StoredAt.Sub(timed[i].StoredAt)
			if gap > windowDuration {
				break // sorted, so all subsequent will be further
			}
			// Weight inversely proportional to time gap (closer = stronger)
			// Max 0.1 at 0 gap, min ~0.02 at window edge
			gapMinutes := gap.Minutes()
			weight := 0.1 * math.Exp(-gapMinutes/temporalWindowMinutes)
			if weight < 0.01 {
				continue
			}
			if err := graphStore.AddEdge(timed[i].ID, timed[j].ID, "temporal", weight); err != nil {
				log.Printf("temporal edge error: %v", err)
				continue
			}
			temporalCount++
		}
	}
	fmt.Printf("  Items with timestamps: %d\n", len(timed))
	fmt.Printf("  Total temporal edges: %d\n", temporalCount)

	fmt.Printf("\n=== Bootstrap Complete ===\n")
	fmt.Printf("  Evidence items: %d\n", len(allEvidence))
	fmt.Printf("  Co-retrieval edges: %d\n", coRetrievalCount)
	fmt.Printf("  Temporal edges: %d\n", temporalCount)
	fmt.Printf("  Total edges created: %d\n", coRetrievalCount+temporalCount)
}

// #endregion main

// #region helpers
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// #endregion helpers
