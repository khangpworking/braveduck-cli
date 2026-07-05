package jobs

import "time"

// Interview-rate ("morale") calculation — port of successRate/rateMessage from
// learning.tsx. Resolved = closed + interview/offer + applied-past-the-response-
// window; the rate is interviews / resolved. Pending applications don't count
// against you yet.

const responseWindowDays = 14

var interviewStages = map[string]bool{"interview": true, "offer": true}

// appliedDate: best available "applied on" date — the apply-silence anchor when
// one was stamped, else the same COALESCE proxy the anchor backfill used.
func appliedDate(j Job) string {
	p := ParseExpiry(j.ExpiresAt)
	if p.Kind == KindApplied && p.Date != "" {
		return p.Date
	}
	for _, s := range []string{j.DraftStatusAt, j.ApplyUpdatedAt, j.SavedAt} {
		if s != "" {
			return s
		}
	}
	return ""
}

type SuccessRate struct {
	Resolved   int
	Interviews int
	Pending    int
	Rate       float64 // interviews / resolved, 0 when nothing resolved
}

// SuccessRateOf computes the funnel counts and interview rate over a set of jobs.
func SuccessRateOf(js []Job, now time.Time) SuccessRate {
	var r SuccessRate
	for _, j := range js {
		switch {
		case interviewStages[j.Status]:
			r.Interviews++
			r.Resolved++
		case j.Status == "closed":
			r.Resolved++
		case j.Status == "applied":
			d := appliedDate(j)
			ageDays := 999
			if t, ok := parseTime(d); ok {
				ageDays = floorDiv(float64(now.Sub(t).Milliseconds()))
			}
			if ageDays >= responseWindowDays {
				r.Resolved++
			} else {
				r.Pending++
			}
		}
	}
	if r.Resolved > 0 {
		r.Rate = float64(r.Interviews) / float64(r.Resolved)
	}
	return r
}

type Morale struct {
	Line  string
	Color string
}

// MoraleMessage is the tiered morale copy. It needs a few resolved CVs before a
// rate means anything. Kept verbatim (incl. Vietnamese copy) from rateMessage.
func MoraleMessage(sr SuccessRate) Morale {
	switch {
	case sr.Resolved < 3:
		return Morale{"Còn sớm để tính tỉ lệ — cứ gửi thêm CV đã 🌱", "var(--muted)"}
	case sr.Rate < 0.10:
		return Morale{"Đủ lượng, chất sẽ đổi. 100 CV không được thì mình sẽ cùng bạn gửi 1000 CV 💪", "#f97316"}
	case sr.Rate < 0.30:
		return Morale{"Đang có đà rồi đó — cứ đều tay, nhà tuyển dụng bắt đầu để mắt tới bạn 🔥", "#84cc16"}
	default:
		return Morale{"You're a fucking superstar 🌟", "#22c55e"}
	}
}
