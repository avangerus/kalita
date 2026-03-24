package workplan

import "context"

type QueuePressureScorer interface {
	Score(ctx context.Context, capability string, queueLen int) float64
}

type queuePressureScorer struct {
	config CoordinationConfig
}

func NewQueuePressureScorer(config CoordinationConfig) QueuePressureScorer {
	return &queuePressureScorer{config: config}
}

func (s *queuePressureScorer) Score(_ context.Context, _ string, queueLen int) float64 {
	threshold := s.config.QueueDepthThreshold
	if threshold <= 0 {
		if queueLen > 0 {
			return 1.0
		}
		return 0.0
	}
	if queueLen <= threshold {
		return 0.0
	}
	pressure := float64(queueLen-threshold) / float64(threshold)
	if pressure > 1.0 {
		return 1.0
	}
	if pressure < 0.0 {
		return 0.0
	}
	return pressure
}
