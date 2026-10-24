package migration

func NextVersion(maxUsed int) int {
	if maxUsed < 0 {
		maxUsed = 0
	}
	return maxUsed + 1
}
