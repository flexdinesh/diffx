package reviewstore

// comparisonPolicySQL mirrors ingestion.ComparisonPolicy over stored metadata.
func comparisonPolicySQL(metadata string) string {
	return `CASE WHEN COALESCE(json_extract(` + metadata + `,'$.comparison.kind'),'working-tree')='working-tree' THEN '' ELSE json_extract(` + metadata + `,'$.comparison.kind')||':'||json_extract(` + metadata + `,'$.comparison.baseRef') END`
}
