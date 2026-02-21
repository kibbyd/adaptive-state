package update

import (
	"fmt"
	"math"
	"time"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/google/uuid"
)

// #region update-function
// Update is a pure function that computes the next state from the current state,
// context, signals, and evidence. Phase 4: signal-driven delta with per-element decay.
func Update(old state.StateRecord, ctx UpdateContext, signals Signals, evidence []string, config UpdateConfig) UpdateResult {
	start := time.Now()

	vec := old.StateVector // copy (value type)
	segMap := old.SegmentMap

	// Segment definitions: name → [lo, hi)
	type seg struct {
		name string
		lo   int
		hi   int
	}
	segments := []seg{
		{"prefs", segMap.Prefs[0], segMap.Prefs[1]},
		{"goals", segMap.Goals[0], segMap.Goals[1]},
		{"heuristics", segMap.Heuristics[0], segMap.Heuristics[1]},
		{"risk", segMap.Risk[0], segMap.Risk[1]},
	}

	// Determine which segments are reinforced this turn
	reinforced := map[string]bool{
		"prefs":      signals.SentimentScore > 0,
		"goals":      signals.CoherenceScore > 0,
		"heuristics": signals.NoveltyScore > 0,
		"risk":       ctx.Entropy > 0,
	}

	// Signal strength per segment
	entropySignal := ctx.Entropy
	if entropySignal < 0 {
		entropySignal = 0
	}
	if entropySignal > 1 {
		entropySignal = 1
	}
	signalMap := map[string]float32{
		"prefs":      signals.SentimentScore,
		"goals":      signals.CoherenceScore,
		"heuristics": signals.NoveltyScore,
		"risk":       entropySignal,
	}

	segmentMetrics := make([]SegmentMetric, 0, len(segments))
	segmentsHit := []string{}

	for _, s := range segments {
		var decayNorm float32
		var deltaNorm float32

		// 1. Decay pass: unreinforced segments decay per-element
		if !reinforced[s.name] && config.DecayRate > 0 {
			var decaySumSq float32
			for i := s.lo; i < s.hi; i++ {
				decayAmount := vec[i] * config.DecayRate
				vec[i] -= decayAmount
				decaySumSq += decayAmount * decayAmount
			}
			decayNorm = float32(math.Sqrt(float64(decaySumSq)))
		}

		// 2. Delta pass: signal-driven bounded delta
		strength := signalMap[s.name]
		if strength > 0 && config.LearningRate > 0 {
			size := s.hi - s.lo
			delta := make([]float32, size)

			// Compute raw delta
			for i := s.lo; i < s.hi; i++ {
				dir := float32(1.0)
				if vec[i] < 0 {
					dir = -1.0
				} else if vec[i] > 0 {
					dir = 1.0
				}
				delta[i-s.lo] = config.LearningRate * strength * dir
			}

			// Compute L2 norm and clamp
			var sumSq float32
			for _, d := range delta {
				sumSq += d * d
			}
			norm := float32(math.Sqrt(float64(sumSq)))

			if norm > config.MaxDeltaNormPerSegment {
				scale := config.MaxDeltaNormPerSegment / norm
				for j := range delta {
					delta[j] *= scale
				}
				norm = config.MaxDeltaNormPerSegment
			}

			// Apply delta
			for i := s.lo; i < s.hi; i++ {
				vec[i] += delta[i-s.lo]
			}

			deltaNorm = norm
			segmentsHit = append(segmentsHit, s.name)
		}

		segmentMetrics = append(segmentMetrics, SegmentMetric{
			Name:      s.name,
			DeltaNorm: deltaNorm,
			DecayNorm: decayNorm,
		})
	}

	// 3. Compute total delta norm (new - old)
	var totalDeltaSumSq float32
	for i := 0; i < len(vec); i++ {
		d := vec[i] - old.StateVector[i]
		totalDeltaSumSq += d * d
	}
	totalDeltaNorm := float32(math.Sqrt(float64(totalDeltaSumSq)))

	// 4. Build result
	newRec := state.StateRecord{
		VersionID:   uuid.New().String(),
		ParentID:    old.VersionID,
		StateVector: vec,
		SegmentMap:  old.SegmentMap,
		CreatedAt:   time.Now().UTC(),
	}

	elapsed := time.Since(start).Milliseconds()

	decision := Decision{Action: "no_op", Reason: "no state change"}
	if totalDeltaNorm > 0 {
		decision = Decision{
			Action: "commit",
			Reason: fmt.Sprintf("segments hit: %v, delta norm: %.6f", segmentsHit, totalDeltaNorm),
		}
	}

	metrics := Metrics{
		DeltaNorm:      totalDeltaNorm,
		SegmentsHit:    segmentsHit,
		SegmentMetrics: segmentMetrics,
		UpdateTimeMs:   elapsed,
	}

	return UpdateResult{
		NewState: newRec,
		Decision: decision,
		Metrics:  metrics,
	}
}

// #endregion update-function
