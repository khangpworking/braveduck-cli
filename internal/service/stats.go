package service

// Stats is the board's funnel: jobs per lane plus the interview-rate
// ("morale") figures, computed by the ported jobs.SuccessRateOf logic.
type Stats struct {
	Total         int            `json:"total"`
	ByStage       map[string]int `json:"by_stage"`
	Resolved      int            `json:"resolved"`
	Interviews    int            `json:"interviews"`
	Pending       int            `json:"pending"`
	InterviewRate float64        `json:"interview_rate"` // interviews / resolved; 0 until something resolves
	Morale        string         `json:"morale"`
}
