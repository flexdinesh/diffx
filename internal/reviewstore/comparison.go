package reviewstore

import (
	"github.com/flexdinesh/diffx/internal/ingestion"
)

// HEAD and missing comparisons share legacy identity. Resolved commits belong
// to the snapshot, not the durable policy stream.
func comparisonPolicy(metadata ingestion.Metadata) string {
	return ingestion.ComparisonPolicy(metadata)
}

func comparisonPolicySQL(d dialect, metadata string) string {
	kind := d.jsonText(metadata, "comparison.kind")
	return `CASE WHEN COALESCE(` + kind + `,'working-tree')='working-tree' THEN '' ELSE ` + kind + `||':'||` + d.jsonText(metadata, "comparison.baseRef") + ` END`
}
