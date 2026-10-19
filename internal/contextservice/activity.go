package contextservice

import "cmp"

// ActivityTime returns advisory code activity, falling back to captured collection
// time for observations from producers without activity metadata.
func ActivityTime(item Context) int64 {
	if item.LastChangedAt > 0 {
		return item.LastChangedAt
	}
	if item.Observation != nil && item.Observation.CollectedAt > 0 {
		return item.Observation.CollectedAt
	}
	return item.CreatedAt
}

// CompareActivity orders most active first, with stable stream and context ties.
// It never changes stream freshness or catalog pagination.
func CompareActivity(left, right Context) int {
	if order := cmp.Compare(ActivityTime(right), ActivityTime(left)); order != 0 {
		return order
	}
	leftStream, rightStream := left.ID, right.ID
	if left.StreamID != nil {
		leftStream = *left.StreamID
	}
	if right.StreamID != nil {
		rightStream = *right.StreamID
	}
	if order := cmp.Compare(leftStream, rightStream); order != 0 {
		return order
	}
	return cmp.Compare(left.ID, right.ID)
}
