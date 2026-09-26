package llm

const ModelMuseSpark13Contributor = "muse-spark-1.3-contributor"

var MusePrices = map[string]Price{
	ModelMuseSpark13Contributor: {Input: 0.10, Output: 0.20, CacheRead: 0.002},
}
